package util

import (
	"io"

	"go.uber.org/zap"
)

func CloseWithLogger(log *zap.Logger, closer io.Closer) {
	err := closer.Close()
	if err != nil {
		log.Error("failed to close resource", zap.Error(err))
	}
}

func StringPtr(s string) *string {
	return &s
}
