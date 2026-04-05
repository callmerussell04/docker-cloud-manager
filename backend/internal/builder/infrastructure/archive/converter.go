package archive

import (
	"archive/tar"
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type Converter struct {
	maxUnpackedSize int64
}

func NewConverter(maxUnpackedSize int64) *Converter {
	return &Converter{
		maxUnpackedSize: maxUnpackedSize,
	}
}

func (c *Converter) ToTarStream(filePath string) (io.ReadCloser, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if strings.HasSuffix(strings.ToLower(filePath), ".tar.gz") {
		ext = ".tar.gz"
	}

	if ext == ".tar" || ext == ".tar.gz" || ext == ".tgz" {
		return os.Open(filePath)
	}

	if ext == ".zip" {
		return c.streamZipAsTar(filePath)
	}

	return nil, apperrors.ErrInvalidFileFormat
}

func (c *Converter) streamZipAsTar(zipPath string) (io.ReadCloser, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()

	go func() {
		defer zr.Close()

		var err error
		defer func() {
			if p := recover(); p != nil {
				pw.CloseWithError(apperrors.ErrInternal)
			} else if err != nil {
				pw.CloseWithError(err)
			} else {
				pw.Close()
			}
		}()

		tw := tar.NewWriter(pw)
		defer func() {
			if closeErr := tw.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
		}()

		remainingBytes := c.maxUnpackedSize

		for _, f := range zr.File {
			if err = c.writeZipFileToTar(tw, f, &remainingBytes); err != nil {
				return
			}
		}
	}()

	return pr, nil
}

func (c *Converter) writeZipFileToTar(tw *tar.Writer, f *zip.File, remainingBytes *int64) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	info := f.FileInfo()

	if !info.IsDir() && info.Size() > *remainingBytes {
		return apperrors.ErrResourceExhausted
	}

	header, err := tar.FileInfoHeader(info, info.Name())
	if err != nil {
		return err
	}
	header.Name = f.Name

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	if !info.IsDir() {
		lr := io.LimitReader(rc, *remainingBytes)
		written, err := io.Copy(tw, lr)
		if err != nil {
			return err
		}
		*remainingBytes -= written

		if *remainingBytes <= 0 {
			return apperrors.ErrResourceExhausted
		}
	}
	return nil
}
