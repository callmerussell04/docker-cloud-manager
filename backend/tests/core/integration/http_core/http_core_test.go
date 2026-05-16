package http_core_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	corehttp "github.com/callmerussell04/docker-cloud-manager/internal/core/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	composemocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCoreHTTPRouterRequiresInternalToken(t *testing.T) {
	router, _ := newHTTPRouter(t, coretest.IntegrationConfig(), uuid.New(), map[string][]byte{})
	scope := accessscope.Scope{Kind: accessscope.KindUser, UserID: uuid.New(), Username: "alice", Role: "user"}

	req := coretest.InternalHTTPRequest(http.MethodGet, "/api/v1/images/build/availability", "", scope, nil, "")
	resp := coretest.PerformHTTP(router, req)
	require.Equal(t, http.StatusUnauthorized, resp.Code)

	req = coretest.InternalHTTPRequest(http.MethodGet, "/api/v1/images/build/availability", "wrong", scope, nil, "")
	resp = coretest.PerformHTTP(router, req)
	require.Equal(t, http.StatusUnauthorized, resp.Code)

	req = coretest.InternalHTTPRequest(http.MethodGet, "/api/v1/images/build/availability", "core-token", scope, nil, "")
	resp = coretest.PerformHTTP(router, req)
	require.Equal(t, http.StatusOK, resp.Code)
}

func TestCoreHTTPBuildAndComposeUploadsPersistToDatabase(t *testing.T) {
	ownerID := uuid.New()
	objects := map[string][]byte{}
	router, repos := newHTTPRouter(t, coretest.IntegrationConfig(), ownerID, objects)
	scope := accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID, Username: "alice", Role: "user"}

	body, contentType := coretest.MultipartBody(t, []coretest.FormPart{
		{Name: "tag", Value: "http-demo:latest"},
		{Name: "context", Value: "app"},
		{Name: "archive", FileName: "source.zip", Value: "archive"},
	})
	req := coretest.InternalHTTPRequest(http.MethodPost, "/api/v1/images/build", "core-token", scope, body, contentType)
	resp := coretest.PerformHTTP(router, req)
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Equal(t, 1, countTable(t, repos.DB, "builds"))
	require.Equal(t, 1, countTable(t, repos.DB, "build_queue_outbox"))

	var buildID uuid.UUID
	var logObjectKey string
	require.NoError(t, repos.DB.QueryRow(`SELECT id, log_file_path FROM builds LIMIT 1`).Scan(&buildID, &logObjectKey))
	objects[logObjectKey] = []byte("build logs\n")
	req = coretest.InternalHTTPRequest(http.MethodGet, "/api/v1/builds/"+buildID.String()+"/logs", "core-token", scope, nil, "")
	resp = coretest.PerformHTTP(router, req)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Equal(t, "build logs\n", resp.Body.String())

	composeBody, composeType := coretest.MultipartBody(t, []coretest.FormPart{
		{Name: "project_name", Value: "http-compose"},
		{Name: "archive", FileName: "compose.zip", Value: string(composeZip(t, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx:latest\n"}))},
	})
	req = coretest.InternalHTTPRequest(http.MethodPost, "/api/v1/projects/compose", "core-token", scope, composeBody, composeType)
	resp = coretest.PerformHTTP(router, req)
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Equal(t, 1, countTable(t, repos.DB, "projects"))
	require.Equal(t, 1, countTable(t, repos.DB, "compose_deployment_jobs"))
}

func TestCoreHTTPValidationErrorsAreSafe(t *testing.T) {
	ownerID := uuid.New()
	router, _ := newHTTPRouter(t, coretest.IntegrationConfig(), ownerID, map[string][]byte{})
	scope := accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID, Username: "alice", Role: "user"}

	body, contentType := coretest.MultipartBody(t, []coretest.FormPart{{Name: "archive", FileName: "source.zip", Value: "archive"}})
	req := coretest.InternalHTTPRequest(http.MethodPost, "/api/v1/images/build", "core-token", scope, body, contentType)
	resp := coretest.PerformHTTP(router, req)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Contains(t, resp.Body.String(), `"error_code"`)

	req = coretest.InternalHTTPRequest(http.MethodPost, "/api/v1/images/build/git", "core-token", scope, strings.NewReader(`{"repo_url":"ftp://example.test/repo.git","tag":"bad"}`), "application/json")
	resp = coretest.PerformHTTP(router, req)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.NotContains(t, resp.Body.String(), "postgres")
	require.NotContains(t, resp.Body.String(), "docker")

	req = coretest.InternalHTTPRequest(http.MethodGet, "/api/v1/builds/not-a-uuid/logs", "core-token", scope, nil, "")
	resp = coretest.PerformHTTP(router, req)
	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func newHTTPRouter(t *testing.T, cfgValue config.SystemConfig, ownerID uuid.UUID, objects map[string][]byte) (http.Handler, coretest.CoreRepositories) {
	t.Helper()
	repos := coretest.OpenCoreRepositories(t)
	cfg := coretest.StaticConfig{Config: cfgValue}
	store := composemocks.NewObjectStorage(t)
	configureObjectStorageMock(store, objects)

	users := coremocks.NewUserInfoProvider(t)
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, Username: "alice", QuotaRAMMB: 4096, QuotaDiskMB: 4096, QuotaCPU: 4}, nil).Maybe()
	metrics := coremocks.NewHostDiskMetricsProvider(t)
	metrics.EXPECT().GetDiskUsage(mock.Anything).Return(model.HostDiskStats{FreeBytes: 1 << 40}, nil).Maybe()
	registry := coremocks.NewImageRegistryAPI(t)
	registry.EXPECT().GetImageSizeAndDigest(mock.Anything, mock.Anything, mock.Anything).Return(int64(1<<20), "sha256:digest", nil).Maybe()

	builds := service.NewBuildService(repos.Builds, repos.Images, registry, users, coretest.DiscardLogger(), service.BuildServiceDeps{
		VolumeRepo:    repos.Volumes,
		Config:        cfg,
		ObjectStore:   store,
		StagedObjects: repos.Staged,
		DiskMetrics:   metrics,
		HostDiskPath:  "/",
	})
	orchestrator := compose.NewOrchestrator(context.Background(), repos.Projects, repos.Builds, composemocks.NewVolumeService(t), composemocks.NewContainerService(t), composemocks.NewComposeDockerAPI(t), cfg, store, builds, composemocks.NewImageCleaner(t), coretest.DiscardLogger())
	orchestrator.SetStagedObjectRepository(repos.Staged)
	orchestrator.SetHostDiskGuard(metrics, "/")
	orchestrator.SetUserInfoProvider(users)

	router := coretest.CoreHTTPRouter(corehttp.NewComposeHandler(orchestrator, cfg), corehttp.NewBuildHandler(builds, cfg), "core-token")
	return router, repos
}

