package api

import (
	"asset-server/generated/api"
	"asset-server/generated/db"
	"asset-server/internal/codepush"
	"asset-server/internal/expo"
	"asset-server/internal/infra"
	"asset-server/internal/logger"
	"asset-server/internal/project"
	"asset-server/internal/storage"
	"asset-server/internal/update"
	"asset-server/internal/util"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	semver "github.com/Masterminds/semver/v3"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type apiServer struct {
	updateSvc   update.Service
	codePushSvc codepush.Service
	expoSvc     expo.Service
	projectSvc  project.Service
	infraSvc    infra.Service
}

func NewServer(
	updateSvc update.Service,
	codePushSvc codepush.Service,
	expoSvc expo.Service,
	projectSvc project.Service,
	infraSvc infra.Service,
) api.StrictServerInterface {
	return &apiServer{
		updateSvc,
		codePushSvc,
		expoSvc,
		projectSvc,
		infraSvc,
	}
}

func (srv *apiServer) projectByID(ctx context.Context, projectID uuid.UUID) (*db.Project, error) {
	if projectID == uuid.Nil {
		return nil, NewValidationError("project_id", "project id is required")
	}

	proj, err := srv.projectSvc.ProjectByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("projectSvc.ProjectByID: %w", err)
	}

	if proj == nil {
		return nil, NewNotFoundError("project not found")
	}

	return proj, nil
}

func (srv *apiServer) PrepareUpdate(
	ctx context.Context,
	request api.PrepareUpdateRequestObject,
) (api.PrepareUpdateResponseObject, error) {
	if request.Body.Channel == nil {
		request.Body.Channel = util.StringPtr(update.DefaultChannelName)
	}

	// normalize runtime version
	runtimeVersion, err := semver.NewVersion(request.Body.RuntimeVersion)
	if err != nil {
		return nil, NewValidationError("runtime_version", "invalid runtime version")
	}
	request.Body.RuntimeVersion = runtimeVersion.String()

	proj, err := srv.projectByID(ctx, request.ProjectID)
	if err != nil {
		return nil, err
	}

	updateID, uploadURLs, err := srv.updateSvc.PrepareUpdate(ctx, proj.ID, *request.Body)
	if err != nil {
		if errors.Is(err, storage.ErrUpdateTooLarge) {
			return nil, NewValidationError("file_metadata", err.Error())
		}
		return nil, fmt.Errorf("updateSvc.PrepareUpdate: %w", err)
	}

	return api.PrepareUpdate201JSONResponse(api.PrepareUpdateResponse{
		UpdateID:   updateID,
		UploadURLs: uploadURLs,
	}), nil
}

func (srv *apiServer) CommitUpdate(
	ctx context.Context,
	request api.CommitUpdateRequestObject) (api.CommitUpdateResponseObject, error) {
	proj, err := srv.projectByID(ctx, request.ProjectID)
	if err != nil {
		return nil, err
	}

	u, err := srv.updateSvc.UpdateByID(ctx, proj.ID, request.UpdateID)
	if err != nil {
		if errors.Is(err, update.ErrUpdateNotFound) {
			return nil, NewNotFoundError("update not found")
		}
		return nil, fmt.Errorf("updateSvc.UpdateByID: %w", err)
	}

	if u.ProjectID != proj.ID {
		return nil, NewNotFoundError("update not found")
	}

	err = srv.updateSvc.CommitUpdate(ctx, request.UpdateID)
	if err != nil {
		return nil, fmt.Errorf("updateSvc.CommitUpdate: %w", err)
	}

	return api.CommitUpdate204Response{}, nil
}

func (srv *apiServer) GetUpdate(
	ctx context.Context,
	request api.GetUpdateRequestObject,
) (api.GetUpdateResponseObject, error) {
	u, err := srv.updateSvc.UpdateByID(
		ctx,
		request.ProjectID,
		request.UpdateID,
	)
	if err != nil {
		if errors.Is(err, update.ErrUpdateNotFound) {
			return nil, NewNotFoundError("update not found")
		}
		return nil, err
	}

	return api.GetUpdate200JSONResponse{
		ID:             u.ID,
		Channel:        u.Channel,
		CreatedAt:      u.CreatedAt.Time.UTC().Truncate(time.Second),
		Message:        u.Message.String,
		RuntimeVersion: u.RuntimeVersion,
		Status:         api.UpdateStatus(u.Status),
	}, nil
}

