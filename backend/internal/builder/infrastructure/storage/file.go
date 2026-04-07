package storage

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type FileManager struct {
	baseDir        string
	maxArchiveSize int64
}

func NewFileManager(baseDir string, maxArchiveSize int64) (*FileManager, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &FileManager{
		baseDir:        baseDir,
		maxArchiveSize: maxArchiveSize,
	}, nil
}

func (fm *FileManager) SaveArchive(file *multipart.FileHeader, buildID string) (string, error) {
	ext := filepath.Ext(file.Filename)
	if strings.HasSuffix(strings.ToLower(file.Filename), ".tar.gz") {
		ext = ".tar.gz"
	}

	filePath := filepath.Join(fm.baseDir, buildID+ext)

	src, err := file.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()

	dst, err := os.Create(filePath)
	if err != nil {
		return "", err
	}

	lr := io.LimitReader(src, fm.maxArchiveSize+1)
	written, err := io.Copy(dst, lr)
	dst.Close()

	if err != nil {
		os.Remove(filePath)
		return "", err
	}

	if written > fm.maxArchiveSize {
		os.Remove(filePath)
		return "", apperrors.ErrBadRequest
	}

	return filePath, nil
}

func (fm *FileManager) ValidateArchive(filePath string) error {
	ext := strings.ToLower(filepath.Ext(filePath))
	if strings.HasSuffix(strings.ToLower(filePath), ".tar.gz") {
		ext = ".tar.gz"
	}

	switch ext {
	case ".zip":
		return fm.checkZip(filePath)
	case ".tar":
		return fm.checkTar(filePath)
	case ".tar.gz", ".tgz":
		return fm.checkTarGz(filePath)
	default:
		return apperrors.ErrInvalidFileFormat
	}
}

func (fm *FileManager) CleanUp(filePath string) error {
	return os.Remove(filePath)
}

func (fm *FileManager) checkZip(filePath string) error {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return apperrors.ErrBadRequest
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "Dockerfile" || strings.HasSuffix(f.Name, "/Dockerfile") {
			return nil
		}
	}

	return apperrors.ErrBadRequest
}

func (fm *FileManager) checkTar(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return apperrors.ErrInternal
	}
	defer f.Close()

	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return apperrors.ErrBadRequest
		}
		if hdr.Name == "Dockerfile" || strings.HasSuffix(hdr.Name, "/Dockerfile") {
			return nil
		}
	}

	return apperrors.ErrBadRequest
}

func (fm *FileManager) checkTarGz(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return apperrors.ErrInternal
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return apperrors.ErrBadRequest
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return apperrors.ErrBadRequest
		}
		if hdr.Name == "Dockerfile" || strings.HasSuffix(hdr.Name, "/Dockerfile") {
			return nil
		}
	}

	return apperrors.ErrBadRequest
}
