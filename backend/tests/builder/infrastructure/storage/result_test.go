package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/storage"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
	"github.com/stretchr/testify/require"
)

func TestResultStoreSurvivesRestartAndRemovesAcceptedResult(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := storage.NewResultStore(dir)
	require.NoError(t, err)
	_, found, err := store.Load(ctx, "build")
	require.NoError(t, err)
	require.False(t, found)
	result := model.BuildResult{ImageID: "image", Status: "success", SizeMB: 2}
	require.NoError(t, store.Save(ctx, "build", result))
	store, err = storage.NewResultStore(dir)
	require.NoError(t, err)
	got, found, err := store.Load(ctx, "build")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, result, got)
	require.NoError(t, store.Delete(ctx, "build"))
	_, found, err = store.Load(ctx, "build")
	require.NoError(t, err)
	require.False(t, found)
	require.NoError(t, store.Delete(ctx, "build"))
}

func TestResultStoreDoesNotReadPartialWritesOrEscapeJournal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := storage.NewResultStore(dir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "results", ".result-interrupted"), []byte("{"), 0600))
	_, found, err := store.Load(ctx, "build")
	require.NoError(t, err)
	require.False(t, found)
	result := model.BuildResult{ImageID: "image", Status: "failed_internal"}
	require.NoError(t, store.Save(ctx, "../../outside", result))
	got, found, err := store.Load(ctx, "../../outside")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, result, got)
	entries, err := os.ReadDir(filepath.Join(dir, "results"))
	require.NoError(t, err)
	require.Len(t, entries, 2)
}