func (srv *apiServer) GetUpdates(
	ctx context.Context,
	request api.GetUpdatesRequestObject,
) (api.GetUpdatesResponseObject, error) {
	proj, err := srv.projectByID(ctx, request.ProjectID)
	if err != nil {
		return nil, err
	}

	updates, err := srv.updateSvc.FindUpdates(
		ctx,
		proj.ID,
		request.Params.Status,
		request.Params.RuntimeVersion,
		request.Params.Channel,
	)

	if err != nil {
		return nil, fmt.Errorf("updateSvc.FindUpdates: %w", err)
	}

	response := make(api.GetUpdatesResponse, 0)

	for _, u := range updates {
		response = append(response, api.Update{
			ID:             u.ID,
			RuntimeVersion: u.RuntimeVersion,
			CreatedAt:      u.CreatedAt.Time.UTC().Truncate(time.Second),
			Status:         api.UpdateStatus(u.Status),
			Message:        u.Message.String,
			Channel:        u.Channel,
		})
	}

	return api.GetUpdates200JSONResponse(response), nil
}

func expoUpdateCacheKey(
	params *expoUpdateParams,
) string {
	currentUpdateIdStr := "none"
	if params.CurrentUpdateId != nil {
		currentUpdateIdStr = params.CurrentUpdateId.String()
	}

	return strings.ToLower(
		fmt.Sprintf(
			"pt:update:%s:%s:%s:%s:%s",
			params.ProjectID,
			params.Channel,
			params.RuntimeVersion,
			params.Platform,
			currentUpdateIdStr,
		),
	)
}

func (srv *apiServer) expoUpdateCachedResponse(
	ctx context.Context,
	params *expoUpdateParams,
) (*expoUpdateMultipartResponse, error) {
	cacheKey := expoUpdateCacheKey(params)
	cache := srv.infraSvc.Cache()
	cachedResponseStr, err := cache.Get(ctx, cacheKey)
	if err != nil {
		return nil, fmt.Errorf("cache.Get: %w", err)
	}

	var cachedResponse *expoUpdateMultipartResponse
	if cachedResponseStr != "" {
		err = json.Unmarshal([]byte(cachedResponseStr), &cachedResponse)
		if err != nil {
			return nil, fmt.Errorf("json.Unmarshal: %w", err)
		}
	}

	return cachedResponse, nil
}

func (srv *apiServer) expoUpdateSetCachedResponse(
	ctx context.Context,
	params *expoUpdateParams,
	response expoUpdateMultipartResponse,
) error {
	cacheKey := expoUpdateCacheKey(params)
	responseJson, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("json.Marshal: %w", err)
	}

	cache := srv.infraSvc.Cache()
	return cache.Set(ctx, cacheKey, string(responseJson), 24*60*60)
}

type expoUpdateParams struct {
	RuntimeVersion  string     `binding:"required"`
	Platform        string     `binding:"required"`
	CurrentUpdateId *uuid.UUID `binding:"omitempty"`
	Channel         string
	ProjectID       uuid.UUID
}

func expoUpdateParseParams(
	ctx context.Context,
	request api.GetExpoUpdateRequestObject,
) (*expoUpdateParams, error) {
	var params expoUpdateParams

	if request.Params.RuntimeVersion != nil {
		params.RuntimeVersion = *request.Params.RuntimeVersion
	} else if request.Params.ExpoRuntimeVersion != nil {
		params.RuntimeVersion = *request.Params.ExpoRuntimeVersion
	}

	if request.Params.Platform != nil {
		params.Platform = *request.Params.Platform
	} else if request.Params.ExpoPlatform != nil {
		params.Platform = *request.Params.ExpoPlatform
	}

	if request.Params.CurrentUpdateId != nil {
		params.CurrentUpdateId = request.Params.CurrentUpdateId
	} else if request.Params.ExpoCurrentUpdateId != nil {
		params.CurrentUpdateId = request.Params.ExpoCurrentUpdateId
	}

	if err := binding.Validator.ValidateStruct(&params); err != nil {
		return nil, err
	}

	// normalize runtime version
	{
		runtimeVersion, err := semver.NewVersion(params.RuntimeVersion)
		if err != nil {
			return nil, NewValidationError("runtime_version", "invalid runtime version")
		}
		params.RuntimeVersion = runtimeVersion.String()
	}

	params.Channel = update.DefaultChannelName
	params.ProjectID = request.ProjectID

	return &params, nil
}

