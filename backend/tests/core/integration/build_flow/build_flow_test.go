package build_flow_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/objectstorage"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBuildArchiveFlowPersistsQueueAndReadsLogs(t *testing.T) {
	ctx := context.Background()
	repos := coretest.OpenCoreRepositories(t)
	ownerID := uuid.New()
	objects := map[string][]byte{}
	builds := newBuildService(t, repos, ownerID, objects, false)

	result, err := builds.CreateBuildFromArchive(coretest.UserContext(ownerID), service.BuildArchiveInput{
		Tag:         "demo:latest",
		ContextDir:  "app",
		Dockerfile:  "Dockerfile",
		BuildArgs:   map[string]string{"VERSION": "1"},
		ArchiveName: "source.zip",
		Archive:     strings.NewReader("archive-bytes"),
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, result.BuildID)

	build, err := repos.Builds.GetByID(ctx, result.BuildID)
	require.NoError(t, err)
	require.Equal(t, ownerID, build.OwnerID)
	require.Equal(t, model.BuildStatusPending, build.Status)
	require.NotEmpty(t, build.ArchiveObjectKey)
	require.Contains(t, objects, build.ArchiveObjectKey)

	image, err := repos.Images.GetByID(ctx, build.ImageID)
	require.NoError(t, err)
	require.Equal(t, "demo:latest", image.Tag)
	require.Equal(t, model.ImageStatusBuilding, image.Status)

	outbox := mustBuildOutbox(t, repos.DB)
	require.Len(t, outbox, 1)
	require.Equal(t, buildqueue.ExchangeName, outbox[0].Exchange)
	require.Equal(t, buildqueue.RoutingKey, outbox[0].RoutingKey)
	var msg buildqueue.ImageBuildMessage
	require.NoError(t, json.Unmarshal(outbox[0].Payload, &msg))
	require.Equal(t, result.BuildID.String(), msg.BuildID)
	require.Equal(t, build.ImageID.String(), msg.ImageID)
	require.Equal(t, "demo:latest", msg.Tag)
	require.Equal(t, "app", msg.ContextDir)
	require.Equal(t, "Dockerfile", msg.Dockerfile)
	require.Equal(t, map[string]string{"VERSION": "1"}, msg.BuildArgs)

	activeBytes, err := repos.Staged.ActiveBytesByOwner(ctx, ownerID)
	require.NoError(t, err)
	require.NotZero(t, activeBytes)

	started, didStart, err := builds.StartBuildRecord(ctx, result.BuildID)
	require.NoError(t, err)
	require.True(t, didStart)
	require.Equal(t, model.BuildStatusRunning, started.Status)
	_, didStart, err = builds.StartBuildRecord(ctx, result.BuildID)
	require.NoError(t, err)
	require.False(t, didStart)

	objects[build.LogFilePath] = []byte("line1\nline2\n")
	logs, err := builds.OpenBuildLogs(coretest.UserContext(ownerID), result.BuildID)
	require.NoError(t, err)
	body, err := io.ReadAll(logs)
	require.NoError(t, err)
	require.NoError(t, logs.Close())
	require.Equal(t, "line1\nline2\n", string(body))

	require.NoError(t, builds.CompleteBuildRecord(ctx, result.BuildID, build.ImageID, model.BuildStatusSuccess, 0))
	completed, err := repos.Builds.GetByID(ctx, result.BuildID)
	require.NoError(t, err)
	require.Equal(t, model.BuildStatusSuccess, completed.Status)
	completedImage, err := repos.Images.GetByID(ctx, build.ImageID)
	require.NoError(t, err)
	require.Equal(t, model.ImageStatusAvailable, completedImage.Status)
	require.Equal(t, 2, completedImage.SizeMB)

	activeBytes, err = repos.Staged.ActiveBytesByOwner(ctx, ownerID)
	require.NoError(t, err)
	require.Zero(t, activeBytes)

	require.NoError(t, builds.DeleteBuild(coretest.UserContext(ownerID), result.BuildID))
	_, err = repos.Builds.GetByID(ctx, result.BuildID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	require.NotContains(t, objects, build.ArchiveObjectKey)
	require.NotContains(t, objects, build.LogFilePath)
}

func TestBuildArchiveUploadFailureReleasesReservation(t *testing.T) {
	repos := coretest.OpenCoreRepositories(t)
	ownerID := uuid.New()
	objects := map[string][]byte{}
	builds := newBuildService(t, repos, ownerID, objects, true)

	_, err := builds.CreateBuildFromArchive(coretest.UserContext(ownerID), service.BuildArchiveInput{
		Tag:         "failed:latest",
		ArchiveName: "source.zip",
		Archive:     strings.NewReader("archive"),
	})
	require.ErrorIs(t, err, apperrors.ErrUnavailable)
	require.Zero(t, countTable(t, repos.DB, "builds"))
	require.Zero(t, countTable(t, repos.DB, "images"))
	activeBytes, err := repos.Staged.ActiveBytesByOwner(context.Background(), ownerID)
	require.NoError(t, err)
	require.Zero(t, activeBytes)
	require.Empty(t, objects)
}

func TestBuildArchiveDuplicateTagCleansObjectAndReservation(t *testing.T) {
	repos := coretest.OpenCoreRepositories(t)
	ownerID := uuid.New()
	objects := map[string][]byte{}
	builds := newBuildService(t, repos, ownerID, objects, false)

	_, err := builds.CreateBuildFromArchive(coretest.UserContext(ownerID), service.BuildArchiveInput{
		Tag:         "duplicate:latest",
		ArchiveName: "source.zip",
		Archive:     strings.NewReader("first"),
	})
	require.NoError(t, err)
	_, err = builds.CreateBuildFromArchive(coretest.UserContext(ownerID), service.BuildArchiveInput{
		Tag:         "duplicate:latest",
		ArchiveName: "source.zip",
		Archive:     strings.NewReader("second"),
	})
	require.Error(t, err)
	require.Equal(t, 1, countTable(t, repos.DB, "builds"))
	activeBytes, err := repos.Staged.ActiveBytesByOwner(context.Background(), ownerID)
	require.NoError(t, err)
	require.NotZero(t, activeBytes)
	require.Len(t, objects, 1)
}

func TestBuildFlowWithMinIOObjectStorage(t *testing.T) {
	cfg := coretest.ObjectStorageConfigFromEnv(t)
	repos := coretest.OpenCoreRepositories(t)
	ownerID := uuid.New()
	store := objectstorage.NewLazyStorage(cfg)
	builds := newBuildServiceWithStore(t, repos, ownerID, store)

	result, err := builds.CreateBuildFromArchive(coretest.UserContext(ownerID), service.BuildArchiveInput{
		Tag:         "minio:latest",
		ArchiveName: "source.zip",
		Archive:     strings.NewReader("archive"),
	})
	require.NoError(t, err)
	build, err := repos.Builds.GetByID(context.Background(), result.BuildID)
	require.NoError(t, err)
	require.NoError(t, store.UploadStream(context.Background(), build.LogFilePath, strings.NewReader("logs"), int64(len("logs")), "text/plain"))

	logs, err := builds.OpenBuildLogs(coretest.UserContext(ownerID), result.BuildID)
	require.NoError(t, err)
	body, err := io.ReadAll(logs)
	require.NoError(t, err)
	require.NoError(t, logs.Close())
	require.Equal(t, "logs", string(body))
	require.NoError(t, builds.DeleteBuild(coretest.UserContext(ownerID), result.BuildID))
}

type buildOutboxRow struct {
	Exchange   string
	RoutingKey string
	Payload    []byte
}

func mustBuildOutbox(t *testing.T, db *sql.DB) []buildOutboxRow {
	t.Helper()
	rows, err := db.Query(`SELECT exchange, routing_key, payload FROM build_queue_outbox ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []buildOutboxRow
	for rows.Next() {
		var row buildOutboxRow
		require.NoError(t, rows.Scan(&row.Exchange, &row.RoutingKey, &row.Payload))
		out = append(out, row)
	}
	require.NoError(t, rows.Err())
	return out
}

func newBuildService(t *testing.T, repos coretest.CoreRepositories, ownerID uuid.UUID, objects map[string][]byte, failUpload bool) *service.BuildService {
	t.Helper()
	store := coremocks.NewBuildObjectStore(t)
	store.EXPECT().
		UploadStream(mock.Anything, mock.AnythingOfType("string"), mock.Anything, mock.AnythingOfType("int64"), mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
			if failUpload {
				return apperrors.ErrUnavailable
			}
			var buf bytes.Buffer
			_, err := io.Copy(&buf, r)
			if err != nil {
				return err
			}
			objects[key] = buf.Bytes()
			return nil
		}).
		Maybe()
	store.EXPECT().
		DeleteObject(mock.Anything, mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, key string) error {
			delete(objects, key)
			return nil
		}).
		Maybe()
	store.EXPECT().
		OpenObject(mock.Anything, mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, key string) (io.ReadCloser, error) {
			data, ok := objects[key]
			if !ok {
				return nil, apperrors.ErrNotFound
			}
			return io.NopCloser(bytes.NewReader(data)), nil
		}).
		Maybe()
	return newBuildServiceWithStore(t, repos, ownerID, store)
}

func newBuildServiceWithStore(t *testing.T, repos coretest.CoreRepositories, ownerID uuid.UUID, store service.BuildObjectStore) *service.BuildService {
	t.Helper()
	cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
	users := coremocks.NewUserInfoProvider(t)
	users.EXPECT().
		GetUser(mock.Anything, ownerID).
		Return(model.UserInfo{ID: ownerID, Username: "builder", QuotaRAMMB: 4096, QuotaDiskMB: 4096, QuotaCPU: 4}, nil).
		Maybe()
	metrics := coremocks.NewHostDiskMetricsProvider(t)
	metrics.EXPECT().GetDiskUsage(mock.Anything).Return(model.HostDiskStats{FreeBytes: 1 << 40}, nil).Maybe()
	registry := coremocks.NewImageRegistryAPI(t)
	registry.EXPECT().
		GetImageSizeAndDigest(mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string")).
		Return(int64(2<<20), "sha256:digest", nil).
		Maybe()
	registry.EXPECT().DeleteManifest(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	return service.NewBuildService(repos.Builds, repos.Images, registry, users, coretest.DiscardLogger(), service.BuildServiceDeps{
		VolumeRepo:    repos.Volumes,
		Config:        cfg,
		ObjectStore:   store,
		StagedObjects: repos.Staged,
		DiskMetrics:   metrics,
		HostDiskPath:  "/",
	})
}

func countTable(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&count))
	return count
}
