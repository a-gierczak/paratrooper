package logger

import (
	"asset-server/generated/api"
	"context"

	"github.com/gin-gonic/gin"
	strictgin "github.com/oapi-codegen/runtime/strictmiddleware/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const ContextKey = "logger"

func NewLogger(isDebug bool) (*zap.Logger, error) {
	if isDebug {
		config := zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		return config.Build()
	} else {
		return zap.NewProduction()
	}
}

func NewMiddleware(log *zap.Logger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Set(ContextKey, log)
		ctx.Next()
	}
}

func ContextWithLogger(c context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(c, ContextKey, logger)
}

func FromContext(c context.Context) *zap.Logger {
	return c.Value(ContextKey).(*zap.Logger)
}

func NewOperationNameStrictMiddleware() api.StrictMiddlewareFunc {
	return func(f strictgin.StrictGinHandlerFunc, operationID string) strictgin.StrictGinHandlerFunc {
		return func(ctx *gin.Context, request interface{}) (response interface{}, err error) {
			log := FromContext(ctx)
			log = log.With(zap.String("operationID", operationID))
			ctx.Set(ContextKey, log)
			return f(ctx, request)
		}
	}
}
