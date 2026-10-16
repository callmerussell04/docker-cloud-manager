package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
)

// ResultStore retains outcomes on the persistent Builder volume across restarts.
type ResultStore struct{ dir string }

func NewResultStore(baseDir string) (*ResultStore, error) {
	dir := filepath.Join(baseDir, "results")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create build result journal: %w", err)
	}
	return &ResultStore{dir: dir}, nil
}

func (s *ResultStore) path(buildID string) string {
	sum := sha256.Sum256([]byte(buildID))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}

func (s *ResultStore) Load(ctx context.Context, buildID string) (model.BuildResult, bool, error) {
	if err := ctx.Err(); err != nil {
		return model.BuildResult{}, false, err
	}
	data, err := os.ReadFile(s.path(buildID))
	if errors.Is(err, os.ErrNotExist) {
		return model.BuildResult{}, false, nil
	}
	if err != nil {
		return model.BuildResult{}, false, fmt.Errorf("read build result: %w", err)
	}
	var result model.BuildResult
	if err := json.Unmarshal(data, &result); err != nil {
		return result, false, fmt.Errorf("decode build result: %w", err)
	}
	if result.ImageID == "" || result.Status == "" {
		return result, false, errors.New("incomplete build result")
	}
	return result, true, nil
}

func (s *ResultStore) Save(ctx context.Context, buildID string, result model.BuildResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode build result: %w", err)
	}
	file, err := os.CreateTemp(s.dir, ".result-*")
	if err != nil {
		return fmt.Errorf("create build result: %w", err)
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), s.path(buildID)); err != nil {
		return fmt.Errorf("persist build result: %w", err)
	}
	return s.syncDir()
}

func (s *ResultStore) Delete(ctx context.Context, buildID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(s.path(buildID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove build result: %w", err)
	}
	return s.syncDir()
}

func (s *ResultStore) syncDir() error {
	dir, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
