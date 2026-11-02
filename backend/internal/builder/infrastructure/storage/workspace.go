package storage

import (
	"context"
	"os"
	"path/filepath"

	"github.com/callmerussell04/docker-cloud-manager/internal/platform/buildobjects"
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
	return path, func() { _ = os.RemoveAll(path) }, nil // best-effort workspace removal; CleanUp reports failures for the build
}

func (m *WorkspaceManager) CleanUp(ctx context.Context, buildID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(m.baseDir, buildID+"_workspace"))
}
