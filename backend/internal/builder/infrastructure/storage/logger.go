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
	logDir string
}

func NewLogManager(logDir string) (*LogManager, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}
	return &LogManager{logDir: logDir}, nil
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

	for scanner.Scan() {
		line := scanner.Text()
		var output buildOutput

		if err := json.Unmarshal([]byte(line), &output); err == nil {
			if output.Error != "" {
				// Если Докер прислал ошибку компиляции (например, неверный Dockerfile)
				buildErr = errors.New(output.Error)
				file.WriteString("[ERROR] " + output.Error + "\n")
			} else if output.Stream != "" {
				// Пишем обычный лог (Step 1/5...)
				file.WriteString(output.Stream)
			}
		} else {
			// Если строка почему-то не в JSON, пишем как есть
			file.WriteString(line + "\n")
		}
	}

	if err := scanner.Err(); err != nil {
		return logFilePath, err
	}

	return logFilePath, buildErr
}
