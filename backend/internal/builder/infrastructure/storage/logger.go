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
	logDir     string
	maxLogSize int64
}

func NewLogManager(logDir string, maxLogSize int64) (*LogManager, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}
	return &LogManager{
		logDir:     logDir,
		maxLogSize: maxLogSize,
	}, nil
}

func (m *LogManager) SaveLogs(buildID string, logStream io.Reader) (string, error) {
	logFilePath := filepath.Join(m.logDir, buildID+".log")
	file, err := os.Create(logFilePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(logStream)
	var totalBytes int64

	for scanner.Scan() {
		line := scanner.Text() + "\n"

		n, _ := file.WriteString(line)
		totalBytes += int64(n)

		if totalBytes > m.maxLogSize {
			file.WriteString("\n[SYSTEM] Log size limit exceeded. Build aborted.\n")
			return logFilePath, ErrLogSizeLimitExceeded
		}
	}

	if err := scanner.Err(); err != nil {
		return logFilePath, err
	}

	return logFilePath, nil
}

func (m *LogManager) WriteSystemLog(buildID string, message string) error {
	logFilePath := filepath.Join(m.logDir, buildID+".log")
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
