package compose_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLocalBuilderClientPreservesComposeSourceArchiveExtension(t *testing.T) {
	store := &recordingObjectStore{}
	builds := &recordingBuildJobCreator{}
	client := compose.NewLocalBuilderClient(store, builds)

	_, err := client.TriggerBuild(context.Background(), uuid.New(), model.ComposeService{
		Name:         "web",
		ImageTag:     "registry.local/web:latest",
		BuildContext: "deploy/app",
		Dockerfile:   "Dockerfile",
	}, "compose-sources/source.tar.gz")
	require.NoError(t, err)

	require.True(t, strings.HasSuffix(store.destKey, ".tar.gz"), store.destKey)
	require.Equal(t, "application/gzip", store.contentType)
	require.Equal(t, store.destKey, builds.archiveObjectKey)
}

func TestLocalBuilderClientReleasesArchiveReservationWhenJobCreationFails(t *testing.T) {
	createErr := errors.New("build job creation failed")
	store := &recordingObjectStore{}
	builds := &recordingBuildJobCreator{createErr: createErr}
	client := compose.NewLocalBuilderClient(store, builds)

	_, err := client.TriggerBuild(context.Background(), uuid.New(), model.ComposeService{
		Name:     "web",
		ImageTag: "registry.local/web:latest",
	}, "compose-sources/source.zip")
	require.ErrorIs(t, err, createErr)

	require.Equal(t, []string{store.destKey}, builds.released)
	require.Equal(t, []string{store.destKey}, store.deleted)
}

type recordingObjectStore struct {
	destKey     string
	contentType string
	deleted     []string
}

func (s *recordingObjectStore) UploadStream(context.Context, string, io.Reader, int64, string) error {
	return nil
}

func (s *recordingObjectStore) DeleteObject(_ context.Context, objectKey string) error {
	s.deleted = append(s.deleted, objectKey)
	return nil
}

func (s *recordingObjectStore) CopyObject(ctx context.Context, sourceKey, destKey, contentType string) error {
	s.destKey = destKey
	s.contentType = contentType
	return nil
}

func (s *recordingObjectStore) OpenObject(context.Context, string) (io.ReadCloser, error) {
	return nil, nil
}

func (s *recordingObjectStore) NewReaderAt(context.Context, string) (io.ReaderAt, int64, error) {
	return nil, 0, nil
}

type recordingBuildJobCreator struct {
	archiveObjectKey string
	createErr        error
	released         []string
}

func (b *recordingBuildJobCreator) CreateBuildJob(ctx context.Context, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (uuid.UUID, uuid.UUID, error) {
	return uuid.New(), uuid.New(), nil
}

func (b *recordingBuildJobCreator) CreateProjectBuildJob(ctx context.Context, projectID uuid.UUID, projectServiceName, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (uuid.UUID, uuid.UUID, error) {
	b.archiveObjectKey = archiveObjectKey
	if b.createErr != nil {
		return uuid.Nil, uuid.Nil, b.createErr
	}
	return uuid.New(), uuid.New(), nil
}

func (b *recordingBuildJobCreator) ReserveBuildArchive(ctx context.Context, archiveObjectKey string) error {
	return nil
}

func (b *recordingBuildJobCreator) ReleaseBuildArchive(_ context.Context, archiveObjectKey string) {
	b.released = append(b.released, archiveObjectKey)
}

func (b *recordingBuildJobCreator) CancelBuildRecord(ctx context.Context, buildID uuid.UUID) error {
	return nil
}
