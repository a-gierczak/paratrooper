-- name: GetLatestPublishedAndCanceledUpdates :many
select distinct on (updates.status) sqlc.embed(updates), asset.content_sha256
from updates
         left join update_assets asset
                   on updates.id = asset.update_id and
                      asset.platform = sqlc.arg(platform) and
                      (asset.is_launch_asset = true or asset.is_archive = true)
where updates.project_id = sqlc.arg(project_id)
  and updates.runtime_version = sqlc.arg(runtime_version)
  and updates.channel = sqlc.arg(channel)
  and updates.status in ('published', 'canceled')
order by updates.status,
         case
             when asset.is_archive = true then 1 -- select archive asset if exists
             else 2
             end,
         updates.created_at desc;

-- name: GetUpdateByID :one
select *
from updates
where id = sqlc.arg(update_id)
  and project_id = sqlc.arg(project_id)
limit 1;

-- name: GetUpdateByIDWithProtocol :one
select u.*, p.update_protocol as protocol
from updates u
         inner join projects p on u.project_id = p.id
where u.id = sqlc.arg(update_id)
limit 1;


-- name: SetUpdateStatus :one
UPDATE updates
SET status = $2
WHERE id = $1
RETURNING *;

-- name: CreateUpdate :exec
INSERT INTO updates (id,
                     project_id,
                     runtime_version,
                     message,
                     channel,
                     status,
                     created_at)
VALUES ($1, $2, $3, $4, $5, 'empty', current_timestamp);

-- name: CreateUpdateAssets :copyfrom
INSERT INTO update_assets (id,
                           update_id,
                           storage_object_path,
                           content_type,
                           extension,
                           content_md5,
                           content_sha256,
                           is_launch_asset,
                           is_archive,
                           platform,
                           content_length)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: CreateUpdateMetadata :exec
INSERT INTO update_metadata (id,
                             update_id,
                             expo_app_config,
                             created_at)
VALUES ($1, $2, $3, current_timestamp);

-- name: GetUpdateAssetsByPlatform :many
select *
from update_assets
where update_id = $1
  and platform = $2
  and is_archive = false;

-- name: GetLaunchAssetOrArchiveByPlatform :one
select *
from update_assets
where update_id = $1
  and (is_launch_asset = true or is_archive = true)
  and platform = $2
order by is_archive desc, is_launch_asset desc
limit 1;

-- name: GetLastNUpdates :many
SELECT *
FROM updates
WHERE project_id = @project_id
  AND (runtime_version = sqlc.narg('runtime_version') OR sqlc.narg('runtime_version') IS NULL)
  AND (status = sqlc.narg(status) OR sqlc.narg(status) IS NULL)
  AND (channel = sqlc.narg(channel) OR sqlc.narg(channel) IS NULL)
ORDER BY created_at DESC
LIMIT $1;
