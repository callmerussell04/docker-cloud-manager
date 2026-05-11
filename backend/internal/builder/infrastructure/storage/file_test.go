package storage

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestFileManagerValidateArchiveRequiresExpectedDockerfilePath(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "source.zip")
	if err := writeZip(archivePath, map[string]string{
		"services/api/Dockerfile": "FROM scratch\n",
		"Dockerfile":              "FROM busybox\n",
	}); err != nil {
		t.Fatalf("failed to write zip: %v", err)
	}

	manager, err := NewFileManager(dir)
	if err != nil {
		t.Fatalf("NewFileManager returned error: %v", err)
	}
	if err := manager.ValidateArchive(archivePath, "services/api", "Dockerfile"); err != nil {
		t.Fatalf("ValidateArchive returned error for expected dockerfile: %v", err)
	}
	if err := manager.ValidateArchive(archivePath, "services/web", "Dockerfile"); err == nil {
		t.Fatal("ValidateArchive returned nil for missing expected dockerfile")
	}
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
