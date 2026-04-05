package storage

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

type buildOutput struct {
	Stream      string `json:"stream"`
	Error       string `json:"error"`
	ErrorDetail struct {
		Message string `json:"message"`
	} `json:"errorDetail"`
}

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

func (m *LogManager) SaveLogs(buildID string, dockerStream io.Reader) (string, error) {
	logFilePath := filepath.Join(m.logDir, buildID+".log")
	file, err := os.Create(logFilePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(dockerStream)
	var buildErr error
	var totalBytes int64

	for scanner.Scan() {
		line := scanner.Text()
		var output buildOutput

		var logStr string
		if err := json.Unmarshal([]byte(line), &output); err == nil {
			if output.Error != "" {
				buildErr = errors.New(output.Error)
				logStr = "[ERROR] " + output.Error + "\n"
			} else if output.Stream != "" {
				logStr = output.Stream
			}
		} else {
			logStr = line + "\n"
		}

		if logStr != "" {
			n, _ := file.WriteString(logStr)
			totalBytes += int64(n)

			if totalBytes > m.maxLogSize {
				file.WriteString("\n[SYSTEM] Log size limit exceeded. Build aborted.\n")
				return logFilePath, errors.New("log size limit exceeded")
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return logFilePath, err
	}

	return logFilePath, buildErr
}
