package gitsource_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/gitsource"
	"github.com/stretchr/testify/require"
)

func TestValidateRepoURL(t *testing.T) {
	t.Parallel()

	allowed := []string{"github.com", "gitlab.com"}
	info, err := gitsource.ValidateRepoURL("https://github.com/acme/app.git", allowed)
	require.NoError(t, err)
	require.Equal(t, "github.com", info.Host)
	require.Equal(t, "acme/app.git", info.Path)

	tests := []string{
		"http://github.com/acme/app.git",
		"https://token@github.com/acme/app.git",
		"https://github.com:8443/acme/app.git",
		"https://example.com/acme/app.git",
		"https://github.com/acme/../app.git",
		"https://github.com/acme/app.git?ref=main",
	}
	for _, raw := range tests {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			_, err := gitsource.ValidateRepoURL(raw, allowed)
			require.Error(t, err)
		})
	}
}

func TestValidateRefAndRelativePath(t *testing.T) {
	t.Parallel()

	require.NoError(t, gitsource.ValidateRef("feature/add-git"))
	for _, ref := range []string{"-main", "feature..bad", "bad@{ref", "release.lock", "bad ref"} {
		require.Error(t, gitsource.ValidateRef(ref))
	}

	require.NoError(t, gitsource.ValidateRelativePath("services/api/Dockerfile"))
	for _, path := range []string{"/etc/passwd", "../app", "app/../../secret", `app\Dockerfile`} {
		require.Error(t, gitsource.ValidateRelativePath(path))
	}
}

func TestArchiveToZipExcludesGitAndAppliesLimits(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".git", "config"), []byte("secret"))
	mustWriteFile(t, filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"))
	mustWriteFile(t, filepath.Join(dir, "app", "main.go"), []byte("package main\n"))

	var buf bytes.Buffer
	stats, err := gitsource.ArchiveToZip(context.Background(), dir, &buf, gitsource.ArchiveLimits{
		MaxRepositoryBytes: 1024 * 1024,
		MaxArchiveBytes:    1024 * 1024,
	})
	require.NoError(t, err)
	require.NotZero(t, stats.RepositoryBytes)
	require.NotZero(t, stats.ArchiveBytes)

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, f := range zr.File {
		seen[f.Name] = true
		require.NotEqual(t, ".git/config", f.Name)
	}
	require.True(t, seen["Dockerfile"])
	require.True(t, seen["app/main.go"])

	var limited bytes.Buffer
	_, err = gitsource.ArchiveToZip(context.Background(), dir, &limited, gitsource.ArchiveLimits{
		MaxRepositoryBytes: 1,
		MaxArchiveBytes:    1024 * 1024,
	})
	require.True(t, errors.Is(err, apperrors.ErrBadRequest))
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, data, 0644))
}
