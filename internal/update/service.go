package update

import (
	"asset-server/generated/api"
	"asset-server/generated/db"
	"asset-server/internal/logger"
	"asset-server/internal/queue"
	"asset-server/internal/storage"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const DefaultChannelName = "production"

var (
	ErrUpdateNotFound     = errors.New("update not found")
	ErrUpdateNotPublished = errors.New("tried to rollback non-published update")
)

type Service interface {
	FindUpdates(
		ctx context.Context,
		projectID uuid.UUID,
		status *api.UpdateStatus,
		runtimeVersion *string,
		channel *string,
	) ([]db.Update, error)
	PrepareUpdate(
		ctx context.Context,
		projectID uuid.UUID,
		request api.PrepareUpdateBody,
	) (uuid.UUID, []api.StorageObjectPathWithURL, error)
	CommitUpdate(ctx context.Context, updateID uuid.UUID) error
	UpdateToInstall(
		ctx context.Context,
		projectID uuid.UUID,
		runtimeVersion string,
		channel string,
		platform string,
		filter CurrentUpdateFilter,
	) (*db.GetLatestPublishedAndCanceledUpdatesRow, error)
	RollbackUpdate(ctx context.Context, projectID uuid.UUID, updateID uuid.UUID) error
	UpdateByID(
		ctx context.Context,
		projectID uuid.UUID,
		updateID uuid.UUID,
	) (*db.Update, error)
	SetUpdateStatus(
		ctx context.Context,
		updateID uuid.UUID,
		status db.UpdateStatus,
	) (*db.Update, error)
	CreateUpdateAssets(ctx context.Context, assets []db.CreateUpdateAssetsParams) (int64, error)
	UpdateByIDWithProtocol(
		ctx context.Context,
		updateID uuid.UUID,
	) (*db.GetUpdateByIDWithProtocolRow, error)
	AssetsByPlatform(
		ctx context.Context,
		updateID uuid.UUID,
		platform string,
	) ([]db.UpdateAsset, error)
}

type service struct {
	q         *db.Queries
	pgPool    *pgxpool.Pool
	storage   *storage.Storage
	queueConn *queue.Connection
}

func NewService(
	q *db.Queries,
	pgPool *pgxpool.Pool,
	st *storage.Storage,
	queueConn *queue.Connection,
) Service {
	return &service{q, pgPool, st, queueConn}
}

func (svc *service) FindUpdates(
	ctx context.Context,
	projectID uuid.UUID,
	status *api.UpdateStatus,
	runtimeVersion *string,
	channel *string,
) ([]db.Update, error) {
	queryParams := db.GetLastNUpdatesParams{
		ProjectID: projectID,
		Limit:     10,
	}

	if status != nil {
		queryParams.Status = db.NullUpdateStatus{
			UpdateStatus: db.UpdateStatus(*status),
			Valid:        true,
		}
	}

	if runtimeVersion != nil {
		queryParams.RuntimeVersion = pgtype.Text{
			String: *runtimeVersion,
			Valid:  true,
		}
	}

	if channel != nil {
		queryParams.Channel = pgtype.Text{
			String: *channel,
			Valid:  true,
		}
	}

	updates, err := svc.q.GetLastNUpdates(ctx, queryParams)
	if err != nil {
		return nil, fmt.Errorf("GetLastNUpdates: %w", err)
	}

	return updates, nil
}

func (svc *service) PrepareUpdate(
	ctx context.Context,
	projectID uuid.UUID,
	request api.PrepareUpdateBody,
) (uuid.UUID, []api.StorageObjectPathWithURL, error) {
	log := logger.FromContext(ctx)
	tx, err := svc.pgPool.Begin(ctx)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer func(tx pgx.Tx, ctx context.Context) {
		err := tx.Rollback(ctx)
		if err != nil && err != pgx.ErrTxClosed {
			logger.FromContext(ctx).
				Error("PrepareUpdate: failed to rollback transaction",
					zap.Error(err),
					zap.Stringp("channel", request.Channel),
					zap.String("runtimeVersion", request.RuntimeVersion))
		}
	}(tx, ctx)

	qtx := svc.q.WithTx(tx)

	update := &db.Update{
		ID:             uuid.Must(uuid.NewV7()),
		ProjectID:      projectID,
		RuntimeVersion: request.RuntimeVersion,
		Message:        pgtype.Text{String: request.Message, Valid: true},
		Channel:        *request.Channel,
	}

	err = qtx.CreateUpdate(ctx, db.CreateUpdateParams{
		ID:             update.ID,
		ProjectID:      update.ProjectID,
		RuntimeVersion: update.RuntimeVersion,
		Message:        update.Message,
		Channel:        update.Channel,
	})
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("CreateUpdate: %w", err)
	}

	if request.ExpoAppConfig != nil {
		appConfigJson, err := json.Marshal(request.ExpoAppConfig)
		if err != nil {
			return uuid.Nil, nil, fmt.Errorf("failed to marshal app config: %w", err)
		}

		if err := qtx.CreateUpdateMetadata(ctx, uuid.Must(uuid.NewV7()), update.ID, appConfigJson); err != nil {
			return uuid.Nil, nil, fmt.Errorf("CreateUpdateMetadata: %w", err)
		}
	}

	uploadURLs, err := svc.storage.UploadURLs(ctx, projectID, update.ID, request.FileMetadata)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("UploadURLs: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Info("update prepared", zap.String("update_id", update.ID.String()))

	return update.ID, uploadURLs, nil
}

func (svc *service) CommitUpdate(
	ctx context.Context,
	updateID uuid.UUID,
) error {
	log := logger.FromContext(ctx)
	update, err := svc.q.SetUpdateStatus(ctx, updateID, db.UpdateStatusPending)
	if err != nil {
		return fmt.Errorf("SetUpdateStatus: %w", err)
	}

	err = svc.queueConn.PublishProcessUpdateMessage(ctx, update.ID)
	if err != nil {
		return fmt.Errorf("PublishProcessUpdateMessage: %w", err)
	}

	log.Info("update committed to processing queue", zap.String("update_id", update.ID.String()))

	return nil
}

type CurrentUpdateFilter struct {
	ID     *uuid.UUID // used by Expo
	SHA256 *string    // used by CodePush, either archive's or bundle's hash
}

func (svc *service) UpdateToInstall(
	ctx context.Context,
	projectID uuid.UUID,
	runtimeVersion string,
	channel string,
	platform string,
	currentUpdate CurrentUpdateFilter,
) (*db.GetLatestPublishedAndCanceledUpdatesRow, error) {
	params := db.GetLatestPublishedAndCanceledUpdatesParams{
		ProjectID:      projectID,
		RuntimeVersion: runtimeVersion,
		Channel:        channel,
		Platform:       platform,
	}

	rows, err := svc.q.GetLatestPublishedAndCanceledUpdates(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUpdateNotFound
		}
		return nil, fmt.Errorf("GetLatestPublishedAndCanceledUpdates: %w", err)
	}

	if len(rows) > 2 {
		return nil, fmt.Errorf("should return at most 2 rows, got %d", len(rows))
	}

	isCurrentUpdate := func(u *db.GetLatestPublishedAndCanceledUpdatesRow) bool {
		matches := false
		if currentUpdate.ID != nil && u.Update.ID == *currentUpdate.ID {
			matches = true
		}

		if currentUpdate.SHA256 != nil && u.ContentSha256.Valid &&
			u.ContentSha256.String == *currentUpdate.SHA256 {
			matches = true
		}

		return matches
	}

	if len(rows) == 2 {
		if rows[0].Update.Status == db.UpdateStatusPublished {
			if !isCurrentUpdate(&rows[0]) {
				return &rows[0], nil
			}

			return nil, nil
		}

		if rows[0].Update.Status == db.UpdateStatusCanceled &&
			rows[1].Update.Status == db.UpdateStatusPublished && !isCurrentUpdate(&rows[1]) {
			return &rows[1], nil
		}

		return nil, nil
	}

	if len(rows) == 1 {
		// current update has been rolled back
		if rows[0].Update.Status == db.UpdateStatusCanceled && isCurrentUpdate(&rows[0]) {
			return &rows[0], nil
		}

		// there's a new published updated
		if rows[0].Update.Status == db.UpdateStatusPublished && !isCurrentUpdate(&rows[0]) {
			return &rows[0], nil
		}

		// published, but already installed, or new but canceled - ignore in both cases
		return nil, nil
	}

	return nil, nil
}

