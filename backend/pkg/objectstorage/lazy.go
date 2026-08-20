package objectstorage

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type LazyStorage struct {
	cfg Config

	mu      sync.Mutex
	storage *MinIOStorage
}

type ObjectInfo struct {
	Size int64
}

func NewLazyStorage(cfg Config) *LazyStorage {
	return &LazyStorage{cfg: cfg}
}

func (s *LazyStorage) get(ctx context.Context) (*MinIOStorage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.storage != nil {
		return s.storage, nil
	}
	storage, err := NewMinIOStorage(ctx, s.cfg)
	if err != nil {
		return nil, apperrors.Wrap(apperrors.ErrUnavailable, "object storage is unavailable", fmt.Errorf("failed to initialize object storage: %w", err))
	}
	s.storage = storage
	return storage, nil
}

func (s *LazyStorage) UploadFile(ctx context.Context, objectKey, filePath, contentType string) error {
	storage, err := s.get(ctx)
	if err != nil {
		return err
	}
	return storage.UploadFile(ctx, objectKey, filePath, contentType)
}

func (s *LazyStorage) UploadStream(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) error {
	storage, err := s.get(ctx)
	if err != nil {
		return err
	}
	return storage.UploadStream(ctx, objectKey, reader, size, contentType)
}

func (s *LazyStorage) CopyObject(ctx context.Context, sourceKey, destKey, contentType string) error {
	storage, err := s.get(ctx)
	if err != nil {
		return err
	}
	return storage.CopyObject(ctx, sourceKey, destKey, contentType)
}

func (s *LazyStorage) StatObject(ctx context.Context, objectKey string) (ObjectInfo, error) {
	storage, err := s.get(ctx)
	if err != nil {
		return ObjectInfo{}, err
	}
	return storage.StatObject(ctx, objectKey)
}

func (s *LazyStorage) DownloadFile(ctx context.Context, objectKey, filePath string) error {
	storage, err := s.get(ctx)
	if err != nil {
		return err
	}
	return storage.DownloadFile(ctx, objectKey, filePath)
}

func (s *LazyStorage) DeleteObject(ctx context.Context, objectKey string) error {
	storage, err := s.get(ctx)
	if err != nil {
		return err
	}
	return storage.DeleteObject(ctx, objectKey)
}

func (s *LazyStorage) OpenObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	storage, err := s.get(ctx)
	if err != nil {
		return nil, err
	}
	return storage.OpenObject(ctx, objectKey)
}

func (s *LazyStorage) NewReaderAt(ctx context.Context, objectKey string) (io.ReaderAt, int64, error) {
	storage, err := s.get(ctx)
	if err != nil {
		return nil, 0, err
	}
	return storage.NewReaderAt(ctx, objectKey)
}
