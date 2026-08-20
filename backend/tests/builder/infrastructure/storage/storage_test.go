package storage_test

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/storage"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/stretchr/testify/require"
)

func TestFileManagerValidateArchiveFindsExpectedDockerfileInSupportedArchives(t *testing.T) {
	for _, tt := range []struct {
		name string
		file string
		save func(string, map[string]string) error
	}{
		{name: "zip", file: "source.zip", save: writeZip},
		{name: "tar", file: "source.tar", save: writeTar},
		{name: "tar.gz", file: "source.tar.gz", save: writeTarGz},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			archivePath := filepath.Join(dir, tt.file)
			require.NoError(t, tt.save(archivePath, map[string]string{
				"Dockerfile":              "FROM busybox\n",
				"services/api/Dockerfile": "FROM scratch\n",
			}))
			manager, err := storage.NewFileManager(dir)
			require.NoError(t, err)

			require.NoError(t, manager.ValidateArchive(archivePath, "services/api", "Dockerfile"))
			require.NoError(t, manager.ValidateArchive(archivePath, ".", ""))
			require.ErrorIs(t, manager.ValidateArchive(archivePath, "services/web", "Dockerfile"), apperrors.ErrBadRequest)
		})
	}
}

func TestFileManagerValidateArchiveRejectsInvalidCorruptAndUnsafeInputs(t *testing.T) {
	dir := t.TempDir()
	manager, err := storage.NewFileManager(dir)
	require.NoError(t, err)

	require.ErrorIs(t, manager.ValidateArchive(filepath.Join(dir, "source.rar"), ".", "Dockerfile"), apperrors.ErrInvalidFileFormat)
	require.Error(t, manager.ValidateArchive(filepath.Join(dir, "source.zip"), "../outside", "Dockerfile"))

	corruptZip := filepath.Join(dir, "corrupt.zip")
	require.NoError(t, os.WriteFile(corruptZip, []byte("not a zip"), 0644))
	require.ErrorIs(t, manager.ValidateArchive(corruptZip, ".", "Dockerfile"), apperrors.ErrBadRequest)
}

func TestFileManagerCleanUpRemovesArchive(t *testing.T) {
	dir := t.TempDir()
	manager, err := storage.NewFileManager(dir)
	require.NoError(t, err)
	path := filepath.Join(dir, "archive.zip")
	require.NoError(t, os.WriteFile(path, []byte("zip"), 0644))

	require.NoError(t, manager.CleanUp(path))
	require.NoFileExists(t, path)
}

func TestLogManagerSaveLogsSystemLogExistsAndCleanup(t *testing.T) {
	manager, err := storage.NewLogManager(t.TempDir())
	require.NoError(t, err)
	longLine := strings.Repeat("x", 70*1024)
	input := longLine + "\nnext line\n"

	logPath, err := manager.SaveLogs("build-id", strings.NewReader(input), int64(len(input)+1024))
	require.NoError(t, err)
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(data), longLine)
	require.Contains(t, string(data), "next line\n")

	require.NoError(t, manager.WriteSystemLog("build-id", "hello"))
	data, err = os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "[SYSTEM] hello")
	exists, err := manager.Exists("build-id")
	require.NoError(t, err)
	require.True(t, exists)

	require.NoError(t, manager.CleanUp("build-id"))
	exists, err = manager.Exists("build-id")
	require.NoError(t, err)
	require.False(t, exists)
	require.NoError(t, manager.CleanUp("build-id"))
}

func TestLogManagerSaveLogsReturnsLimitErrorAndMarker(t *testing.T) {
	manager, err := storage.NewLogManager(t.TempDir())
	require.NoError(t, err)

	logPath, err := manager.SaveLogs("build-id", strings.NewReader("12345\n"), 4)
	require.ErrorIs(t, err, storage.ErrLogSizeLimitExceeded)
	require.True(t, manager.IsLogSizeLimitExceeded(err))
	data, readErr := os.ReadFile(logPath)
	require.NoError(t, readErr)
	require.Contains(t, string(data), "Log size limit exceeded")
}

func TestWorkspaceManagerArchivePathAndWorkspaceCleanup(t *testing.T) {
	base := t.TempDir()
	manager, err := storage.NewWorkspaceManager(base)
	require.NoError(t, err)

	require.Equal(t, filepath.Join(base, "build-id.zip"), manager.ArchivePath("build-id", "build-archives/source.zip"))
	require.Equal(t, filepath.Join(base, "build-id.tar.gz"), manager.ArchivePath("build-id", "build-archives/source.tar.gz"))

	workspace, cleanup, err := manager.Create("build-id")
	require.NoError(t, err)
	require.DirExists(t, workspace)
	cleanup()
	require.NoDirExists(t, workspace)
}

func writeZip(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for name, body := range files {
		entry, err := w.Create(name)
		if err != nil {
			_ = w.Close()
			return err
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			_ = w.Close()
			return err
		}
	}
	return w.Close()
}

func writeTar(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := tar.NewWriter(f)
	if err := writeTarFiles(w, files); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func writeTarGz(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	defer gw.Close()
	w := tar.NewWriter(gw)
	if err := writeTarFiles(w, files); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func writeTarFiles(w *tar.Writer, files map[string]string) error {
	for name, body := range files {
		data := []byte(body)
		if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data))}); err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}
