package compose

import (
	"context"
	"io"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/buildobjects"
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/logging"
	"github.com/google/uuid"
)

type ObjectStorage interface {
	UploadStream(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) error
	DeleteObject(ctx context.Context, objectKey string) error
	CopyObject(ctx context.Context, sourceKey, destKey, contentType string) error
	OpenObject(ctx context.Context, objectKey string) (io.ReadCloser, error)
	NewReaderAt(ctx context.Context, objectKey string) (io.ReaderAt, int64, error)
}

type BuildJobCreator interface {
	CreateBuildJob(ctx context.Context, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (uuid.UUID, uuid.UUID, error)
	CreateProjectBuildJob(ctx context.Context, projectID uuid.UUID, projectServiceName, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (uuid.UUID, uuid.UUID, error)
	ReserveBuildArchive(ctx context.Context, archiveObjectKey string) error
	ReleaseBuildArchive(ctx context.Context, archiveObjectKey string)
	CancelBuildRecord(ctx context.Context, buildID uuid.UUID) error
}

type LocalBuilderClient struct {
	objectStore ObjectStorage
	builds      BuildJobCreator
}

func NewLocalBuilderClient(objectStore ObjectStorage, builds BuildJobCreator) *LocalBuilderClient {
	return &LocalBuilderClient{
		objectStore: objectStore,
		builds:      builds,
	}
}

func (c *LocalBuilderClient) TriggerBuild(ctx context.Context, projectID uuid.UUID, srv model.ComposeService, sourceObjectKey string) (uuid.UUID, error) {
	if c.objectStore == nil || c.builds == nil {
		return uuid.Nil, apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}

	fileID := uuid.New().String()
	archiveObjectKey := buildobjects.ArchiveObjectKey(fileID, sourceObjectKey)
	logObjectKey := buildobjects.LogObjectKey(fileID)

	if err := c.objectStore.CopyObject(ctx, sourceObjectKey, archiveObjectKey, composeArchiveContentType(sourceObjectKey)); err != nil {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		_ = c.objectStore.DeleteObject(cleanupCtx, archiveObjectKey) // best-effort removal of the unused copy; the job error is returned
		cancel()
		return uuid.Nil, err
	}
	if err := c.builds.ReserveBuildArchive(ctx, archiveObjectKey); err != nil {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		_ = c.objectStore.DeleteObject(cleanupCtx, archiveObjectKey) // best-effort removal of the unused copy; the job error is returned
		cancel()
		return uuid.Nil, err
	}

	buildID, _, err := c.builds.CreateProjectBuildJob(ctx, projectID, srv.Name, srv.ImageTag, archiveObjectKey, logObjectKey, srv.BuildContext, srv.Dockerfile, srv.BuildArgs, logging.RequestIDFromContext(ctx))
	if err != nil {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		_ = c.objectStore.DeleteObject(cleanupCtx, archiveObjectKey) // best-effort removal of the unused copy; the job error is returned
		c.builds.ReleaseBuildArchive(cleanupCtx, archiveObjectKey)
		cancel()
		return uuid.Nil, err
	}
	return buildID, nil
}

func composeArchiveContentType(sourceObjectKey string) string {
	switch buildobjects.ArchiveObjectExt(sourceObjectKey) {
	case ".zip":
		return "application/zip"
	case ".tar":
		return "application/x-tar"
	case ".tar.gz", ".tgz":
		return "application/gzip"
	default:
		return "application/octet-stream"
	}
}

func (c *LocalBuilderClient) CancelBuild(ctx context.Context, buildID uuid.UUID) error {
	if c.builds == nil {
		return apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}
	return c.builds.CancelBuildRecord(ctx, buildID)
}
