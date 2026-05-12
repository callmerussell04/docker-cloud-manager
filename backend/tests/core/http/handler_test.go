package http_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	corehttp "github.com/callmerussell04/docker-cloud-manager/internal/core/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	corehttpmocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/http"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBuildHandlerBuildImageAcceptsMultipartArchive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buildID := uuid.New()
	builds := corehttpmocks.NewMockBuildUseCases(t)
	var archiveInput service.BuildArchiveInput
	var archiveData string
	builds.EXPECT().
		CreateBuildFromArchive(mock.Anything, mock.AnythingOfType("service.BuildArchiveInput")).
		Run(func(ctx context.Context, input service.BuildArchiveInput) {
			archiveInput = input
			data, err := io.ReadAll(input.Archive)
			require.NoError(t, err)
			archiveData = string(data)
		}).
		Return(service.BuildInitResult{BuildID: buildID}, nil)
	router := gin.New()
	router.POST("/images/build", corehttp.NewBuildHandler(builds, newHTTPConfigMock(t)).BuildImage)
	body, contentType := multipartBody(t, []formPart{
		{name: "tag", value: "demo"},
		{name: "context", value: "app"},
		{name: "dockerfile", value: "Dockerfile.prod"},
		{name: "build_args", value: `{"VERSION":"1"}`},
		{name: "archive", fileName: "source.zip", value: "zip"},
	})

	resp := perform(router, http.MethodPost, "/images/build", contentType, body)

	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Equal(t, "demo", archiveInput.Tag)
	require.Equal(t, "app", archiveInput.ContextDir)
	require.Equal(t, "Dockerfile.prod", archiveInput.Dockerfile)
	require.Equal(t, map[string]string{"VERSION": "1"}, archiveInput.BuildArgs)
	require.Equal(t, "source.zip", archiveInput.ArchiveName)
	require.Equal(t, "zip", archiveData)
}

func TestBuildHandlerBuildImageRejectsArchiveBeforeMetadataAndInvalidArgs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name  string
		parts []formPart
	}{
		{
			name:  "archive before tag",
			parts: []formPart{{name: "archive", fileName: "source.zip", value: "zip"}},
		},
		{
			name: "invalid build args",
			parts: []formPart{
				{name: "tag", value: "demo"},
				{name: "build_args", value: `not-json`},
				{name: "archive", fileName: "source.zip", value: "zip"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.POST("/images/build", corehttp.NewBuildHandler(corehttpmocks.NewMockBuildUseCases(t), newHTTPConfigMock(t)).BuildImage)
			body, contentType := multipartBody(t, tt.parts)
			resp := perform(router, http.MethodPost, "/images/build", contentType, body)
			require.Equal(t, http.StatusBadRequest, resp.Code)
		})
	}
}

