package storage

import (
	"os"
	"strings"
	"testing"
)

func TestLogManagerSaveLogsHandlesLongLines(t *testing.T) {
	manager, err := NewLogManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewLogManager returned error: %v", err)
	}

	longLine := strings.Repeat("x", 70*1024)
	input := longLine + "\nnext line\n"
	logPath, err := manager.SaveLogs("build-id", strings.NewReader(input), int64(len(input)+1024))
	if err != nil {
		t.Fatalf("SaveLogs returned error: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, longLine) || !strings.Contains(got, "next line\n") {
		t.Fatalf("saved log does not contain expected long-line content")
	}
}
