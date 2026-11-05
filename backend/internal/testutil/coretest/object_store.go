package coretest

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
)

// MemoryObjects is in-memory object storage serving both the Compose
// orchestrator and the build service.
type MemoryObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func NewMemoryObjects() *MemoryObjects {
	return &MemoryObjects{objects: make(map[string][]byte)}
}

// Has reports whether key is stored.
func (m *MemoryObjects) Has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok
}

// Put stores data at key, as another component writing the store would.
func (m *MemoryObjects) Put(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = append([]byte(nil), data...)
}

func (m *MemoryObjects) get(key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, apperrors.ErrNotFound
	}
	return data, nil
}

func (m *MemoryObjects) UploadStream(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.Put(key, data)
	return nil
}

func (m *MemoryObjects) NewReaderAt(_ context.Context, key string) (io.ReaderAt, int64, error) {
	data, err := m.get(key)
	if err != nil {
		return nil, 0, err
	}
	return bytes.NewReader(data), int64(len(data)), nil
}

func (m *MemoryObjects) OpenObject(_ context.Context, key string) (io.ReadCloser, error) {
	data, err := m.get(key)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *MemoryObjects) CopyObject(_ context.Context, sourceKey, destKey, _ string) error {
	data, err := m.get(sourceKey)
	if err != nil {
		return err
	}
	m.Put(destKey, data)
	return nil
}

func (m *MemoryObjects) StatObject(_ context.Context, key string) (objectstorage.ObjectInfo, error) {
	data, err := m.get(key)
	if err != nil {
		return objectstorage.ObjectInfo{}, err
	}
	return objectstorage.ObjectInfo{Size: int64(len(data))}, nil
}

func (m *MemoryObjects) DeleteObject(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}
