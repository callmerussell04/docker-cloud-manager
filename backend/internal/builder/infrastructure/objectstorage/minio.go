package objectstorage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

type MinIOStorage struct {
	client *minio.Client
	bucket string
}

func NewMinIOStorage(ctx context.Context, cfg Config) (*MinIOStorage, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}

	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, err
		}
	}

	return &MinIOStorage{client: client, bucket: cfg.Bucket}, nil
}

func (s *MinIOStorage) UploadFile(ctx context.Context, objectKey, filePath, contentType string) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err := s.client.FPutObject(ctx, s.bucket, objectKey, filePath, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("failed to upload object %s: %w", objectKey, err)
	}
	return nil
}

func (s *MinIOStorage) UploadStream(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err := s.client.PutObject(ctx, s.bucket, objectKey, reader, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("failed to upload object %s: %w", objectKey, err)
	}
	return nil
}

func (s *MinIOStorage) DownloadFile(ctx context.Context, objectKey, filePath string) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}
	if err := s.client.FGetObject(ctx, s.bucket, objectKey, filePath, minio.GetObjectOptions{}); err != nil {
		return mapObjectStorageError(err)
	}
	return nil
}

func (s *MinIOStorage) DeleteObject(ctx context.Context, objectKey string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, objectKey, minio.RemoveObjectOptions{}); err != nil {
		return mapObjectStorageError(err)
	}
	return nil
}

func (s *MinIOStorage) OpenObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, mapObjectStorageError(err)
	}
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, mapObjectStorageError(err)
	}
	return obj, nil
}

func mapObjectStorageError(err error) error {
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket" || resp.StatusCode == 404 {
		return apperrors.ErrNotFound
	}
	return err
}