func configureObjectStorageMock(store *composemocks.ObjectStorage, objects map[string][]byte) {
	store.EXPECT().UploadStream(mock.Anything, mock.AnythingOfType("string"), mock.Anything, mock.AnythingOfType("int64"), mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r); err != nil {
				return err
			}
			objects[key] = buf.Bytes()
			return nil
		}).Maybe()
	store.EXPECT().NewReaderAt(mock.Anything, mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, key string) (io.ReaderAt, int64, error) {
			data, ok := objects[key]
			if !ok {
				return nil, 0, apperrors.ErrNotFound
			}
			return bytes.NewReader(data), int64(len(data)), nil
		}).Maybe()
	store.EXPECT().OpenObject(mock.Anything, mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, key string) (io.ReadCloser, error) {
			data, ok := objects[key]
			if !ok {
				return nil, apperrors.ErrNotFound
			}
			return io.NopCloser(bytes.NewReader(data)), nil
		}).Maybe()
	store.EXPECT().CopyObject(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	store.EXPECT().DeleteObject(mock.Anything, mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, key string) error {
			delete(objects, key)
			return nil
		}).Maybe()
}

func composeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zipWriter{buf: &buf}
	zw.write(t, files)
	return buf.Bytes()
}

type zipWriter struct {
	buf *bytes.Buffer
}

func (w zipWriter) write(t *testing.T, files map[string]string) {
	t.Helper()
	archive := zip.NewWriter(w.buf)
	for name, contents := range files {
		part, err := archive.Create(name)
		require.NoError(t, err)
		_, err = io.Copy(part, strings.NewReader(contents))
		require.NoError(t, err)
	}
	require.NoError(t, archive.Close())
}

func countTable(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&count))
	return count
}
