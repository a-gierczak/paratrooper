package api

import (
	"asset-server/internal/logger"
	"asset-server/internal/storage"
	"asset-server/internal/util"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"go.uber.org/zap"
)

type uploadAssetParams struct {
	ProjectID     string `binding:"required,uuid"`
	UpdateID      string `binding:"required,uuid"`
	Path          string `binding:"required,asset_path"`
	ContentLength int64  `binding:"required,min=1,max_object_size"`
}

func handleGetAsset(svc storage.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		log := logger.FromContext(ctx)
		objectKey, err := svc.ObjectKeyFromURL(ctx, ctx.Request.URL)
		if err != nil {
			ctx.Error(&HTTPError{
				StatusCode: http.StatusUnauthorized,
				Message:    "failed to get object key from URL",
				Inner:      err,
			})
			return
		}

		reader, attrs, err := svc.ReadObjectWithAttributes(ctx, objectKey)
		if err != nil {
			ctx.Error(err)
			return
		}
		defer util.CloseWithLogger(log, reader)

		ctx.DataFromReader(
			http.StatusOK,
			reader.Size(),
			attrs.ContentType,
			reader,
			nil,
		)
	}
}

func handleUploadAsset(svc storage.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		log := logger.FromContext(ctx)

		objectKey, err := svc.ObjectKeyFromURL(ctx, ctx.Request.URL)
		if err != nil {
			ctx.Error(&HTTPError{
				StatusCode: http.StatusUnauthorized,
				Message:    "failed to get object key from URL",
				Inner:      err,
			})
			return
		}

		var params uploadAssetParams
		params.ProjectID, params.UpdateID, params.Path = storage.AssetObjectKeySegments(objectKey)
		params.ContentLength = ctx.Request.ContentLength
		params.Path = storage.CleanPath(params.Path)

		if err := binding.Validator.ValidateStruct(&params); err != nil {
			ctx.Error(err)
			return
		}

		log = log.With(zap.String("object", objectKey),
			zap.Int64("size", params.ContentLength))

		log.Debug("saving file to local storage")
		if err = svc.Upload(ctx, ctx.Request.Body, objectKey); err != nil {
			log.Error("failed to save file to local storage", zap.Error(err))
			ctx.Error(err)
			return
		}
		log.Debug("file saved to local storage")

		ctx.JSON(http.StatusOK, nil)
	}
}

func addStorageRoutes(r gin.IRoutes, st *storage.Storage) {
	svc := storage.NewService(st)

	r.GET(storage.AssetEndpointPath, handleGetAsset(svc))
	r.PUT(storage.AssetEndpointPath, handleUploadAsset(svc))
}
