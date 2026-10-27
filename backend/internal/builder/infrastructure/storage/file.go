package storage

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/gitsource"
)

type FileManager struct {
	baseDir string
}

func NewFileManager(baseDir string) (*FileManager, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &FileManager{
		baseDir: baseDir,
	}, nil
}

func (fm *FileManager) ValidateArchive(filePath, contextDir, dockerfile string) error {
	ext := strings.ToLower(filepath.Ext(filePath))
	if strings.HasSuffix(strings.ToLower(filePath), ".tar.gz") {
		ext = ".tar.gz"
	}
	expectedDockerfile, err := expectedDockerfilePath(contextDir, dockerfile)
	if err != nil {
		return err
	}

	switch ext {
	case ".zip":
		return fm.checkZip(filePath, expectedDockerfile)
	case ".tar":
		return fm.checkTar(filePath, expectedDockerfile)
	case ".tar.gz", ".tgz":
		return fm.checkTarGz(filePath, expectedDockerfile)
	default:
		return apperrors.ErrInvalidFileFormat
	}
}

func (fm *FileManager) CleanUp(filePath string) error {
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (fm *FileManager) checkZip(filePath, expectedDockerfile string) error {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return apperrors.ErrBadRequest
	}
	defer func() { _ = r.Close() }() // read-only archive; close failure is not actionable

	for _, f := range r.File {
		if !f.FileInfo().IsDir() && cleanArchiveEntryName(f.Name) == expectedDockerfile {
			return nil
		}
	}

	return apperrors.ErrBadRequest
}

func (fm *FileManager) checkTar(filePath, expectedDockerfile string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return apperrors.ErrInternal
	}
	defer func() { _ = f.Close() }() // read-only archive; close failure is not actionable

	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return apperrors.ErrBadRequest
		}
		if hdr.Typeflag == tar.TypeReg && cleanArchiveEntryName(hdr.Name) == expectedDockerfile {
			return nil
		}
	}

	return apperrors.ErrBadRequest
}

func (fm *FileManager) checkTarGz(filePath, expectedDockerfile string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return apperrors.ErrInternal
	}
	defer func() { _ = f.Close() }() // read-only archive; close failure is not actionable

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return apperrors.ErrBadRequest
	}
	defer func() { _ = gzr.Close() }() // read-only stream; close failure is not actionable

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return apperrors.ErrBadRequest
		}
		if hdr.Typeflag == tar.TypeReg && cleanArchiveEntryName(hdr.Name) == expectedDockerfile {
			return nil
		}
	}

	return apperrors.ErrBadRequest
}

func expectedDockerfilePath(contextDir, dockerfile string) (string, error) {
	contextDir, err := gitsource.CleanRelativePath(contextDir)
	if err != nil {
		return "", err
	}
	dockerfile, err = gitsource.CleanRelativePath(dockerfile)
	if err != nil {
		return "", err
	}
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	if contextDir == "" || contextDir == "." {
		return dockerfile, nil
	}
	return path.Join(contextDir, dockerfile), nil
}

func cleanArchiveEntryName(name string) string {
	name = strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "./")
	return path.Clean(name)
}
