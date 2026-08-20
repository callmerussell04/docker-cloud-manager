package archive_test

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/archive"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/stretchr/testify/require"
)

func TestExtractorExtractsZipTarAndTarGz(t *testing.T) {
	for _, tt := range []struct {
		name  string
		write func(string, map[string]string) error
		file  string
	}{
		{name: "zip", write: writeZipArchive, file: "source.zip"},
		{name: "tar", write: writeTarArchive, file: "source.tar"},
		{name: "tar.gz", write: writeTarGzArchive, file: "source.tar.gz"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			archivePath := filepath.Join(dir, tt.file)
			destDir := filepath.Join(dir, "out")
			err := tt.write(archivePath, map[string]string{
				"Dockerfile":              "FROM scratch\n",
				"services/api/main.go":    "package main\n",
				"services/api/nested.txt": "hello\n",
			})
			require.NoError(t, err)

			err = archive.NewExtractor().Extract(archivePath, destDir, 1024)
			require.NoError(t, err)
			require.FileExists(t, filepath.Join(destDir, "Dockerfile"))
			require.FileExists(t, filepath.Join(destDir, "services/api/main.go"))
			require.FileExists(t, filepath.Join(destDir, "services/api/nested.txt"))
		})
	}
}

func TestExtractorRejectsUnsupportedTraversalOversizeAndCorruptArchives(t *testing.T) {
	dir := t.TempDir()
	extractor := archive.NewExtractor()

	err := extractor.Extract(filepath.Join(dir, "source.rar"), filepath.Join(dir, "out"), 1024)
	require.ErrorIs(t, err, apperrors.ErrInvalidFileFormat)

	traversalZip := filepath.Join(dir, "traversal.zip")
	require.NoError(t, writeZipArchive(traversalZip, map[string]string{"../evil.txt": "owned"}))
	err = extractor.Extract(traversalZip, filepath.Join(dir, "zip-out"), 1024)
	require.Error(t, err)
	require.NoFileExists(t, filepath.Join(dir, "evil.txt"))

	oversizeTar := filepath.Join(dir, "oversize.tar")
	require.NoError(t, writeTarArchive(oversizeTar, map[string]string{"big.txt": "12345"}))
	err = extractor.Extract(oversizeTar, filepath.Join(dir, "oversize-out"), 4)
	require.ErrorIs(t, err, apperrors.ErrResourceExhausted)

	corruptZip := filepath.Join(dir, "corrupt.zip")
	require.NoError(t, os.WriteFile(corruptZip, []byte("not a zip"), 0644))
	err = extractor.Extract(corruptZip, filepath.Join(dir, "corrupt-out"), 1024)
	require.Error(t, err)
	require.False(t, errors.Is(err, apperrors.ErrInvalidFileFormat))
}

func writeZipArchive(path string, files map[string]string) error {
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

func writeTarArchive(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := tar.NewWriter(f)
	if err := writeTarEntries(w, files); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func writeTarGzArchive(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	if err := writeTarEntries(tw, files); err != nil {
		_ = tw.Close()
		return err
	}
	return tw.Close()
}

func writeTarEntries(w *tar.Writer, files map[string]string) error {
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
