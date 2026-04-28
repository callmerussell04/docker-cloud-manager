package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type Extractor struct {
}

func NewExtractor() *Extractor {
	return &Extractor{}
}

func (e *Extractor) Extract(archivePath string, destDir string, maxUnpackedSize int64) error {
	ext := strings.ToLower(filepath.Ext(archivePath))
	if strings.HasSuffix(strings.ToLower(archivePath), ".tar.gz") {
		ext = ".tar.gz"
	}

	switch ext {
	case ".zip":
		return e.extractZip(archivePath, destDir, maxUnpackedSize)
	case ".tar":
		return e.extractTar(archivePath, destDir, maxUnpackedSize, false)
	case ".tar.gz", ".tgz":
		return e.extractTar(archivePath, destDir, maxUnpackedSize, true)
	default:
		return apperrors.ErrInvalidFileFormat
	}
}

func (e *Extractor) extractZip(zipPath string, destDir string, maxUnpackedSize int64) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	remainingBytes := maxUnpackedSize

	for _, f := range r.File {
		if err := e.extractZipFile(f, destDir, &remainingBytes); err != nil {
			return err
		}
	}
	return nil
}

func (e *Extractor) extractZipFile(f *zip.File, destDir string, remainingBytes *int64) error {
	// Защита от ZipSlip
	destPath, err := sanitizeExtractPath(f.Name, destDir)
	if err != nil {
		return err
	}

	if f.FileInfo().IsDir() {
		return os.MkdirAll(destPath, os.ModePerm)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), os.ModePerm); err != nil {
		return err
	}

	dstFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	srcFile, err := f.Open()
	if err != nil {
		return err
	}
	defer srcFile.Close()

	lr := io.LimitReader(srcFile, *remainingBytes)
	written, err := io.Copy(dstFile, lr)
	if err != nil {
		return err
	}

	*remainingBytes -= written
	if *remainingBytes <= 0 {
		return apperrors.ErrResourceExhausted
	}

	return nil
}

func (e *Extractor) extractTar(tarPath string, destDir string, maxUnpackedSize int64, isGzip bool) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()

	var tr *tar.Reader
	if isGzip {
		gzr, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gzr.Close()
		tr = tar.NewReader(gzr)
	} else {
		tr = tar.NewReader(f)
	}

	remainingBytes := maxUnpackedSize

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		destPath, err := sanitizeExtractPath(header.Name, destDir)
		if err != nil {
			return err // Пропускаем файлы с плохими путями или прерываем
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return err
			}
			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			lr := io.LimitReader(tr, remainingBytes)
			written, err := io.Copy(outFile, lr)
			outFile.Close()
			if err != nil {
				return err
			}

			remainingBytes -= written
			if remainingBytes <= 0 {
				return apperrors.ErrResourceExhausted
			}
		}
	}
	return nil
}

// sanitizeExtractPath защищает от ZipSlip уязвимости (пути вида ../../etc/passwd)
func sanitizeExtractPath(filePath string, destination string) (string, error) {
	destPath := filepath.Join(destination, filePath)
	if !strings.HasPrefix(destPath, filepath.Clean(destination)+string(os.PathSeparator)) {
		return "", fmt.Errorf("illegal file path: %s", filePath)
	}
	return destPath, nil
}