func (srv *apiServer) GetExpoUpdate(
	ctx context.Context,
	request api.GetExpoUpdateRequestObject,
) (api.GetExpoUpdateResponseObject, error) {
	params, err := expoUpdateParseParams(ctx, request)
	if err != nil {
		return nil, err
	}

	log := logger.FromContext(ctx)

	log.Debug(
		"GetExpoUpdate",
		zap.Stringer("projectID", request.ProjectID),
		zap.String("runtimeVersion", params.RuntimeVersion),
		zap.String("platform", params.Platform),
		zap.Stringer("currentUpdateId", params.CurrentUpdateId),
		zap.String("channel", params.Channel),
	)

	cachedResponse, err := srv.expoUpdateCachedResponse(ctx, params)
	if err != nil {
		log.Error("failed to get cached response", zap.Error(err))
	} else if cachedResponse != nil {
		log.Debug("found cached response")
		return cachedResponse, nil
	}

	proj, err := srv.projectSvc.ProjectByID(ctx, request.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("projectSvc.ProjectByID: %w", err)
	}

	if proj == nil {
		return api.GetExpoUpdate400JSONResponse(
			NewValidationErrorResponse("project_id", "project not found"),
		), nil
	}

	if proj.UpdateProtocol != db.UpdateProtocolExpo {
		return api.GetExpoUpdate400JSONResponse(
			NewValidationErrorResponse("project_id", "project does not use Expo update protocol"),
		), nil
	}

	result, err := srv.updateSvc.UpdateToInstall(
		ctx,
		request.ProjectID,
		params.RuntimeVersion,
		params.Channel,
		params.Platform,
		update.CurrentUpdateFilter{
			ID: params.CurrentUpdateId,
		},
	)
	if err != nil && !errors.Is(err, update.ErrUpdateNotFound) {
		return nil, fmt.Errorf("updateSvc.UpdateToInstall: %w", err)
	}

	if result != nil && result.Update.Status == db.UpdateStatusPublished {
		manifest, err := srv.expoSvc.UpdateManifest(ctx, result.Update, params.Platform)
		if err != nil {
			return nil, fmt.Errorf("expoSvc.UpdateManifest: %w", err)
		}

		resp := expoUpdateMultipartResponse{"manifest", manifest}
		if err := srv.expoUpdateSetCachedResponse(ctx, params, resp); err != nil {
			log.Error("failed to cache response", zap.Error(err))
		}

		return &resp, nil
	}

	if result != nil && result.Update.Status == db.UpdateStatusCanceled {
		resp := expoUpdateMultipartResponse{
			"directive",
			gin.H{
				"type": "rollBackToEmbedded",
				"parameters": gin.H{
					"commitTime": time.Now().UTC().Format("2006-01-02T15:04:05.0Z07"),
				},
			},
		}
		if err := srv.expoUpdateSetCachedResponse(ctx, params, resp); err != nil {
			log.Error("failed to cache response", zap.Error(err))
		}
		return &resp, nil
	}

	resp := expoUpdateMultipartResponse{
		"directive",
		gin.H{"type": "noUpdateAvailable"},
	}
	if err := srv.expoUpdateSetCachedResponse(ctx, params, resp); err != nil {
		log.Error("failed to cache response", zap.Error(err))
	}
	return &resp, nil
}

func (srv *apiServer) RollbackUpdate(
	ctx context.Context,
	request api.RollbackUpdateRequestObject,
) (api.RollbackUpdateResponseObject, error) {
	log := logger.FromContext(ctx)

	err := srv.updateSvc.RollbackUpdate(ctx, request.ProjectID, request.UpdateID)
	if err != nil {
		if errors.Is(err, update.ErrUpdateNotFound) {
			log.Debug("update not found", zap.String("update_id", request.UpdateID.String()))
			return api.RollbackUpdate400JSONResponse(
				NewValidationErrorResponse("update_id", "update not found"),
			), nil
		}

		if errors.Is(err, update.ErrUpdateNotPublished) {
			log.Debug(
				"tried to rollback non-published update",
				zap.String("update_id", request.UpdateID.String()),
			)
			return api.RollbackUpdate400JSONResponse(
				NewValidationErrorResponse("update_id", "update not published"),
			), nil
		}

		log.Error("failed to rollback update", zap.Error(err))
		return nil, err
	}

	return api.RollbackUpdate204Response{}, nil
}

