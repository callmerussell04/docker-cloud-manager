package buildobjects

import (
	"path/filepath"
	"strings"
)

func ArchiveObjectKey(fileID, filePath string) string {
	return "build-archives/" + fileID + ArchiveObjectExt(filePath)
}

func LogObjectKey(fileID string) string {
	return "build-logs/" + fileID + ".log"
}

func ArchiveObjectExt(path string) string {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".tar.gz") {
		return ".tar.gz"
	}
	ext := filepath.Ext(path)
	if ext == "" {
		return ".archive"
	}
	return ext
}
