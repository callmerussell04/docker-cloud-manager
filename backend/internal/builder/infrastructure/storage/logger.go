package storage

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
)

var ErrLogSizeLimitExceeded = errors.New("log size limit exceeded")

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
	defer file.Close()

	reader := bufio.NewReader(logStream)
	var totalBytes int64

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			n, _ := file.WriteString(line)
			totalBytes += int64(n)

			if totalBytes > maxLogSize {
				file.WriteString("\n[SYSTEM] Log size limit exceeded. Build aborted.\n")
				return logFilePath, ErrLogSizeLimitExceeded
			}
		}

		if errors.Is(err, io.EOF) {
			return logFilePath, nil
		}
		if err != nil {
			return logFilePath, err
		}
	}
}

func (m *LogManager) WriteSystemLog(buildID string, message string) error {
	logFilePath := m.LogPath(buildID)
	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := file.WriteString("\n[SYSTEM] " + message + "\n"); err != nil {
		return err
	}
	return nil
}

func (m *LogManager) IsLogSizeLimitExceeded(err error) bool {
	return errors.Is(err, ErrLogSizeLimitExceeded)
}

func (m *LogManager) LogPath(buildID string) string {
	return filepath.Join(m.logDir, buildID+".log")
}