func (srv *apiServer) GetCodePushUpdate(
	ctx context.Context,
	request api.GetCodePushUpdateRequestObject,
) (api.GetCodePushUpdateResponseObject, error) {
	log := logger.FromContext(ctx)
	projectID, platform, channel, err := codepush.ParseDeploymentKey(request.Params.DeploymentKey)
	if err != nil {
		return api.GetCodePushUpdate400JSONResponse(
			NewValidationErrorResponse("deployment_key", "invalid deployment key"),
		), nil
	}

	appVersion, err := semver.NewVersion(request.Params.AppVersion)
	if err != nil {
		return api.GetCodePushUpdate400JSONResponse(
			NewValidationErrorResponse("app_version", "invalid app version"),
		), nil
	}

	log.Debug(
		"GetCodePushUpdate",
		zap.String("projectID", projectID.String()),
		zap.String("channel", channel),
		zap.String("platform", platform),
		zap.String("appVersion", appVersion.String()),
		zap.Stringp("packageHash", request.Params.PackageHash),
	)

	proj, err := srv.projectSvc.ProjectByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("projectSvc.ProjectByID: %w", err)
	}

	if proj == nil {
		return api.GetCodePushUpdate400JSONResponse(
			NewValidationErrorResponse("project_id", "project not found"),
		), nil
	}

	if proj.UpdateProtocol != db.UpdateProtocolCodepush {
		return api.GetCodePushUpdate400JSONResponse(
			NewValidationErrorResponse(
				"project_id",
				"project does not use CodePush update protocol",
			),
		), nil
	}

	updateToInstall, err := srv.updateSvc.UpdateToInstall(
		ctx,
		projectID,
		appVersion.String(),
		channel,
		platform,
		update.CurrentUpdateFilter{
			SHA256: request.Params.PackageHash,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("updateSvc.UpdateToInstall: %w", err)
	}

	if updateToInstall == nil {
		return api.GetCodePushUpdate200JSONResponse{
			UpdateInfo: api.CodePushUpdate{
				DownloadURL:            "",
				Description:            util.StringPtr(""),
				IsAvailable:            false,
				IsMandatory:            false,
				AppVersion:             "",
				PackageHash:            "",
				Label:                  "",
				PackageSize:            0,
				UpdateAppVersion:       false,
				ShouldRunBinaryVersion: true,
			},
		}, nil
	}

	updateInfo, err := srv.codePushSvc.UpdateToInstall(ctx, updateToInstall.Update, platform)
	if err != nil {
		return nil, fmt.Errorf("codePushSvc.UpdateToInstall: %w", err)
	}

	return api.GetCodePushUpdate200JSONResponse{
		UpdateInfo: *updateInfo,
	}, nil
}

func (srv *apiServer) CreateProject(
	ctx context.Context,
	request api.CreateProjectRequestObject,
) (api.CreateProjectResponseObject, error) {
	proj, err := srv.projectSvc.CreateProject(
		ctx,
		request.Body.Name,
		request.Body.UpdateProtocol,
	)
	if err != nil {
		return nil, fmt.Errorf("projectSvc.CreateProject: %w", err)
	}

	return api.CreateProject200JSONResponse{
		ID:             proj.ID,
		Name:           proj.Name,
		UpdateProtocol: api.UpdateProtocol(proj.UpdateProtocol),
	}, nil
}

func (srv *apiServer) GetProjectByID(
	ctx context.Context,
	request api.GetProjectByIDRequestObject,
) (api.GetProjectByIDResponseObject, error) {
	proj, err := srv.projectByID(ctx, request.ProjectID)
	if err != nil {
		return nil, err
	}

	return api.GetProjectByID200JSONResponse{
		ID:             proj.ID,
		Name:           proj.Name,
		UpdateProtocol: api.UpdateProtocol(proj.UpdateProtocol),
	}, nil
}

func (srv *apiServer) HealthCheck(
	ctx context.Context,
	_ api.HealthCheckRequestObject,
) (api.HealthCheckResponseObject, error) {
	err := srv.infraSvc.HealthCheck(ctx)
	if err != nil {
		return nil, err
	}

	return api.HealthCheck200JSONResponse{Status: "ok"}, nil
}
