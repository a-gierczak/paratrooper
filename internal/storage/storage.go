package storage

import (
	"asset-server/generated/api"
	"asset-server/internal/logger"
	"asset-server/internal/util"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gocloud.dev/blob"
	"gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
	_ "gocloud.dev/blob/s3blob"
)

// TODO: test validation
type Config struct {
	LocalPath     string `env:"STORAGE_LOCAL_PATH,default=assets"`
	SecretKeyPath string `env:"STORAGE_LOCAL_SECRET_KEY_PATH"     validate:"required_with=LocalPath"`
	ApiPublicURL  string `env:"API_PUBLIC_URL"                    validate:"required_with=LocalPath"`
	DriverURL     string `env:"STORAGE_DRIVER_URL"                validate:"excluded_with=LocalPath"`
}

const (
	ProviderLocal    = "local"
	ProviderExternal = "external"
)
const UploadURLExpiry = 15 * time.Minute
const DownloadURLExpiry = 30 * time.Minute
const MaxUpdateTotalSizeMB = 100

// AssetEndpointPath only relevant for local & memory storage
const AssetEndpointPath = "/assets"

var ErrUpdateTooLarge = fmt.Errorf("max update size is %dMB", MaxUpdateTotalSizeMB)

type Storage struct {
	provider  string
	bucket    *blob.Bucket
	localPath string
	// used only in local storage
	urlSigner fileblob.URLSigner
}

func cleanLocalPath(localPath string) string {
	localPath = path.Clean(localPath)
	if !path.IsAbs(localPath) {
		localPath = "./" + localPath
	}
	return localPath
}

func generateSecretKeyFile(ctx context.Context, path string) error {
	log := logger.FromContext(ctx)
	key := make([]byte, 32)
	_, err := rand.Read(key)
	if err != nil {
		return fmt.Errorf("failed to generate random bytes: %w", err)
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		if os.IsExist(err) {
			log.Info("found secret key file")
			return nil
		}
		return fmt.Errorf("failed to open file: %w", err)
	}

	defer util.CloseWithLogger(log, file)

	_, err = file.Write(key)
	if err != nil {
		return fmt.Errorf("failed to write key to file: %w", err)
	}

	log.Info("generated secret key file")
	return nil
}

func Init(ctx context.Context, config *Config) (*Storage, error) {
	err := binding.Validator.ValidateStruct(config)
	if err != nil {
		return nil, err
	}

	log := logger.FromContext(ctx)

	if err := RegisterValidators(); err != nil {
		return nil, fmt.Errorf("failed to register storage validators: %w", err)
	}

	if config.DriverURL != "" {
		storage := Storage{provider: ProviderExternal}
		bucket, err := blob.OpenBucket(ctx, config.DriverURL)
		if err != nil {
			return nil, fmt.Errorf("failed to open cloud storage bucket: %w", err)
		}
		storage.bucket = bucket
		log.Info("initialized external storage")
		return &storage, nil
	} else if config.LocalPath != "" {
		storage := Storage{provider: ProviderLocal}
		storage.localPath = cleanLocalPath(config.LocalPath)

		// generate secret key file if it doesn't exist
		if config.SecretKeyPath != "" {
			err := generateSecretKeyFile(ctx, config.SecretKeyPath)
			if err != nil {
				return nil, fmt.Errorf("failed to generate secret key file: %w", err)
			}
		}

		storage.urlSigner, err = newLocalURLSigner(config.ApiPublicURL, config.SecretKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create URL signer: %w", err)
		}

		bucket, err := fileblob.OpenBucket(storage.localPath, &fileblob.Options{
			URLSigner: storage.urlSigner,
			CreateDir: true,
			NoTempDir: true,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to open local storage bucket: %w", err)
		}
		storage.bucket = bucket
		log.Info("initialized local storage", zap.String("path", storage.localPath))
		return &storage, nil
	}

	return nil, errors.New("you must provide either local path or driver URL")
}

func (s *Storage) LocalDirPath() string {
	if s.provider == ProviderLocal {
		return s.localPath
	}

	panic("LocalDirPath called on non-local storage")
}

func CleanPath(path string) string {
	return filepath.Clean(filepath.ToSlash(path))
}

func AssetObjectKey(projectID uuid.UUID, updateId uuid.UUID, path string) string {
	return fmt.Sprintf("%s/%s/%s", projectID, updateId, path)
}

func ArchiveObjectKey(projectID uuid.UUID, updateId uuid.UUID, platform string) string {
	return fmt.Sprintf("%s/archives/%s/%s.zip", projectID, updateId, platform)
}

func AssetObjectKeySegments(assetObjectKey string) (projectID, updateID, path string) {
	segments := strings.SplitN(assetObjectKey, "/", 3)
	if len(segments) != 3 {
		return "", "", ""
	}
	path, _ = strings.CutPrefix(segments[2], "/")
	return segments[0], segments[1], path
}

func (s *Storage) UploadURLs(
	ctx context.Context,
	projectID uuid.UUID,
	updateID uuid.UUID,
	objects []api.StorageObject,
) ([]api.StorageObjectPathWithURL, error) {
	totalSize := 0
	for _, object := range objects {
		totalSize += object.ContentLength
	}
	if totalSize > MaxUpdateTotalSizeMB*1024*1024 {
		return nil, ErrUpdateTooLarge
	}

	log := logger.FromContext(ctx)
	urls := make([]api.StorageObjectPathWithURL, 0, len(objects))
	for _, object := range objects {
		cleanPath := CleanPath(object.Path)
		objectKey := AssetObjectKey(projectID, updateID, cleanPath)
		log.Info(
			"creating singed url for upload",
			zap.String("object", objectKey),
			zap.String("content_type", object.ContentType),
		)
		url, err := s.bucket.SignedURL(ctx, objectKey, &blob.SignedURLOptions{
			Method:      "PUT",
			Expiry:      UploadURLExpiry,
			ContentType: object.ContentType,
		})

		if err != nil {
			err = fmt.Errorf("failed to get upload URL: %w", err)
			log.Error(err.Error(), zap.String("object", object.Path))
			return nil, err
		}
		urls = append(urls, api.StorageObjectPathWithURL{Path: object.Path, Url: url})
	}
	return urls, nil
}

func (s *Storage) Provider() string {
	return s.provider
}

func (s *Storage) Bucket() *blob.Bucket {
	return s.bucket
}

func (s *Storage) URLSigner() fileblob.URLSigner {
	return s.urlSigner
}

// use the same logic as fileblob.OpenBucket, but we need to do it manually
// because they don't expose the URLSigner
func newLocalURLSigner(apiPublicURL, secretKeyPath string) (fileblob.URLSigner, error) {
	baseURL, err := url.JoinPath(apiPublicURL, AssetEndpointPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create URL: %w", err)
	}

	if (baseURL == "") != (secretKeyPath == "") {
		return nil, errors.New("must supply both base_url and secret_key_path query parameters")
	}

	burl, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL: %w", err)
	}
	sk, err := os.ReadFile(secretKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read secret key file: %w", err)
	}
	return fileblob.NewURLSignerHMAC(burl, sk), nil
}
