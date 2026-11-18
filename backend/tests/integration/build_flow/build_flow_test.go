package build_flow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	builderclient "github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/grpc/client"
	buildermodel "github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
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

	// Builder claims and completes the build through its own client and
	// credential, behind Core's production caller policies.
	builderConn := coretest.NewCoreGRPCConn(t, coretest.CoreGRPCRegistration{Builds: builds},
		grpc.WithUnaryInterceptor(internalauth.SystemScopeUnaryClientInterceptor(coretest.BuilderCredential)))
	core := builderclient.NewCoreClient(builderConn)
	_, didStart, startedStatus, err := core.StartBuildRecord(ctx, result.BuildID.String())
	require.NoError(t, err)
	require.True(t, didStart)
	require.Equal(t, buildermodel.BuildStatus(model.BuildStatusRunning), startedStatus)
	_, didStart, _, err = core.StartBuildRecord(ctx, result.BuildID.String())
	require.NoError(t, err)
	require.False(t, didStart)
	// Builder's status poll reads the owner's build with system scope.
	polled, err := core.GetBuildStatus(ctx, result.BuildID.String())
	require.NoError(t, err)
	require.Equal(t, buildermodel.BuildStatus(model.BuildStatusRunning), polled)

	objects[build.LogObjectKey] = []byte("line1\nline2\n")
	logs, err := builds.OpenBuildLogs(coretest.UserContext(ownerID), result.BuildID)
	require.NoError(t, err)
	body, err := io.ReadAll(logs)
	require.NoError(t, err)
	require.NoError(t, logs.Close())
	require.Equal(t, "line1\nline2\n", string(body))

	require.NoError(t, core.CompleteBuildRecord(ctx, result.BuildID.String(), build.ImageID.String(), buildermodel.BuildStatus(model.BuildStatusSuccess), 0))
	completed, err := repos.Builds.GetByID(ctx, result.BuildID)
	require.NoError(t, err)
	require.Equal(t, model.BuildStatusSuccess, completed.Status)
	completedImage, err := repos.Images.GetByID(ctx, build.ImageID)
	require.NoError(t, err)
	require.Equal(t, model.ImageStatusAvailable, completedImage.Status)
	require.EqualValues(t, 2<<20+123, completedImage.SizeBytes, "the size the Registry reports is stored unrounded")

	activeBytes, err = repos.Staged.ActiveBytesByOwner(ctx, ownerID)
	require.NoError(t, err)
	require.Zero(t, activeBytes)

	require.NoError(t, builds.DeleteBuild(coretest.UserContext(ownerID), result.BuildID))
	_, err = repos.Builds.GetByID(ctx, result.BuildID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	require.NotContains(t, objects, build.ArchiveObjectKey)
	require.NotContains(t, objects, build.LogObjectKey)
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
	require.NoError(t, store.UploadStream(context.Background(), build.LogObjectKey, strings.NewReader("logs"), int64(len("logs")), "text/plain"))

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

func mustBuildOutbox(t *testing.T, db *pgxpool.Pool) []buildOutboxRow {
	t.Helper()
	rows, err := db.Query(context.Background(), `SELECT exchange, routing_key, payload FROM build_queue_outbox ORDER BY created_at, id`)
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
	return newBuildServiceWithStore(t, repos, ownerID, newMemoryBuildObjectStore(t, objects, failUpload))
}

// newMemoryBuildObjectStore keeps staged objects in objects. With failUpload
// every upload fails as unavailable.
func newMemoryBuildObjectStore(t *testing.T, objects map[string][]byte, failUpload bool) service.BuildObjectStore {
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
	store.EXPECT().
		StatObject(mock.Anything, mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, key string) (objectstorage.ObjectInfo, error) {
			data, ok := objects[key]
			if !ok {
				return objectstorage.ObjectInfo{}, apperrors.ErrNotFound
			}
			return objectstorage.ObjectInfo{Size: int64(len(data))}, nil
		}).
		Maybe()
	return store
}

