package update

import (
	"archive/zip"
	"asset-server/generated/db"
	"asset-server/internal/logger"
	"asset-server/internal/queue"
	"asset-server/internal/storage"
	"asset-server/internal/util"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"os/signal"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
	"gocloud.dev/blob"
)

var ErrUpdateNotPending = errors.New("update is not pending")
var platforms = []string{"android", "ios"}

type Processor struct {
	storage   *storage.Storage
	svc       Service
	queueConn *queue.Connection
}

func NewProcessor(
	svc Service,
	storage *storage.Storage,
	queueConn *queue.Connection,
) *Processor {
	return &Processor{
		storage:   storage,
		svc:       svc,
		queueConn: queueConn,
	}
}

func (p *Processor) StartWorker(ctx context.Context) error {
	log := logger.FromContext(ctx)
	err := p.queueConn.Consume(ctx, p.newMessageHandler(ctx), p.newMaxDeliveriesHandler(ctx))
	if err != nil {
		return err
	}
	defer p.queueConn.Close()

	log.Info("worker started")

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	<-signalChan
	return nil
}

func (p *Processor) newMessageHandler(ctx context.Context) func(msg jetstream.Msg) {
	log := logger.FromContext(ctx)
	log = log.With(zap.String("consumer", "process-update"))

	return func(msg jetstream.Msg) {
		payload, err := queue.ParseProcessUpdateMessage(msg.Data())
		if err != nil {
			log.Error("failed to unmarshal payload", zap.Error(err))
			if err := msg.Term(); err != nil {
				log.Error("failed to terminate message", zap.Error(err))
			}
			return
		}

		updateLog := log.With(
			zap.String("update_id", payload.UpdateID.String()),
		)

		updateLog.Info("processing update")

		err = p.ProcessUpdate(ctx, payload.UpdateID)
		if err != nil {
			if errors.Is(err, ErrUpdateNotPending) {
				// TODO: we should probably not drop the message here, but rather set the status to failed
				// after some delay, to pick up the updates that are kept in limbo
				updateLog.Error("update is not pending, dropping")
				if err := msg.Term(); err != nil {
					updateLog.Error("failed to terminate message", zap.Error(err))
				}
				return
			}

			updateLog.Error("failed to process update, retrying in a few sec", zap.Error(err))

			_, err = p.svc.SetUpdateStatus(ctx, payload.UpdateID, db.UpdateStatusPending)
			if err != nil {
				updateLog.Error("failed to set update status back to pending", zap.Error(err))
			}

			if err := msg.NakWithDelay(5 * time.Second); err != nil {
				updateLog.Error("failed to nak message", zap.Error(err))
			}
			return
		}

		updateLog.Info("update processed successfully")

		if err := msg.Ack(); err != nil {
			updateLog.Error("failed to ack message", zap.Error(err))
		}
	}
}

func (p *Processor) newMaxDeliveriesHandler(ctx context.Context) func(msg *jetstream.RawStreamMsg) {
	log := logger.FromContext(ctx)

	return func(msg *jetstream.RawStreamMsg) {
		payload, err := queue.ParseProcessUpdateMessage(msg.Data)
		if err != nil {
			log.Error("failed to unmarshal payload", zap.Error(err))
			return
		}

		updateLog := log.With(
			zap.String("update_id", payload.UpdateID.String()),
		)

		updateLog.Error("max retry attempts reached, dropping message")

		_, err = p.svc.SetUpdateStatus(ctx, payload.UpdateID, db.UpdateStatusFailed)
		if err != nil {
			updateLog.Error("failed to set update status to failed", zap.Error(err))
		}
	}
}