func (svc *service) RollbackUpdate(
	ctx context.Context,
	projectID uuid.UUID,
	updateID uuid.UUID,
) error {
	log := logger.FromContext(ctx)
	update, err := svc.UpdateByID(ctx, projectID, updateID)
	if err != nil {
		if errors.Is(err, ErrUpdateNotFound) {
			return err
		}
		return fmt.Errorf("GetUpdateById: %w", err)
	}

	if update.Status != db.UpdateStatusPublished {
		log.Debug(
			"tried to rollback non-published update",
			zap.String("update_id", updateID.String()),
			zap.String("status", string(update.Status)),
		)
		return ErrUpdateNotPublished
	}

	_, err = svc.q.SetUpdateStatus(ctx, updateID, db.UpdateStatusCanceled)
	if err != nil {
		return fmt.Errorf("SetUpdateStatus: %w", err)
	}

	return nil
}

func (svc *service) UpdateByID(
	ctx context.Context,
	projectID uuid.UUID,
	updateID uuid.UUID,
) (*db.Update, error) {
	u, err := svc.q.GetUpdateByID(ctx, updateID, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUpdateNotFound
		}
		return nil, err
	}

	return &u, nil
}

func (svc *service) UpdateByIDWithProtocol(
	ctx context.Context,
	updateID uuid.UUID,
) (*db.GetUpdateByIDWithProtocolRow, error) {
	u, err := svc.q.GetUpdateByIDWithProtocol(ctx, updateID)
	if err != nil {
		return nil, err
	}

	return &u, nil
}

func (svc *service) CreateUpdateAssets(
	ctx context.Context,
	assets []db.CreateUpdateAssetsParams,
) (int64, error) {
	return svc.q.CreateUpdateAssets(ctx, assets)
}

func (svc *service) SetUpdateStatus(
	ctx context.Context,
	updateID uuid.UUID,
	status db.UpdateStatus,
) (*db.Update, error) {
	u, err := svc.q.SetUpdateStatus(ctx, updateID, status)
	if err != nil {
		return nil, err
	}

	return &u, nil
}

func (svc *service) AssetsByPlatform(
	ctx context.Context,
	updateID uuid.UUID,
	platform string,
) ([]db.UpdateAsset, error) {
	return svc.q.GetUpdateAssetsByPlatform(ctx, updateID, platform)
}
