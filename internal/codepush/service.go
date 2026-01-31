package codepush

import (
	"asset-server/generated/api"
	"asset-server/generated/db"
	"asset-server/internal/storage"
	"context"
	"fmt"
	"gocloud.dev/blob"
)

type Service interface {
	UpdateToInstall(
		ctx context.Context,
		update db.Update,
		platform string,
	) (*api.CodePushUpdate, error)
}

type service struct {
	q       *db.Queries
	storage *storage.Storage
}

func NewService(q *db.Queries, st *storage.Storage) Service {
	return &service{q, st}
}

func (svc *service) UpdateToInstall(
	ctx context.Context,
	update db.Update,
	platform string,
) (*api.CodePushUpdate, error) {
	asset, err := svc.q.GetLaunchAssetOrArchiveByPlatform(ctx, update.ID, platform)
	if err != nil {
		return nil, fmt.Errorf("failed to get asset from db: %w", err)
	}

	assetURL, err := svc.storage.Bucket().
		SignedURL(ctx, asset.StorageObjectPath, &blob.SignedURLOptions{
			Method: "GET",
			Expiry: storage.DownloadURLExpiry,
		})

	if err != nil {
		return nil, fmt.Errorf("failed to sign asset download URL: %w", err)
	}

	return &api.CodePushUpdate{
		AppVersion:             update.RuntimeVersion,
		Description:            &update.Message.String,
		DownloadURL:            assetURL,
		IsAvailable:            true,
		IsMandatory:            true,
		Label:                  update.ID.String(),
		PackageHash:            asset.ContentSha256,
		PackageSize:            int(asset.ContentLength),
		ShouldRunBinaryVersion: false,
		TargetBinaryRange:      update.RuntimeVersion,
		UpdateAppVersion:       false,
	}, nil
}
