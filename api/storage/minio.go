package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/aidashboard/api/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinioStorage struct {
	client *minio.Client
	bucket string
}

func NewMinioStorage(cfg *config.Config) (*MinioStorage, error) {
	return newMinioStorage(cfg, true)
}

// NewMinioStorageReadOnly opens an existing bucket without creating or modifying it.
func NewMinioStorageReadOnly(cfg *config.Config) (*MinioStorage, error) {
	return newMinioStorage(cfg, false)
}

func newMinioStorage(cfg *config.Config, createBucket bool) (*MinioStorage, error) {
	client, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client init: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if createBucket {
		err = client.MakeBucket(ctx, cfg.MinioBucket, minio.MakeBucketOptions{})
		if err != nil {
			exists, errCheck := client.BucketExists(ctx, cfg.MinioBucket)
			if errCheck != nil || !exists {
				return nil, fmt.Errorf("minio bucket create: %w", err)
			}
		} else {
			log.Printf("Created MinIO bucket: %s", cfg.MinioBucket)
		}
	} else {
		exists, err := client.BucketExists(ctx, cfg.MinioBucket)
		if err != nil {
			return nil, fmt.Errorf("minio bucket check: %w", err)
		}
		if !exists {
			return nil, fmt.Errorf("minio bucket %s does not exist", cfg.MinioBucket)
		}
	}

	return &MinioStorage{
		client: client,
		bucket: cfg.MinioBucket,
	}, nil
}

func (s *MinioStorage) Upload(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, objectName, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("minio upload %s: %w", objectName, err)
	}
	return nil
}

func (s *MinioStorage) PutVerified(ctx context.Context, objectName string, reader io.Reader, size int64, expectedSHA256 string) error {
	if size <= 0 || len(expectedSHA256) != sha256.Size*2 || expectedSHA256 != strings.ToLower(expectedSHA256) {
		return errors.New("invalid verified upload metadata")
	}
	if _, err := hex.DecodeString(expectedSHA256); err != nil {
		return errors.New("invalid verified upload hash")
	}

	hasher := sha256.New()
	info, err := s.client.PutObject(ctx, s.bucket, objectName, io.TeeReader(reader, hasher), size, minio.PutObjectOptions{
		ContentType: "application/x-jsonlines",
	})
	if err != nil {
		return fmt.Errorf("minio verified upload %s: %w", objectName, err)
	}
	cleanup := func() {
		_ = s.client.RemoveObject(context.Background(), s.bucket, objectName, minio.RemoveObjectOptions{})
	}

	var trailing [1]byte
	n, trailingErr := io.ReadFull(reader, trailing[:])
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if info.Size != size || actualHash != expectedSHA256 || n != 0 || (trailingErr != nil && !errors.Is(trailingErr, io.EOF)) {
		cleanup()
		return fmt.Errorf("minio verified upload %s failed size/hash validation", objectName)
	}
	stat, err := s.client.StatObject(ctx, s.bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		cleanup()
		return fmt.Errorf("minio verified upload %s stat: %w", objectName, err)
	}
	if stat.Size != size {
		cleanup()
		return fmt.Errorf("minio verified upload %s stored size mismatch", objectName)
	}
	stored, err := s.client.GetObject(ctx, s.bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		cleanup()
		return fmt.Errorf("minio verified upload %s open: %w", objectName, err)
	}
	readable := make([]byte, 1)
	_, readErr := io.ReadFull(stored, readable)
	closeErr := stored.Close()
	if readErr != nil || closeErr != nil {
		cleanup()
		return fmt.Errorf("minio verified upload %s readability check failed", objectName)
	}
	return nil
}

func (s *MinioStorage) Delete(ctx context.Context, objectName string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, objectName, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("minio delete %s: %w", objectName, err)
	}
	return nil
}

func (s *MinioStorage) Download(ctx context.Context, objectName string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("minio download %s: %w", objectName, err)
	}
	return obj, nil
}

func (s *MinioStorage) HealthCheck(ctx context.Context) error {
	_, err := s.client.BucketExists(ctx, s.bucket)
	return err
}

var _ interface {
	Upload(context.Context, string, io.Reader, int64, string) error
	PutVerified(context.Context, string, io.Reader, int64, string) error
} = (*MinioStorage)(nil)
