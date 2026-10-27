package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
)

var errLogCloseFailed = errors.New("close build log")

type LogManager struct {
	logDir string
}

func NewLogManager(logDir string) (*LogManager, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}
	return &LogManager{
		logDir: logDir,
	}, nil
}

func (m *LogManager) SaveLogs(buildID string, logStream io.Reader, maxLogSize int64) (string, error) {
	logFilePath := m.LogPath(buildID)
	file, err := os.Create(logFilePath)
	if err != nil {
		return "", err
	}
	return logFilePath, WriteLogs(file, logStream, maxLogSize)
}

// WriteLogs owns and closes the destination, and never writes beyond maxLogSize.
func WriteLogs(destination io.WriteCloser, source io.Reader, maxLogSize int64) (err error) {
	defer func() {
		if closeErr := destination.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("%w: %w", errLogCloseFailed, closeErr))
		}
	}()
	if maxLogSize < 0 {
		maxLogSize = 0
	}
	n, err := io.Copy(destination, io.LimitReader(source, maxLogSize))
	if err != nil {
		return fmt.Errorf("write build log: %w", err)
	}
	if n == maxLogSize {
		var probe [1]byte
		if _, err := io.ReadFull(source, probe[:]); !errors.Is(err, io.EOF) {
			if err != nil {
				return fmt.Errorf("read build log: %w", err)
			}
			return model.ErrBuildLogSizeLimitExceeded
		}
	}
	return nil
}

func (m *LogManager) WriteSystemLog(buildID string, message string, maxLogSize int64) error {
	logFilePath := m.LogPath(buildID)
	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return errors.Join(err, file.Close())
	}
	return WriteLogs(file, strings.NewReader("\n[SYSTEM] "+message+"\n"), maxLogSize-info.Size())
}

func (m *LogManager) IsLogSizeLimitExceeded(err error) bool {
	return errors.Is(err, model.ErrBuildLogSizeLimitExceeded) && !errors.Is(err, errLogCloseFailed)
}

func (m *LogManager) LogPath(buildID string) string {
	return filepath.Join(m.logDir, buildID+".log")
}

func (m *LogManager) Exists(buildID string) (bool, error) {
	_, err := os.Stat(m.LogPath(buildID))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (m *LogManager) CleanUp(buildID string) error {
	if err := os.Remove(m.LogPath(buildID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
