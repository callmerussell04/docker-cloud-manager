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

type Converter struct{}

func NewConverter() *Converter {
	return &Converter{}
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
		defer pw.Close()

		tw := tar.NewWriter(pw)
		defer tw.Close()

		for _, f := range zr.File {
			if err := c.writeZipFileToTar(tw, f); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
	}()

	return pr, nil
}

func (c *Converter) writeZipFileToTar(tw *tar.Writer, f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	info := f.FileInfo()
	header, err := tar.FileInfoHeader(info, info.Name())
	if err != nil {
		return err
	}
	header.Name = f.Name

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	if !info.IsDir() {
		_, err = io.Copy(tw, rc)
	}
	return err
}