func TestBuildHandlerGitLogsAndAvailability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buildID := uuid.New()
	builds := corehttpmocks.NewMockBuildUseCases(t)
	var gitInput service.BuildGitInput
	builds.EXPECT().
		CreateBuildFromGit(mock.Anything, mock.AnythingOfType("service.BuildGitInput")).
		Run(func(ctx context.Context, input service.BuildGitInput) {
			gitInput = input
		}).
		Return(service.BuildInitResult{BuildID: buildID}, nil)
	builds.EXPECT().
		OpenBuildLogs(mock.Anything, buildID).
		Return(io.NopCloser(strings.NewReader("line1\nline2\n")), nil)
	router := gin.New()
	handler := corehttp.NewBuildHandler(builds, newHTTPConfigMock(t))
	router.POST("/images/build/git", handler.BuildImageFromGit)
	router.GET("/images/build/availability", handler.BuildAvailability)
	router.GET("/builds/:id/logs", handler.BuildLogs)

	resp := perform(router, http.MethodPost, "/images/build/git", "application/json", strings.NewReader(`{"repo_url":"https://github.com/acme/app.git","tag":"demo","build_args":{"A":"B"}}`))
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Equal(t, "https://github.com/acme/app.git", gitInput.RepoURL)
	require.Equal(t, map[string]string{"A": "B"}, gitInput.BuildArgs)

	resp = perform(router, http.MethodGet, "/images/build/availability", "", nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"enabled":true`)
	require.Contains(t, resp.Body.String(), `"git_sources_enabled":true`)

	resp = perform(router, http.MethodGet, "/builds/"+buildID.String()+"/logs", "", nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Equal(t, "line1\nline2\n", resp.Body.String())

	resp = perform(router, http.MethodGet, "/builds/not-a-uuid/logs", "", nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestComposeHandlerDeployComposeAndGit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	projectID := uuid.New()
	composeSvc := corehttpmocks.NewMockComposeUseCases(t)
	var projectName string
	var archiveName string
	var archiveData string
	var gitProjectName string
	var gitSource model.GitSource
	composeSvc.EXPECT().
		StartDeployment(mock.Anything, "demo", "compose.zip", mock.Anything).
		Run(func(ctx context.Context, name, fileName string, archive io.Reader) {
			projectName = name
			archiveName = fileName
			data, err := io.ReadAll(archive)
			require.NoError(t, err)
			archiveData = string(data)
		}).
		Return(projectID, nil)
	composeSvc.EXPECT().
		StartGitDeployment(mock.Anything, "demo", mock.AnythingOfType("model.GitSource")).
		Run(func(ctx context.Context, name string, source model.GitSource) {
			gitProjectName = name
			gitSource = source
		}).
		Return(projectID, nil)
	router := gin.New()
	handler := corehttp.NewComposeHandler(composeSvc, newHTTPConfigMock(t))
	router.POST("/projects/compose", handler.DeployCompose)
	router.POST("/projects/compose/git", handler.DeployComposeFromGit)

	body, contentType := multipartBody(t, []formPart{
		{name: "project_name", value: "demo"},
		{name: "archive", fileName: "compose.zip", value: "zip"},
	})
	resp := perform(router, http.MethodPost, "/projects/compose", contentType, body)
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Equal(t, "demo", projectName)
	require.Equal(t, "compose.zip", archiveName)
	require.Equal(t, "zip", archiveData)

	resp = perform(router, http.MethodPost, "/projects/compose/git", "application/json", strings.NewReader(`{"project_name":"demo","repo_url":"https://github.com/acme/app.git","ref":"main","compose_file":"deploy/docker-compose.yml"}`))
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Equal(t, "demo", gitProjectName)
	require.Equal(t, "https://github.com/acme/app.git", gitSource.RepoURL)
	require.Equal(t, "deploy/docker-compose.yml", gitSource.ComposeFile)
}

func TestComposeHandlerRejectsInvalidUploadAndGitPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := corehttp.NewComposeHandler(corehttpmocks.NewMockComposeUseCases(t), newHTTPConfigMock(t))
	router.POST("/projects/compose", handler.DeployCompose)
	router.POST("/projects/compose/git", handler.DeployComposeFromGit)

	body, contentType := multipartBody(t, []formPart{{name: "archive", fileName: "compose.zip", value: "zip"}})
	resp := perform(router, http.MethodPost, "/projects/compose", contentType, body)
	require.Equal(t, http.StatusBadRequest, resp.Code)

	resp = perform(router, http.MethodPost, "/projects/compose/git", "application/json", strings.NewReader(`{"project_name":"demo"}`))
	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func newHTTPConfigMock(t *testing.T) *corehttpmocks.MockConfigProvider {
	t.Helper()
	cfg := corehttpmocks.NewMockConfigProvider(t)
	cfg.EXPECT().Get().Return(config.SystemConfig{
		MaxUploadSizeBytes:    10 << 20,
		ComposeUploadMaxBytes: 10 << 20,
		ImageBuildsEnabled:    true,
		GitSourcesEnabled:     true,
	}).Maybe()
	return cfg
}

type formPart struct {
	name     string
	fileName string
	value    string
}

func multipartBody(t *testing.T, parts []formPart) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for _, part := range parts {
		var w io.Writer
		var err error
		if part.fileName != "" {
			w, err = writer.CreateFormFile(part.name, part.fileName)
		} else {
			w, err = writer.CreateFormField(part.name)
		}
		require.NoError(t, err)
		_, err = io.WriteString(w, part.value)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return &buf, writer.FormDataContentType()
}

func perform(router http.Handler, method, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}
