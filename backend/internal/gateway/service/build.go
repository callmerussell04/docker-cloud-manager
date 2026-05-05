package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildobjects"
	"github.com/callmerussell04/docker-cloud-manager/pkg/gitsource"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	"github.com/google/uuid"
)

type BuildProvider interface {
	CreateBuildJob(ctx context.Context, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (string, string, error)
	GetBuild(ctx context.Context, buildID string) (model.Build, error)
	CancelBuildRecord(ctx context.Context, buildID string) error
	DeleteBuild(ctx context.Context, buildID string) error
	GetAllBuilds(ctx context.Context, page, limit int) (model.PaginatedBuilds, error)
}

type BuildObjectStore interface {
	UploadStream(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) error
	OpenObject(ctx context.Context, objectKey string) (io.ReadCloser, error)
	DeleteObject(ctx context.Context, objectKey string) error
}

const imageBuildsUnavailableMessage = "Image builds are currently unavailable. Use Docker Hub images."
const gitSourcesUnavailableMessage = "Git sources are currently unavailable. Upload an archive instead."

func (s *Core) CreateBuildJob(ctx context.Context, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (string, string, error) {
	return s.provider.CreateBuildJob(ctx, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile, buildArgs, requestID)
}

func (s *Core) CreateBuildFromArchive(ctx context.Context, input model.BuildArchiveInput) (model.BuildInitResult, error) {
	if s.objectStore == nil {
		return model.BuildInitResult{}, apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}
	cfg, err := s.GetSystemConfig(ctx)
	if err != nil {
		return model.BuildInitResult{}, err
	}
	if !cfg.ImageBuildsEnabled {
		return model.BuildInitResult{}, apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}
	if err := validation.ImageTag(input.Tag); err != nil {
		return model.BuildInitResult{}, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if err := gitsource.ValidateRelativePath(input.ContextDir); err != nil {
		return model.BuildInitResult{}, fmt.Errorf("%w: invalid build context: %v", apperrors.ErrBadRequest, err)
	}
	if err := gitsource.ValidateRelativePath(input.Dockerfile); err != nil {
		return model.BuildInitResult{}, fmt.Errorf("%w: invalid dockerfile path: %v", apperrors.ErrBadRequest, err)
	}
	if input.Archive == nil || input.ArchiveName == "" {
		return model.BuildInitResult{}, apperrors.New(apperrors.ErrBadRequest, "build archive is required")
	}
	if input.BuildArgs == nil {
		input.BuildArgs = map[string]string{}
	}

	fileID := uuid.New().String()
	archiveObjectKey := buildobjects.ArchiveObjectKey(fileID, input.ArchiveName)
	logObjectKey := buildobjects.LogObjectKey(fileID)

	limitedArchive := &maxBytesReader{r: input.Archive, remaining: cfg.MaxArchiveSizeBytes}
	if err := s.objectStore.UploadStream(ctx, archiveObjectKey, limitedArchive, -1, "application/octet-stream"); err != nil {
		_ = s.objectStore.DeleteObject(context.Background(), archiveObjectKey)
		if errors.Is(err, apperrors.ErrBadRequest) {
			return model.BuildInitResult{}, apperrors.New(apperrors.ErrBadRequest, "archive exceeds configured size limit")
		}
		return model.BuildInitResult{}, err
	}

	buildID, _, err := s.provider.CreateBuildJob(ctx, input.Tag, archiveObjectKey, logObjectKey, input.ContextDir, input.Dockerfile, input.BuildArgs, logging.RequestIDFromContext(ctx))
	if err != nil {
		_ = s.objectStore.DeleteObject(context.Background(), archiveObjectKey)
		return model.BuildInitResult{}, err
	}

	args := []any{
		"request_id", logging.RequestIDFromContext(ctx),
		"build_id", buildID,
		"archive_object_key", archiveObjectKey,
		"log_object_key", logObjectKey,
	}
	if scope, ok := accessscope.FromContext(ctx); ok {
		args = append(args, "user_id", scope.UserID)
	}
	s.logger.InfoContext(ctx, "build upload accepted", args...)

	return model.BuildInitResult{BuildID: buildID}, nil
}

func (s *Core) CreateBuildFromGit(ctx context.Context, input model.BuildGitInput) (model.BuildInitResult, error) {
	if s.objectStore == nil {
		return model.BuildInitResult{}, apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}
	cfg, err := s.GetSystemConfig(ctx)
	if err != nil {
		return model.BuildInitResult{}, err
	}
	if !cfg.ImageBuildsEnabled {
		return model.BuildInitResult{}, apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}
	if !cfg.GitSourcesEnabled {
		return model.BuildInitResult{}, apperrors.New(apperrors.ErrUnavailable, gitSourcesUnavailableMessage)
	}
	if err := validation.ImageTag(input.Tag); err != nil {
		return model.BuildInitResult{}, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	contextDir, err := gitsource.CleanRelativePath(input.ContextDir)
	if err != nil {
		return model.BuildInitResult{}, fmt.Errorf("%w: invalid build context: %v", apperrors.ErrBadRequest, err)
	}
	dockerfile, err := gitsource.CleanRelativePath(input.Dockerfile)
	if err != nil {
		return model.BuildInitResult{}, fmt.Errorf("%w: invalid dockerfile path: %v", apperrors.ErrBadRequest, err)
	}
	if _, err := gitsource.ValidateRepoURL(input.RepoURL, cfg.GitAllowedHosts); err != nil {
		return model.BuildInitResult{}, err
	}
	if err := gitsource.ValidateRef(input.Ref); err != nil {
		return model.BuildInitResult{}, err
	}
	if input.BuildArgs == nil {
		input.BuildArgs = map[string]string{}
	}

	tmpDir, err := os.MkdirTemp("", "dcm-git-build-*")
	if err != nil {
		return model.BuildInitResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")
	repoInfo, err := gitsource.Clone(ctx, gitsource.CloneRequest{
		RepoURL:            input.RepoURL,
		Ref:                input.Ref,
		DestDir:            repoDir,
		AllowedHosts:       cfg.GitAllowedHosts,
		Timeout:            time.Duration(cfg.GitCloneTimeoutSeconds) * time.Second,
		MaxRepositoryBytes: cfg.GitMaxRepositoryBytes,
	})
	if err != nil {
		return model.BuildInitResult{}, err
	}

	archivePath := filepath.Join(tmpDir, "source.zip")
	stats, err := gitsource.ArchiveToZipFile(ctx, repoDir, archivePath, gitsource.ArchiveLimits{
		MaxRepositoryBytes: cfg.GitMaxRepositoryBytes,
		MaxArchiveBytes:    cfg.MaxArchiveSizeBytes,
	})
	if err != nil {
		return model.BuildInitResult{}, err
	}

	fileID := uuid.New().String()
	archiveObjectKey := buildobjects.ArchiveObjectKey(fileID, "source.zip")
	logObjectKey := buildobjects.LogObjectKey(fileID)

	archive, err := os.Open(archivePath)
	if err != nil {
		return model.BuildInitResult{}, err
	}
	defer archive.Close()

	if err := s.objectStore.UploadStream(ctx, archiveObjectKey, archive, stats.ArchiveBytes, "application/zip"); err != nil {
		_ = s.objectStore.DeleteObject(context.Background(), archiveObjectKey)
		return model.BuildInitResult{}, err
	}

	buildID, _, err := s.provider.CreateBuildJob(ctx, input.Tag, archiveObjectKey, logObjectKey, contextDir, dockerfile, input.BuildArgs, logging.RequestIDFromContext(ctx))
	if err != nil {
		_ = s.objectStore.DeleteObject(context.Background(), archiveObjectKey)
		return model.BuildInitResult{}, err
	}

	args := []any{
		"request_id", logging.RequestIDFromContext(ctx),
		"build_id", buildID,
		"git_host", repoInfo.Host,
		"git_repo_path", repoInfo.Path,
		"git_ref", repoInfo.Ref,
		"archive_bytes", stats.ArchiveBytes,
		"repository_bytes", stats.RepositoryBytes,
	}
	if scope, ok := accessscope.FromContext(ctx); ok {
		args = append(args, "user_id", scope.UserID)
	}
	s.logger.InfoContext(ctx, "git build accepted", args...)

	return model.BuildInitResult{BuildID: buildID}, nil
}

func (s *Core) OpenBuildLogs(ctx context.Context, buildID string) (io.ReadCloser, error) {
	if s.objectStore == nil {
		return nil, apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}
	build, err := s.provider.GetBuild(ctx, buildID)
	if err != nil {
		return nil, err
	}
	if build.LogFilePath == "" {
		return nil, apperrors.ErrNotFound
	}
	return s.objectStore.OpenObject(ctx, build.LogFilePath)
}

func (s *Core) GetBuild(ctx context.Context, buildID string) (model.Build, error) {
	return s.provider.GetBuild(ctx, buildID)
}

func (s *Core) CancelBuildRecord(ctx context.Context, buildID string) error {
	return s.provider.CancelBuildRecord(ctx, buildID)
}

func (s *Core) DeleteBuild(ctx context.Context, buildID string) error {
	return s.provider.DeleteBuild(ctx, buildID)
}

func (s *Core) GetAllBuilds(ctx context.Context, page, limit int) (model.PaginatedBuilds, error) {
	return s.provider.GetAllBuilds(ctx, page, limit)
}

type maxBytesReader struct {
	r         io.Reader
	remaining int64
}

func (r *maxBytesReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		var one [1]byte
		n, err := r.r.Read(one[:])
		if n > 0 {
			return 0, apperrors.ErrBadRequest
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	return n, err
}
