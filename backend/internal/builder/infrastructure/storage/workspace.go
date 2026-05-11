package storage

import (
	"os"
	"path/filepath"

	"github.com/callmerussell04/docker-cloud-manager/pkg/buildobjects"
)

type WorkspaceManager struct {
	baseDir string
}

func NewWorkspaceManager(baseDir string) (*WorkspaceManager, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &WorkspaceManager{baseDir: baseDir}, nil
}

func (m *WorkspaceManager) ArchivePath(buildID, objectKey string) string {
	return filepath.Join(m.baseDir, buildID+buildobjects.ArchiveObjectExt(objectKey))
}

func (m *WorkspaceManager) Create(buildID string) (string, func(), error) {
	path := filepath.Join(m.baseDir, buildID+"_workspace")
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", func() {}, err
	}
	return path, func() { _ = os.RemoveAll(path) }, nil
}
