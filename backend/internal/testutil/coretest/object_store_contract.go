package coretest

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	"github.com/stretchr/testify/require"
)

// ObjectStore is the object storage Compose and builds use: the methods
// MemoryObjects models.
type ObjectStore interface {
	UploadStream(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) error
	OpenObject(ctx context.Context, objectKey string) (io.ReadCloser, error)
	NewReaderAt(ctx context.Context, objectKey string) (io.ReaderAt, int64, error)
	CopyObject(ctx context.Context, sourceKey, destKey, contentType string) error
	StatObject(ctx context.Context, objectKey string) (objectstorage.ObjectInfo, error)
	DeleteObject(ctx context.Context, objectKey string) error
}

var (
	_ ObjectStore = (*MemoryObjects)(nil)
	_ ObjectStore = (*objectstorage.MinIOStorage)(nil)
)

// RunObjectStoreContract pins the object storage behaviour MemoryObjects
// models. newStore returns an empty store.
func RunObjectStoreContract(t *testing.T, newStore func(*testing.T) ObjectStore) {
	t.Helper()
	ctx := context.Background()
	const content = "services:\n  web:\n    image: nginx\n"

	t.Run("stored object reads back whole and by range", func(t *testing.T) {
		store := newStore(t)
		require.NoError(t, store.UploadStream(ctx, "sources/a.zip", strings.NewReader(content), int64(len(content)), "application/zip"))

		info, err := store.StatObject(ctx, "sources/a.zip")
		require.NoError(t, err)
		require.EqualValues(t, len(content), info.Size)

		rc, err := store.OpenObject(ctx, "sources/a.zip")
		require.NoError(t, err)
		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		require.Equal(t, content, string(data))

		reader, size, err := store.NewReaderAt(ctx, "sources/a.zip")
		require.NoError(t, err)
		require.EqualValues(t, len(content), size)
		part := make([]byte, 8)
		n, err := reader.ReadAt(part, 2)
		require.NoError(t, err)
		require.Equal(t, content[2:10], string(part[:n]))
	})

	t.Run("upload replaces an object", func(t *testing.T) {
		store := newStore(t)
		require.NoError(t, store.UploadStream(ctx, "k", strings.NewReader("old"), 3, ""))
		require.NoError(t, store.UploadStream(ctx, "k", strings.NewReader("newer"), 5, ""))
		info, err := store.StatObject(ctx, "k")
		require.NoError(t, err)
		require.EqualValues(t, 5, info.Size)
	})

	t.Run("copy is independent of its source", func(t *testing.T) {
		store := newStore(t)
		require.NoError(t, store.UploadStream(ctx, "src", bytes.NewReader([]byte(content)), int64(len(content)), ""))
		require.NoError(t, store.CopyObject(ctx, "src", "dst", "application/zip"))
		require.NoError(t, store.DeleteObject(ctx, "src"))

		rc, err := store.OpenObject(ctx, "dst")
		require.NoError(t, err)
		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		require.Equal(t, content, string(data))
	})

	t.Run("missing objects are not found", func(t *testing.T) {
		store := newStore(t)
		_, err := store.StatObject(ctx, "missing")
		require.ErrorIs(t, err, apperrors.ErrNotFound)
		_, err = store.OpenObject(ctx, "missing")
		require.ErrorIs(t, err, apperrors.ErrNotFound)
		_, _, err = store.NewReaderAt(ctx, "missing")
		require.ErrorIs(t, err, apperrors.ErrNotFound)
		require.ErrorIs(t, store.CopyObject(ctx, "missing", "dst", ""), apperrors.ErrNotFound)
		_, err = store.StatObject(ctx, "dst")
		require.ErrorIs(t, err, apperrors.ErrNotFound, "a failed copy writes nothing")
	})

	t.Run("delete is idempotent", func(t *testing.T) {
		store := newStore(t)
		require.NoError(t, store.UploadStream(ctx, "k", strings.NewReader("x"), 1, ""))
		require.NoError(t, store.DeleteObject(ctx, "k"))
		_, err := store.StatObject(ctx, "k")
		require.ErrorIs(t, err, apperrors.ErrNotFound)
		require.NoError(t, store.DeleteObject(ctx, "k"), "deleting a missing object succeeds")
	})
}
