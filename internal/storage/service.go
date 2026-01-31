package storage

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/url"

	"github.com/a-gierczak/paratrooper/internal/logger"
	"github.com/a-gierczak/paratrooper/internal/util"

	"gocloud.dev/blob"
)

type Service interface {
	Upload(ctx context.Context, reader io.Reader, objectKey string) error
	ReadObjectWithAttributes(
		ctx context.Context,
		objectKey string,
	) (*blob.Reader, *blob.Attributes, error)
	ObjectKeyFromURL(ctx context.Context, requestURL *url.URL) (string, error)
}

type service struct {
	storage *Storage
}

func NewService(storage *Storage) Service {
	return &service{storage}
}

func (s *service) Upload(ctx context.Context, reader io.Reader, objectKey string) error {
	// TODO: check if user has access to this update
	writer, err := s.storage.Bucket().NewWriter(ctx, objectKey, nil)
	if err != nil {
		return fmt.Errorf("failed to create object: %w", err)
	}
	log := logger.FromContext(ctx)
	defer util.CloseWithLogger(log, writer)

	if _, err := io.Copy(writer, reader); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (s *service) ObjectKeyFromURL(ctx context.Context, requestURL *url.URL) (string, error) {
	return s.storage.URLSigner().KeyFromURL(ctx, requestURL)
}

type ObjectFile interface {
	io.ReadSeekCloser
	fs.FileInfo
}

func (s *service) ReadObjectWithAttributes(
	ctx context.Context,
	objectKey string,
) (*blob.Reader, *blob.Attributes, error) {
	attrs, err := s.storage.bucket.Attributes(ctx, objectKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read object attributes: %w", err)
	}

	reader, err := s.storage.Bucket().NewReader(ctx, objectKey, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create object reader: %w", err)
	}

	return reader, attrs, nil
}
