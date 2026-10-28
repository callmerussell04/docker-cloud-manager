package storage_test

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/storage"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
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

	require.NoError(t, manager.WriteSystemLog(context.Background(), "build-id", "hello", int64(len(input)+1024)))
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

func TestLogManagerSaveLogsNeverWritesBeyondLimit(t *testing.T) {
	manager, err := storage.NewLogManager(t.TempDir())
	require.NoError(t, err)

	logPath, err := manager.SaveLogs("build-id", strings.NewReader("12345\n"), 4)
	require.ErrorIs(t, err, model.ErrBuildLogSizeLimitExceeded)
	require.True(t, manager.IsLogSizeLimitExceeded(err))
	data, readErr := os.ReadFile(logPath)
	require.NoError(t, readErr)
	require.Equal(t, "1234", string(data))
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

func writeZip(path string, files map[string]string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
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

func writeTar(path string, files map[string]string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	w := tar.NewWriter(f)
	if err := writeTarFiles(w, files); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func writeTarGz(path string, files map[string]string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	gw := gzip.NewWriter(f)
	defer func() { err = errors.Join(err, gw.Close()) }()
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

func TestLogManagerSaveLogsBoundsNewlineFreeInput(t *testing.T) {
	manager, err := storage.NewLogManager(t.TempDir())
	require.NoError(t, err)
	source := &countedOutput{remaining: 1 << 30}
	path, err := manager.SaveLogs("huge", source, 1024)
	require.ErrorIs(t, err, model.ErrBuildLogSizeLimitExceeded)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, int64(1024), info.Size())
	require.LessOrEqual(t, source.read, int64(1025))
}

type countedOutput struct{ remaining, read int64 }

func (r *countedOutput) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.remaining)
	for i := range p[:n] {
		p[i] = 'x'
	}
	r.remaining -= n
	r.read += n
	return int(n), nil
}

func TestWriteLogsPreservesWriteShortWriteReadAndCloseErrors(t *testing.T) {
	writeErr := errors.New("write failed")
	closeErr := errors.New("close failed")
	for _, tt := range []struct {
		name   string
		source io.Reader
		writer *faultLogWriter
		want   error
		also   error
	}{
		{name: "write", source: strings.NewReader("hello"), writer: &faultLogWriter{writeErr: writeErr}, want: writeErr},
		{name: "short write", source: strings.NewReader("hello"), writer: &faultLogWriter{short: true}, want: io.ErrShortWrite},
		{name: "close", source: strings.NewReader("hello"), writer: &faultLogWriter{closeErr: closeErr}, want: closeErr},
		{name: "write and close", source: strings.NewReader("hello"), writer: &faultLogWriter{writeErr: writeErr, closeErr: closeErr}, want: writeErr, also: closeErr},
		{name: "read", source: &failedLogReader{err: io.ErrUnexpectedEOF}, writer: &faultLogWriter{}, want: io.ErrUnexpectedEOF},
		{name: "limit and close", source: strings.NewReader("12345678901"), writer: &faultLogWriter{closeErr: closeErr}, want: model.ErrBuildLogSizeLimitExceeded, also: closeErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.WriteLogs(tt.writer, tt.source, 10)
			require.ErrorIs(t, err, tt.want)
			if tt.also != nil {
				require.ErrorIs(t, err, tt.also)
			}
			require.True(t, tt.writer.closed)
			require.LessOrEqual(t, tt.writer.written, 10)
		})
	}
}

func TestLogManagerSystemMessagesRespectRemainingBudget(t *testing.T) {
	manager, err := storage.NewLogManager(t.TempDir())
	require.NoError(t, err)
	path, err := manager.SaveLogs("build", strings.NewReader("1234"), 4)
	require.NoError(t, err)
	require.ErrorIs(t, manager.WriteSystemLog(context.Background(), "build", "failure", 4), model.ErrBuildLogSizeLimitExceeded)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "1234", string(data))
}

// Fault injection at the storage stream boundary; a real file cannot reliably
// produce short writes or close failures in an isolated test.
type faultLogWriter struct {
	writeErr, closeErr error
	short, closed      bool
	written            int
}

func (w *faultLogWriter) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	n := len(p)
	if w.short {
		n--
	}
	w.written += n
	return n, nil
}
func (w *faultLogWriter) Close() error { w.closed = true; return w.closeErr }

type failedLogReader struct{ err error }

func (r *failedLogReader) Read(p []byte) (int, error) { return 0, r.err }

func TestLogManagerSystemMessageHonoursCancellation(t *testing.T) {
	manager, err := storage.NewLogManager(t.TempDir())
	require.NoError(t, err)
	path, err := manager.SaveLogs("build", strings.NewReader("log\n"), 1024)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, manager.WriteSystemLog(ctx, "build", "failure", 1024), context.Canceled)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "log\n", string(data))
}
