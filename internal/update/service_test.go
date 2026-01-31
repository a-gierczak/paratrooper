package update

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/a-gierczak/paratrooper/generated/db"
	"github.com/a-gierczak/paratrooper/internal/util"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var (
	expoProject     db.Project
	codePushProject db.Project
)

func setupFixtures(t *testing.T, ctx context.Context, dbDsn string) {
	conn, err := pgx.Connect(ctx, dbDsn)
	require.NoError(t, err)
	defer conn.Close(ctx)
	q := db.New(conn)

	expoProject, err = q.CreateProject(
		ctx,
		uuid.Must(uuid.NewV7()),
		"test_expo",
		db.UpdateProtocolExpo,
	)
	require.NoError(t, err)

	codePushProject, err = q.CreateProject(
		ctx,
		uuid.Must(uuid.NewV7()),
		"test_codepush",
		db.UpdateProtocolCodepush,
	)
	require.NoError(t, err)
}

func TestUpdateToInstall(t *testing.T) {
	ctx := context.Background()

	dbName := "test"
	dbUser := "user"
	dbPassword := "password"

	ctr, err := postgres.Run(ctx,
		"postgres:13",
		postgres.WithInitScripts(filepath.Join("..", "..", "db", "schema.sql")),
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPassword),
		postgres.BasicWaitStrategies(),
		postgres.WithSQLDriver("pgx"),
	)
	defer testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err)

	dbDsn, err := ctr.ConnectionString(ctx)
	require.NoError(t, err)

	setupFixtures(t, ctx, dbDsn)

	err = ctr.Snapshot(ctx)
	require.NoError(t, err)

	t.Run("returns nil if there are no updates", func(t *testing.T) {
		t.Cleanup(func() {
			err = ctr.Restore(ctx)
			require.NoError(t, err)
		})

		conn, err := pgx.Connect(ctx, dbDsn)
		require.NoError(t, err)
		defer conn.Close(ctx)
		q := db.New(conn)
		svc := NewService(q, nil, nil, nil)

		runtimeVersion := "1.0.0"
		channel := "production"
		platform := "ios"
		filter := CurrentUpdateFilter{}

		updates, err := svc.UpdateToInstall(
			ctx,
			expoProject.ID,
			runtimeVersion,
			channel,
			platform,
			filter,
		)
		require.NoError(t, err)
		require.Nil(t, updates)
	})

	t.Run("returns nil if published update matches CurrentUpdateID", func(t *testing.T) {
		t.Cleanup(func() {
			err = ctr.Restore(ctx)
			require.NoError(t, err)
		})

		conn, err := pgx.Connect(ctx, dbDsn)
		require.NoError(t, err)
		defer conn.Close(ctx)
		q := db.New(conn)
		svc := NewService(q, nil, nil, nil)

		updateID := uuid.Must(uuid.NewV7())

		runtimeVersion := "1.0.0"
		channel := "production"
		platform := "ios"
		filter := CurrentUpdateFilter{
			ID: &updateID,
		}

		err = q.CreateUpdate(ctx, db.CreateUpdateParams{
			ID:             updateID,
			ProjectID:      expoProject.ID,
			RuntimeVersion: runtimeVersion,
			Message:        pgtype.Text{String: "test", Valid: true},
			Channel:        channel,
		})
		require.NoError(t, err)

		updates, err := svc.UpdateToInstall(
			ctx,
			expoProject.ID,
			runtimeVersion,
			channel,
			platform,
			filter,
		)
		require.NoError(t, err)
		require.Nil(t, updates)
	})

	t.Run("returns published update with launch asset", func(t *testing.T) {
		t.Cleanup(func() {
			err = ctr.Restore(ctx)
			require.NoError(t, err)
		})

		conn, err := pgx.Connect(ctx, dbDsn)
		require.NoError(t, err)
		defer conn.Close(ctx)
		q := db.New(conn)
		svc := NewService(q, nil, nil, nil)

		updateID := uuid.Must(uuid.NewV7())

		runtimeVersion := "1.0.0"
		channel := "production"
		platform := "ios"
		filter := CurrentUpdateFilter{}

		err = q.CreateUpdate(ctx, db.CreateUpdateParams{
			ID:             updateID,
			ProjectID:      expoProject.ID,
			RuntimeVersion: runtimeVersion,
			Message:        pgtype.Text{String: "test", Valid: true},
			Channel:        channel,
		})
		require.NoError(t, err)

		assetID := uuid.Must(uuid.NewV7())
		_, err = q.CreateUpdateAssets(ctx, []db.CreateUpdateAssetsParams{
			{
				ID:                assetID,
				UpdateID:          updateID,
				StorageObjectPath: "http://localhost/some-fake-path/main.jsbundle",
				ContentType:       "application/javascript",
				Extension:         ".jsbundle",
				ContentMd5:        "md5",
				ContentSha256:     "sha256",
				IsLaunchAsset:     true,
				IsArchive:         false,
				Platform:          "ios",
				ContentLength:     123,
			},
		})
		require.NoError(t, err)

		u, err := q.SetUpdateStatus(ctx, updateID, db.UpdateStatusPublished)
		require.NoError(t, err)

		updates, err := svc.UpdateToInstall(
			ctx,
			expoProject.ID,
			runtimeVersion,
			channel,
			platform,
			filter,
		)
		require.NoError(t, err)
		require.NotNil(t, updates)
		require.Equal(t, updates.Update, u)
		require.Equal(t, updates.ContentSha256, pgtype.Text{String: "sha256", Valid: true})
	})

	t.Run("returns published update with archive asset", func(t *testing.T) {
		t.Cleanup(func() {
			err = ctr.Restore(ctx)
			require.NoError(t, err)
		})

		conn, err := pgx.Connect(ctx, dbDsn)
		require.NoError(t, err)
		defer conn.Close(ctx)
		q := db.New(conn)
		svc := NewService(q, nil, nil, nil)

		updateID := uuid.Must(uuid.NewV7())

		runtimeVersion := "1.0.0"
		channel := "production"
		platform := "ios"
		filter := CurrentUpdateFilter{}

		err = q.CreateUpdate(ctx, db.CreateUpdateParams{
			ID:             updateID,
			ProjectID:      codePushProject.ID,
			RuntimeVersion: runtimeVersion,
			Message:        pgtype.Text{String: "test", Valid: true},
			Channel:        channel,
		})
		require.NoError(t, err)

		assetID := uuid.Must(uuid.NewV7())
		_, err = q.CreateUpdateAssets(ctx, []db.CreateUpdateAssetsParams{
			{
				ID:                assetID,
				UpdateID:          updateID,
				StorageObjectPath: "http://localhost/some-fake-path/main.zip",
				ContentType:       "application/zip",
				Extension:         ".zip",
				ContentMd5:        "md5",
				ContentSha256:     "sha256",
				IsLaunchAsset:     false,
				IsArchive:         true,
				Platform:          "ios",
				ContentLength:     123,
			},
		})
		require.NoError(t, err)

		u, err := q.SetUpdateStatus(ctx, updateID, db.UpdateStatusPublished)
		require.NoError(t, err)

		updates, err := svc.UpdateToInstall(
			ctx,
			codePushProject.ID,
			runtimeVersion,
			channel,
			platform,
			filter,
		)
		require.NoError(t, err)
		require.NotNil(t, updates)
		require.Equal(t, updates.Update, u)
		require.Equal(t, updates.ContentSha256, pgtype.Text{String: "sha256", Valid: true})
	})

	t.Run(
		"should return the latest published update if newer updates were canceled",
		func(t *testing.T) {
			t.Cleanup(func() {
				err = ctr.Restore(ctx)
				require.NoError(t, err)
			})

			conn, err := pgx.Connect(ctx, dbDsn)
			require.NoError(t, err)
			defer conn.Close(ctx)
			q := db.New(conn)
			svc := NewService(q, nil, nil, nil)

			input := []struct {
				UpdateID uuid.UUID
				Status   db.UpdateStatus
			}{
				{uuid.Must(uuid.NewV7()), db.UpdateStatusPublished},
				{uuid.Must(uuid.NewV7()), db.UpdateStatusCanceled},
				{uuid.Must(uuid.NewV7()), db.UpdateStatusPublished},
				{uuid.Must(uuid.NewV7()), db.UpdateStatusCanceled},
			}

			for _, update := range input {
				err = q.CreateUpdate(ctx, db.CreateUpdateParams{
					ID:             update.UpdateID,
					ProjectID:      expoProject.ID,
					RuntimeVersion: "1.0.0",
					Channel:        "production",
				})
				require.NoError(t, err)

				_, err = q.CreateUpdateAssets(ctx, []db.CreateUpdateAssetsParams{
					{
						ID:                uuid.Must(uuid.NewV7()),
						UpdateID:          update.UpdateID,
						StorageObjectPath: "http://localhost/some-fake-path/main.jsbundle",
						ContentType:       "application/javascript",
						Extension:         ".jsbundle",
						ContentMd5:        "md5",
						ContentSha256:     "sha256",
						IsLaunchAsset:     true,
						IsArchive:         false,
						Platform:          "ios",
						ContentLength:     123,
					},
				})
				require.NoError(t, err)

				_, err = q.SetUpdateStatus(
					ctx,
					update.UpdateID,
					update.Status,
				)
				require.NoError(t, err)
			}

			updates, err := svc.UpdateToInstall(
				ctx,
				expoProject.ID,
				"1.0.0",
				"production",
				"ios",
				CurrentUpdateFilter{},
			)
			require.NoError(t, err)
			require.NotNil(t, updates)
			require.Equal(t, updates.Update.ID, input[2].UpdateID)
			require.Equal(t, updates.ContentSha256, pgtype.Text{String: "sha256", Valid: true})
		},
	)

	t.Run("should return nil if the current update is canceled", func(t *testing.T) {
		t.Cleanup(func() {
			err = ctr.Restore(ctx)
			require.NoError(t, err)
		})

		conn, err := pgx.Connect(ctx, dbDsn)
		require.NoError(t, err)
		defer conn.Close(ctx)
		q := db.New(conn)
		svc := NewService(q, nil, nil, nil)

		currentUpdateID := uuid.Must(uuid.NewV7())

		err = q.CreateUpdate(ctx, db.CreateUpdateParams{
			ID:             currentUpdateID,
			ProjectID:      expoProject.ID,
			RuntimeVersion: "1.0.0",
			Channel:        "production",
		})
		require.NoError(t, err)

		_, err = q.SetUpdateStatus(ctx, currentUpdateID, db.UpdateStatusCanceled)
		require.NoError(t, err)

		updates, err := svc.UpdateToInstall(
			ctx,
			expoProject.ID,
			"1.0.0",
			"production",
			"ios",
			CurrentUpdateFilter{},
		)
		require.NoError(t, err)
		require.Nil(t, updates)
	})

	t.Run(
		"should return current update if it's been canceled and there's no published update",
		func(t *testing.T) {
			t.Cleanup(func() {
				err = ctr.Restore(ctx)
				require.NoError(t, err)
			})

			conn, err := pgx.Connect(ctx, dbDsn)
			require.NoError(t, err)
			defer conn.Close(ctx)
			q := db.New(conn)
			svc := NewService(q, nil, nil, nil)

			currentUpdateID := uuid.Must(uuid.NewV7())

			err = q.CreateUpdate(ctx, db.CreateUpdateParams{
				ID:             currentUpdateID,
				ProjectID:      expoProject.ID,
				RuntimeVersion: "1.0.0",
				Channel:        "production",
			})
			require.NoError(t, err)

			assetID := uuid.Must(uuid.NewV7())
			_, err = q.CreateUpdateAssets(ctx, []db.CreateUpdateAssetsParams{
				{
					ID:                assetID,
					UpdateID:          currentUpdateID,
					StorageObjectPath: "http://localhost/some-fake-path/main.jsbundle",
					ContentType:       "application/javascript",
					Extension:         ".jsbundle",
					ContentMd5:        "md5",
					ContentSha256:     "sha256",
					IsLaunchAsset:     true,
					IsArchive:         false,
					Platform:          "ios",
					ContentLength:     123,
				},
			})
			require.NoError(t, err)

			_, err = q.SetUpdateStatus(ctx, currentUpdateID, db.UpdateStatusCanceled)
			require.NoError(t, err)

			// find by ID
			updates, err := svc.UpdateToInstall(
				ctx,
				expoProject.ID,
				"1.0.0",
				"production",
				"ios",
				CurrentUpdateFilter{
					ID: &currentUpdateID,
				},
			)
			require.NoError(t, err)
			require.NotNil(t, updates)
			require.Equal(t, updates.Update.ID, currentUpdateID)
			require.Equal(t, updates.ContentSha256, pgtype.Text{String: "sha256", Valid: true})

			// find by SHA256
			updates, err = svc.UpdateToInstall(
				ctx,
				expoProject.ID,
				"1.0.0",
				"production",
				"ios",
				CurrentUpdateFilter{
					SHA256: util.StringPtr("sha256"),
				},
			)
			require.NoError(t, err)
			require.NotNil(t, updates)
			require.Equal(t, updates.Update.ID, currentUpdateID)
			require.Equal(t, updates.ContentSha256, pgtype.Text{String: "sha256", Valid: true})
		},
	)

	t.Run("should prioritize archive over bundle", func(t *testing.T) {
		t.Cleanup(func() {
			err = ctr.Restore(ctx)
			require.NoError(t, err)
		})

		conn, err := pgx.Connect(ctx, dbDsn)
		require.NoError(t, err)
		defer conn.Close(ctx)
		q := db.New(conn)
		svc := NewService(q, nil, nil, nil)

		updateID := uuid.Must(uuid.NewV7())

		err = q.CreateUpdate(ctx, db.CreateUpdateParams{
			ID:             updateID,
			ProjectID:      codePushProject.ID,
			RuntimeVersion: "1.0.0",
			Channel:        "production",
		})
		require.NoError(t, err)

		_, err = q.CreateUpdateAssets(ctx, []db.CreateUpdateAssetsParams{
			{
				ID:                uuid.Must(uuid.NewV7()),
				UpdateID:          updateID,
				StorageObjectPath: "http://localhost/some-fake-path/main.jsbundle",
				ContentType:       "application/javascript",
				Extension:         ".jsbundle",
				ContentMd5:        "md5",
				ContentSha256:     "bundle_sha256",
				IsLaunchAsset:     true,
				IsArchive:         false,
				Platform:          "ios",
				ContentLength:     123,
			},
			{
				ID:                uuid.Must(uuid.NewV7()),
				UpdateID:          updateID,
				StorageObjectPath: "http://localhost/some-fake-path/main.zip",
				ContentType:       "application/zip",
				Extension:         ".zip",
				ContentMd5:        "md5",
				ContentSha256:     "archive_sha256",
				IsLaunchAsset:     false,
				IsArchive:         true,
				Platform:          "ios",
				ContentLength:     123,
			},
		})
		require.NoError(t, err)

		_, err = q.SetUpdateStatus(ctx, updateID, db.UpdateStatusPublished)
		require.NoError(t, err)

		updates, err := svc.UpdateToInstall(
			ctx,
			codePushProject.ID,
			"1.0.0",
			"production",
			"ios",
			CurrentUpdateFilter{},
		)
		require.NoError(t, err)
		require.NotNil(t, updates)
		require.Equal(t, updates.Update.ID, updateID)
		require.Equal(t, updates.ContentSha256, pgtype.Text{String: "archive_sha256", Valid: true})
	})
}