func readMetadata(
	ctx context.Context,
	storage *storage.Storage,
	objectKey string,
) (*Metadata, error) {
	reader, err := storage.Bucket().NewReader(ctx, objectKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}
	defer util.CloseWithLogger(logger.FromContext(ctx), reader)

	meta, err := ParseMetadata(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return meta, nil
}

type assetParser struct {
	st     *storage.Storage
	update db.Update
	log    *zap.Logger
}

type parseAssetMeta struct {
	extension     string
	isLaunchAsset bool
	contentType   string
	platform      string
}

func (p *assetParser) parse(
	ctx context.Context,
	filePath string,
	meta parseAssetMeta,
) (*db.CreateUpdateAssetsParams, error) {
	objectKey := storage.AssetObjectKey(p.update.ProjectID, p.update.ID, filePath)
	blobReader, err := p.st.Bucket().
		NewReader(ctx, objectKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to read bundle file: %w", err)
	}
	defer util.CloseWithLogger(
		p.log.With(zap.String("object_key", objectKey)),
		blobReader,
	)

	shaWriter := sha256.New()
	md5Writer := md5.New()
	writer := io.MultiWriter(shaWriter, md5Writer)

	_, err = io.Copy(writer, blobReader)
	if err != nil {
		return nil, fmt.Errorf("failed to copy bundle file content: %w", err)
	}

	contentSha256 := fmt.Sprintf("%x", shaWriter.Sum(nil))
	contentMd5 := fmt.Sprintf("%x", md5Writer.Sum(nil))

	return &db.CreateUpdateAssetsParams{
		ID:                uuid.Must(uuid.NewV7()),
		UpdateID:          p.update.ID,
		StorageObjectPath: objectKey,
		ContentMd5:        contentMd5,
		ContentSha256:     contentSha256,
		ContentLength:     blobReader.Size(),
		Extension:         meta.extension,
		IsLaunchAsset:     meta.isLaunchAsset,
		Platform:          meta.platform,
		ContentType:       meta.contentType,
	}, nil
}

func (p *assetParser) parseAssets(
	ctx context.Context,
	meta *Metadata,
) ([]db.CreateUpdateAssetsParams, []error) {
	parsedAssets := make([]db.CreateUpdateAssetsParams, 0)
	parseErrors := make([]error, 0)
	for _, platform := range platforms {
		platformMeta, ok := meta.FileMetadata[platform]
		if !ok {
			p.log.Warn("missing platform metadata, skipping", zap.String("platform", platform))
			continue
		}

		{
			extension := path.Ext(platformMeta.Bundle)
			if extension == "" {
				extension = ".bundle"
			}
			asset, err := p.parse(
				ctx,
				platformMeta.Bundle,
				parseAssetMeta{
					extension:     extension,
					isLaunchAsset: true,
					contentType:   "application/javascript",
					platform:      platform,
				},
			)
			if err != nil {
				parseErrors = append(parseErrors, fmt.Errorf("failed to process bundle: %w", err))
				continue
			}

			parsedAssets = append(parsedAssets, *asset)

			p.log.Info("processed bundle", zap.String("platform", asset.Platform))
		}

		for _, assetMeta := range platformMeta.Assets {
			asset, err := p.parse(
				ctx,
				assetMeta.Path,
				parseAssetMeta{
					extension:     assetMeta.Ext,
					isLaunchAsset: false,
					contentType:   mime.TypeByExtension(assetMeta.Ext),
					platform:      platform,
				},
			)
			if err != nil {
				parseErrors = append(parseErrors, fmt.Errorf("failed to process asset: %w", err))
				continue
			}

			p.log.Info("processed asset", zap.String("path", assetMeta.Path))

			parsedAssets = append(parsedAssets, *asset)
		}
	}

	return parsedAssets, parseErrors
}

func (p *Processor) ProcessUpdate(ctx context.Context, id uuid.UUID) error {
	log := logger.FromContext(ctx).With(zap.String("update_id", id.String()))

	updateWithProtocol, err := p.svc.UpdateByIDWithProtocol(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get update: %w", err)
	}

	if updateWithProtocol.Status != db.UpdateStatusPending {
		return ErrUpdateNotPending
	}

	update, err := p.svc.SetUpdateStatus(ctx, updateWithProtocol.ID, db.UpdateStatusProcessing)
	if err != nil {
		return fmt.Errorf("failed to set update status to processing: %w", err)
	}
	log.Info("set update status to processing")

	log = log.With(zap.String("project_id", update.ProjectID.String()))

	metadataJsonPath := storage.AssetObjectKey(update.ProjectID, update.ID, "metadata.json")
	meta, err := readMetadata(ctx, p.storage, metadataJsonPath)
	if err != nil {
		return fmt.Errorf("failed to read metadata.json: %w", err)
	}

	assetParser := &assetParser{
		st:     p.storage,
		update: *update,
		log:    log,
	}
	// TODO: parse only assets that are not already in the DB
	parsedAssets, parseErrors := assetParser.parseAssets(ctx, meta)

	log.Info(fmt.Sprintf("processed %d files (%d errors)", len(parsedAssets), len(parseErrors)))

	numSaved, err := p.svc.CreateUpdateAssets(ctx, parsedAssets)
	if err != nil {
		return fmt.Errorf("failed to save assets to db: %w", err)
	}

	log.Info(fmt.Sprintf("saved %d parsed assets to db", numSaved))

	if len(parseErrors) > 0 {
		return fmt.Errorf("failed to parse some assets")
	}

	archiver := &archiver{
		st:     p.storage,
		update: *update,
		svc:    p.svc,
		log:    log,
	}
	archivedAssets := make([]db.CreateUpdateAssetsParams, 0)
	for _, platform := range platforms {
		platformMeta, ok := meta.FileMetadata[platform]
		if !ok {
			log.Warn("missing platform metadata, skipping", zap.String("platform", platform))
			continue
		}

		shouldMakeArchive := updateWithProtocol.Protocol == db.UpdateProtocolCodepush &&
			len(platformMeta.Assets) > 0

		if shouldMakeArchive {
			assetParams, err := archiver.archiveForPlatform(ctx, platform)
			if err != nil {
				return fmt.Errorf("failed to archive update: %w", err)
			}
			archivedAssets = append(archivedAssets, *assetParams)
		}
	}

	numSaved, err = p.svc.CreateUpdateAssets(ctx, archivedAssets)
	if err != nil {
		return fmt.Errorf("failed to save archive assets to db: %w", err)
	}

	log.Info(fmt.Sprintf("saved %d archive assets to db", numSaved))

	_, err = p.svc.SetUpdateStatus(ctx, update.ID, db.UpdateStatusPublished)
	if err != nil {
		return fmt.Errorf("failed to set update status to published: %w", err)
	}
	log.Info("set update status to published")

	return nil
}

type archiver struct {
	st     *storage.Storage
	update db.Update
	svc    Service
	log    *zap.Logger
}

func (a *archiver) archiveForPlatform(
	ctx context.Context,
	platform string,
) (*db.CreateUpdateAssetsParams, error) {
	log := a.log.With(zap.String("platform", platform))

	objectKey := storage.ArchiveObjectKey(a.update.ProjectID, a.update.ID, platform)
	blobWriter, err := a.st.Bucket().
		NewWriter(ctx, objectKey, &blob.WriterOptions{ContentType: "application/zip"})
	if err != nil {
		return nil, fmt.Errorf("failed to create blob: %w", err)
	}
	defer blobWriter.Close()

	assets, err := a.svc.AssetsByPlatform(ctx, a.update.ID, platform)
	if err != nil {
		return nil, fmt.Errorf("failed to get assets from db: %w", err)
	}

	if len(assets) == 0 {
		return nil, fmt.Errorf("no assets found for platform %s", platform)
	}

	zipWriter := zip.NewWriter(blobWriter)
	defer zipWriter.Close()

	archivedAssets := 0
	for _, asset := range assets {
		_, _, fileLocalPath := storage.AssetObjectKeySegments(asset.StorageObjectPath)

		// during bundling assets are stored in a platform-specific folder,
		// so we need to trim the platform prefix from the path,
		// so that the path is the same as in the original build
		pathInZip := strings.TrimPrefix(fileLocalPath, platform+"/")

		zipFileWriter, err := zipWriter.Create(pathInZip)
		if err != nil {
			return nil, fmt.Errorf("failed to create file in zip: %w", err)
		}

		blobReader, err := a.st.Bucket().NewReader(ctx, asset.StorageObjectPath, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to read asset from storage: %w", err)
		}
		defer blobReader.Close()

		_, err = io.Copy(zipFileWriter, blobReader)
		if err != nil {
			return nil, fmt.Errorf("failed to copy asset to zip: %w", err)
		}

		err = blobReader.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to close blob reader: %w", err)
		}
		archivedAssets += 1
	}

	err = zipWriter.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close zip writer: %w", err)
	}

	err = blobWriter.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close blob writer: %w", err)
	}

	log.Info(fmt.Sprintf("archived %d assets", archivedAssets))

	contentSha256, err := calculateSHA256ForArchive(assets)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate sha256: %w", err)
	}

	attrs, err := a.st.Bucket().Attributes(ctx, objectKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get attributes: %w", err)
	}

	return &db.CreateUpdateAssetsParams{
		ID:                uuid.Must(uuid.NewV7()),
		UpdateID:          a.update.ID,
		StorageObjectPath: objectKey,
		ContentType:       "application/zip",
		Extension:         ".zip",
		ContentMd5:        fmt.Sprintf("%x", attrs.MD5),
		ContentSha256:     contentSha256,
		IsLaunchAsset:     false,
		IsArchive:         true,
		Platform:          platform,
		ContentLength:     attrs.Size,
	}, nil
}

// calculateSHA256ForArchive calculates CodePush compatible SHA256 hash for the archive
func calculateSHA256ForArchive(assets []db.UpdateAsset) (string, error) {
	tokens := make([]string, 0, len(assets))
	for _, asset := range assets {
		_, _, filePath := storage.AssetObjectKeySegments(asset.StorageObjectPath)
		tokens = append(tokens, fmt.Sprintf("%s:%s", filePath, asset.ContentSha256))
	}
	slices.Sort(tokens)

	jsonData, err := json.Marshal(tokens)
	if err != nil {
		return "", fmt.Errorf("json.Marshal: %w", err)
	}

	hash := sha256.Sum256(jsonData)
	return fmt.Sprintf("%x", hash[:]), nil
}
