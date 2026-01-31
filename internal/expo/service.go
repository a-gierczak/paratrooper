package expo

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/a-gierczak/paratrooper/generated/db"
	"github.com/a-gierczak/paratrooper/internal/storage"

	"gocloud.dev/blob"
)

type Manifest struct {
	Id             string          `json:"id"`
	CreatedAt      string          `json:"createdAt"`
	RuntimeVersion string          `json:"runtimeVersion"`
	Assets         []ManifestAsset `json:"assets"`
	LaunchAsset    ManifestAsset   `json:"launchAsset"`
}

type ManifestAsset struct {
	Hash          string `json:"hash"`
	Key           string `json:"key"`
	FileExtension string `json:"fileExtension"`
	ContentType   string `json:"contentType"`
	Url           string `json:"url"`
}

type service struct {
	q       *db.Queries
	storage *storage.Storage
}

type Service interface {
	UpdateManifest(
		ctx context.Context,
		update db.Update,
		platform string,
	) (*Manifest, error)
}

func NewService(q *db.Queries, st *storage.Storage) Service {
	return &service{q, st}
}

func (svc *service) UpdateManifest(
	ctx context.Context,
	update db.Update,
	platform string,
) (*Manifest, error) {
	updateAssets, err := svc.q.GetUpdateAssetsByPlatform(ctx, update.ID, platform)
	if err != nil {
		return nil, fmt.Errorf("GetUpdateAssetsByPlatform: %w", err)
	}

	if len(updateAssets) == 0 {
		return nil, fmt.Errorf("no assets found for update %s", update.ID)
	}

	var launchAsset *ManifestAsset
	manifestAssets := make([]ManifestAsset, 0)

	for _, asset := range updateAssets {
		sha256Bytes, err := hex.DecodeString(asset.ContentSha256)
		if err != nil {
			return nil, fmt.Errorf("failed to decode sha256: %w", err)
		}

		assetURL, err := svc.storage.Bucket().
			SignedURL(ctx, asset.StorageObjectPath, &blob.SignedURLOptions{
				Method: "GET",
				Expiry: storage.DownloadURLExpiry,
			})
		if err != nil {
			return nil, fmt.Errorf("failed to get asset URL: %w", err)
		}

		manifestAsset := ManifestAsset{
			Hash:          base64.RawURLEncoding.EncodeToString(sha256Bytes),
			Key:           asset.ContentMd5,
			FileExtension: asset.Extension,
			ContentType:   asset.ContentType,
			Url:           assetURL,
		}
		if asset.IsLaunchAsset {
			launchAsset = &manifestAsset
		} else {
			manifestAssets = append(manifestAssets, manifestAsset)
		}
	}

	if launchAsset == nil {
		return nil, fmt.Errorf("no launch asset found for update %s", update.ID)
	}

	return &Manifest{
		Id:             update.ID.String(),
		CreatedAt:      update.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
		RuntimeVersion: update.RuntimeVersion,
		Assets:         manifestAssets,
		LaunchAsset:    *launchAsset,
	}, nil
}