func newBuildServiceWithStore(t *testing.T, repos coretest.CoreRepositories, ownerID uuid.UUID, store service.BuildObjectStore) *service.BuildService {
	t.Helper()
	metrics := coremocks.NewHostDiskMetricsProvider(t)
	metrics.EXPECT().GetDiskUsage(mock.Anything).Return(model.HostDiskStats{FreeBytes: 1 << 40}, nil).Maybe()
	return newBuildServiceWithDisk(t, repos, ownerID, store, coretest.IntegrationConfig(), metrics)
}

// newBuildServiceWithDisk is a build service over repos that reads cfg and
// measures the host disk through metrics.
func newBuildServiceWithDisk(t *testing.T, repos coretest.CoreRepositories, ownerID uuid.UUID, store service.BuildObjectStore, systemConfig config.SystemConfig, metrics service.HostDiskMetricsProvider) *service.BuildService {
	t.Helper()
	cfg := coretest.StaticConfig{Config: systemConfig}
	repos.SetCustomQuota(t, ownerID, model.UserQuota{CPUMillicores: 4000, RAMMB: 4096, DiskMB: 4096})
	hostMetrics := coremocks.NewHostMetricsProvider(t)
	hostMetrics.EXPECT().GetTotalMemory().Return(int64(1<<40), nil).Maybe()
	hostMetrics.EXPECT().GetLogicalCPUs().Return(int64(64), nil).Maybe()

	registry := coremocks.NewImageRegistryAPI(t)
	registry.EXPECT().
		GetImageSizeAndDigest(mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string")).
		Return(int64(2<<20+123), "sha256:digest", nil).
		Maybe()
	registry.EXPECT().DeleteManifest(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	imageCache := coremocks.NewBuildImageCache(t)
	imageCache.EXPECT().RemoveImage(mock.Anything, mock.Anything, false).Return(nil).Maybe()

	svc, err := service.NewBuildService(service.BuildServiceDeps{
		Repository:    repos.Builds,
		RegistryAuth:  repos.RegistryAuthorization(t),
		Images:        repos.Images,
		Registry:      registry,
		Docker:        imageCache,
		Quotas:        repos.UserQuotas(t, cfg),
		Logger:        coretest.DiscardLogger(),
		Containers:    repos.Containers,
		HostMetrics:   hostMetrics,
		VolumeRepo:    repos.Volumes,
		Config:        cfg,
		ObjectStore:   store,
		StagedObjects: repos.Staged,
		DiskMetrics:   metrics,
		HostDiskPath:  "/",
	})
	require.NoError(t, err)
	return svc
}

func countTable(t *testing.T, db *pgxpool.Pool, table string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+table).Scan(&count))
	return count
}

// A user's cancellation reaches Builder's status poll through its own client
// and credential, which is how a running build learns to stop.
func TestBuilderStatusPollSeesCancellation(t *testing.T) {
	ctx := context.Background()
	repos := coretest.OpenCoreRepositories(t)
	ownerID := uuid.New()
	builds := newBuildService(t, repos, ownerID, map[string][]byte{}, false)
	result, err := builds.CreateBuildFromArchive(coretest.UserContext(ownerID), service.BuildArchiveInput{
		Tag:         "cancel-me:latest",
		ContextDir:  "app",
		Dockerfile:  "Dockerfile",
		ArchiveName: "source.zip",
		Archive:     strings.NewReader("archive-bytes"),
	})
	require.NoError(t, err)

	builderConn := coretest.NewCoreGRPCConn(t, coretest.CoreGRPCRegistration{Builds: builds},
		grpc.WithUnaryInterceptor(internalauth.SystemScopeUnaryClientInterceptor(coretest.BuilderCredential)))
	core := builderclient.NewCoreClient(builderConn)
	_, didStart, _, err := core.StartBuildRecord(ctx, result.BuildID.String())
	require.NoError(t, err)
	require.True(t, didStart)

	require.NoError(t, builds.CancelBuildRecord(coretest.UserContext(ownerID), result.BuildID))

	polled, err := core.GetBuildStatus(ctx, result.BuildID.String())
	require.NoError(t, err)
	require.Equal(t, buildermodel.BuildStatus(model.BuildStatusCanceled), polled)
}
