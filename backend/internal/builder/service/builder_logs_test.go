package service_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	builderdocker "github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dockertest"
	builderservicemocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/builder/service"
)

// Builder's own logs, at every level, never carry the scoped Registry
// password, secret build-argument values or object keys, on success or on any
// failure path. The real Docker adapter runs against the fake daemon so its
// errors reach the log as they would in production.
func TestBuilderServiceLogsExcludeCredentialsBuildArgsAndObjectKeys(t *testing.T) {
	const (
		registryPassword = "scoped-registry-password-7f3a"
		buildArgSecret   = "build-arg-secret-value-91c2"
	)
	container := "/containers/build-container"
	created := dockertest.Created(`{"Id":"build-container"}`)
	noContent := dockertest.NoContent()
	daemonFailure := dockertest.Error(http.StatusInternalServerError, "daemon failure")
	for _, tt := range []struct {
		name        string
		daemon      map[string]dockertest.Response
		uploadFails bool
		wantMsg     string
	}{
		{
			name: "successful build",
			daemon: map[string]dockertest.Response{
				container + "/wait":   dockertest.OK(`{"StatusCode":0}`),
				"DELETE " + container: noContent,
			},
			wantMsg: "build completed",
		},
		{
			name: "container start fails",
			daemon: map[string]dockertest.Response{
				container + "/start":  daemonFailure,
				"DELETE " + container: noContent,
			},
			wantMsg: "failed to start build container",
		},
		{
			name: "build fails and container removal fails",
			daemon: map[string]dockertest.Response{
				container + "/wait":   dockertest.OK(`{"StatusCode":1}`),
				"DELETE " + container: daemonFailure,
			},
			wantMsg: "failed to remove build container",
		},
		{
			name: "build log upload fails",
			daemon: map[string]dockertest.Response{
				container + "/wait":   dockertest.OK(`{"StatusCode":0}`),
				"DELETE " + container: noContent,
			},
			uploadFails: true,
			wantMsg:     "failed to upload final build log",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			routes := map[string]dockertest.Response{
				"/images/kaniko:test/json": dockertest.OK(`{"Id":"kaniko"}`),
				"/containers/create":       created,
				container + "/archive":     dockertest.OK(""),
				container + "/start":       noContent,
				container + "/logs":        dockertest.OK(""),
			}
			for path, response := range tt.daemon {
				routes[path] = response
			}
			dockertest.Serve(t, routes)
			adapter, err := builderdocker.NewAdapter(builderdocker.Config{RegistryCAFile: dockertest.RegistryCAFile(t)})
			require.NoError(t, err)
			t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

			h := newBuilderHarness(t, testBuilderRuntimeConfig())
			var output bytes.Buffer
			deps := h.deps()
			deps.Docker = adapter
			if tt.uploadFails {
				// The real store's error reaches the log, as it would in production.
				deps.ObjectStore = rejectingUploads{ObjectStorage: h.objects, uploads: rejectingObjectStore(t)}
			}
			deps.Logger = slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
			h.service = newBuilderService(t, deps)

			msg := testBuildMessage()
			logPath := filepath.Join(t.TempDir(), "build.log")
			require.NoError(t, os.WriteFile(logPath, []byte("build output\n"), 0o600))
			msg.BuildArgs = map[string]string{"API_TOKEN": buildArgSecret}
			h.core.EXPECT().StartBuildRecord(mock.Anything, msg.BuildID).Return(model.BuildClaim{
				ImageID:          msg.ImageID,
				RegistryUsername: msg.BuildID,
				RegistryPassword: registryPassword,
				Destination:      "registry:5000/owner-id_app:latest",
			}, true, "running", nil).Once()
			h.workspaces.EXPECT().ArchivePath(msg.BuildID, msg.ArchiveObjectKey).Return("/tmp/archive.zip")
			h.objects.EXPECT().DownloadFile(mock.Anything, msg.ArchiveObjectKey, "/tmp/archive.zip").Return(nil)
			h.files.EXPECT().ValidateArchive("/tmp/archive.zip", msg.ContextDir, msg.Dockerfile).Return(nil)
			h.files.EXPECT().CleanUp("/tmp/archive.zip").Return(nil)
			h.workspaces.EXPECT().Create(msg.BuildID).Return(t.TempDir(), func() {}, nil)
			h.extractor.EXPECT().Extract("/tmp/archive.zip", mock.Anything, mock.Anything).Return(nil)
			h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, mock.Anything).
				RunAndReturn(func(_ string, logs io.Reader, _ int64) (string, error) {
					_, err := io.Copy(io.Discard, logs)
					return logPath, err
				}).Maybe()
			h.logs.EXPECT().WriteSystemLog(mock.Anything, msg.BuildID, mock.Anything, mock.Anything).Return(nil).Maybe()
			h.logs.EXPECT().Exists(msg.BuildID).Return(true, nil)
			h.logs.EXPECT().LogPath(msg.BuildID).Return(logPath)
			h.logs.EXPECT().CleanUp(msg.BuildID).Return(nil)
			if !tt.uploadFails {
				h.objects.EXPECT().UploadFile(mock.Anything, msg.LogObjectKey, logPath, "text/plain").Return(nil)
			}
			h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, mock.Anything, 0).Return(nil)
			h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)

			require.NoError(t, h.service.HandleBuildMessage(context.Background(), msg))

			logs := output.String()
			require.Contains(t, logs, `"msg":"`+tt.wantMsg+`"`, "the case must reach its logging path")
			for _, sensitive := range []string{registryPassword, buildArgSecret, msg.ArchiveObjectKey, msg.LogObjectKey} {
				require.NotContains(t, logs, sensitive)
			}
		})
	}
}

var _ service.DockerAPI = (*builderdocker.Adapter)(nil)

// rejectingUploads sends uploads to a real MinIO store and everything else to
// the mock.
type rejectingUploads struct {
	*builderservicemocks.ObjectStorage
	uploads *objectstorage.MinIOStorage
}

func (r rejectingUploads) UploadFile(ctx context.Context, objectKey, filePath, contentType string) error {
	return r.uploads.UploadFile(ctx, objectKey, filePath, contentType)
}

// rejectingObjectStore returns a MinIO store whose bucket exists but whose
// object writes all fail.
func rejectingObjectStore(t *testing.T) *objectstorage.MinIOStorage {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Has("location"):
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`) // test server; a failed write fails the client side
		case r.URL.Path == "/bucket" || r.URL.Path == "/bucket/":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	t.Cleanup(server.Close)
	store, err := objectstorage.NewMinIOStorage(t.Context(), objectstorage.Config{
		Endpoint:  strings.TrimPrefix(server.URL, "http://"),
		Bucket:    "bucket",
		AccessKey: "access",
		SecretKey: "secret",
	})
	require.NoError(t, err)
	return store
}
