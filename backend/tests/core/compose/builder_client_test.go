package compose_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLocalBuilderClientPreservesComposeSourceArchiveExtension(t *testing.T) {
	store := &recordingObjectStore{}
	builds := &recordingBuildJobCreator{}
	client := NewLocalBuilderClient(store, builds)

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

type recordingObjectStore struct {
	destKey     string
	contentType string
}

func (s *recordingObjectStore) UploadStream(context.Context, string, io.Reader, int64, string) error {
	return nil
}

func (s *recordingObjectStore) DeleteObject(context.Context, string) error {
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
}

func (b *recordingBuildJobCreator) CreateBuildJob(ctx context.Context, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (uuid.UUID, uuid.UUID, error) {
	return uuid.New(), uuid.New(), nil
}

func (b *recordingBuildJobCreator) CreateProjectBuildJob(ctx context.Context, projectID uuid.UUID, projectServiceName, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (uuid.UUID, uuid.UUID, error) {
	b.archiveObjectKey = archiveObjectKey
	return uuid.New(), uuid.New(), nil
}

func (b *recordingBuildJobCreator) ReserveBuildArchive(ctx context.Context, archiveObjectKey string) error {
	return nil
}

func (b *recordingBuildJobCreator) CancelBuildRecord(ctx context.Context, buildID uuid.UUID) error {
	return nil
}
