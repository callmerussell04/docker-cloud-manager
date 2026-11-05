package http_core_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	corehttp "github.com/callmerussell04/docker-cloud-manager/backend/internal/core/http"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCoreHTTPRouterRequiresInternalToken(t *testing.T) {
	router, _ := newHTTPRouter(t, coretest.IntegrationConfig())
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
	router, f := newHTTPRouter(t, coretest.IntegrationConfig())
	scope := accessscope.Scope{Kind: accessscope.KindUser, UserID: f.OwnerID, Username: "alice", Role: "user"}

	body, contentType := coretest.MultipartBody(t, []coretest.FormPart{
		{Name: "tag", Value: "http-demo:latest"},
		{Name: "context", Value: "app"},
		{Name: "archive", FileName: "source.zip", Value: "archive"},
	})
	req := coretest.InternalHTTPRequest(http.MethodPost, "/api/v1/images/build", "core-token", scope, body, contentType)
	resp := coretest.PerformHTTP(router, req)
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Equal(t, 1, dbtest.CountRows(t, f.Repos.DB, "builds"))
	require.Equal(t, 1, dbtest.CountRows(t, f.Repos.DB, "build_queue_outbox"))

	var buildID uuid.UUID
	var logObjectKey string
	require.NoError(t, f.Repos.DB.QueryRow(context.Background(), `SELECT id, log_file_path FROM builds LIMIT 1`).Scan(&buildID, &logObjectKey))
	f.Objects.Put(logObjectKey, []byte("build logs\n"))
	req = coretest.InternalHTTPRequest(http.MethodGet, "/api/v1/builds/"+buildID.String()+"/logs", "core-token", scope, nil, "")
	resp = coretest.PerformHTTP(router, req)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Equal(t, "build logs\n", resp.Body.String())

	composeBody, composeType := coretest.MultipartBody(t, []coretest.FormPart{
		{Name: "project_name", Value: "http-compose"},
		{Name: "archive", FileName: "compose.zip", Value: string(coretest.ComposeZip(t, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx:latest\n"}))},
	})
	req = coretest.InternalHTTPRequest(http.MethodPost, "/api/v1/projects/compose", "core-token", scope, composeBody, composeType)
	resp = coretest.PerformHTTP(router, req)
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Equal(t, 1, dbtest.CountRows(t, f.Repos.DB, "projects"))
	require.Equal(t, 1, dbtest.CountRows(t, f.Repos.DB, "compose_deployment_jobs"))
}

func TestCoreHTTPValidationErrorsAreSafe(t *testing.T) {
	router, f := newHTTPRouter(t, coretest.IntegrationConfig())
	scope := accessscope.Scope{Kind: accessscope.KindUser, UserID: f.OwnerID, Username: "alice", Role: "user"}

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

// newHTTPRouter serves Core's build and Compose HTTP routes over the shared
// Compose fixture.
func newHTTPRouter(t *testing.T, cfg config.SystemConfig) (http.Handler, *coretest.ComposeFixture) {
	t.Helper()
	f := coretest.NewComposeFixture(t, cfg)
	router := coretest.CoreHTTPRouter(corehttp.NewComposeHandler(f.Orchestrator, f.Config), corehttp.NewBuildHandler(f.Builds, f.Config), "core-token")
	return router, f
}
