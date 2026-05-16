package coretest

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	coregrpc "github.com/callmerussell04/docker-cloud-manager/internal/core/grpc"
	corehttp "github.com/callmerussell04/docker-cloud-manager/internal/core/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/objectstorage"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

type StaticConfig struct {
	Config config.SystemConfig
}

func (c StaticConfig) Get() config.SystemConfig {
	return c.Config
}

func IntegrationConfig() config.SystemConfig {
	cfg := SystemConfig()
	cfg.BaseDomain = "example.test"
	cfg.RegistryAPIURL = "registry.test:5000"
	cfg.RegistryPublicURL = "registry-public.test:5000"
	cfg.MaxVolumesPerUser = 10
	cfg.MaxContainersPerUser = 10
	cfg.MaxQueuedBuildsPerUser = 10
	cfg.MaxQueuedContainerCreatesPerUser = 10
	cfg.MaxQueuedComposeDeploysPerUser = 10
	cfg.MaxStagedSourceBytesPerUser = 100 << 20
	cfg.HostMinFreeDiskBytes = 0
	cfg.ContainerTTLHours = 0
	cfg.ComposeUploadMaxBytes = 10 << 20
	cfg.MaxUploadSizeBytes = 10 << 20
	cfg.MaxArchiveSizeBytes = 10 << 20
	cfg.GitSourcesEnabled = true
	cfg.ImageBuildsEnabled = true
	return cfg
}

type CoreRepositories struct {
	DB         *sql.DB
	Builds     *repository.BuildRepository
	Images     *repository.ImageRepository
	Volumes    *repository.VolumeRepository
	Containers *repository.ContainerRepository
	Projects   *repository.ProjectRepository
	Staged     *repository.StagedObjectRepository
	Reports    *repository.ReportSnapshotRepository
}

func OpenCoreRepositories(t *testing.T) CoreRepositories {
	t.Helper()
	db := dbtest.OpenCorePostgres(t)
	return CoreRepositories{
		DB:         db,
		Builds:     repository.NewBuildRepository(db),
		Images:     repository.NewImageRepository(db),
		Volumes:    repository.NewVolumeRepository(db),
		Containers: repository.NewContainerRepository(db),
		Projects:   repository.NewProjectRepository(db),
		Staged:     repository.NewStagedObjectRepository(db),
		Reports:    repository.NewReportSnapshotRepository(db, nil, DiscardLogger()),
	}
}

type CoreGRPCRegistration struct {
	Containers coregrpc.ContainerLogic
	Volumes    coregrpc.VolumeLogic
	Images     coregrpc.ImageLogic
	Builds     coregrpc.BuildLogic
	Projects   coregrpc.ProjectLogic
	System     coregrpc.SystemLogic
	Stats      coregrpc.StatsLogic
	Reports    coregrpc.ReportLogic
	Users      coregrpc.UserDirectory
}

func NewCoreGRPCConn(t *testing.T, token string, reg CoreGRPCRegistration) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(internalauth.UnaryServerInterceptor(token)))
	if reg.Containers != nil {
		coregrpc.RegisterContainerAPI(server, reg.Containers, reg.Users)
	}
	if reg.Volumes != nil {
		coregrpc.RegisterVolumeAPI(server, reg.Volumes, reg.Users)
	}
	if reg.Images != nil {
		coregrpc.RegisterImageAPI(server, reg.Images, reg.Builds, reg.Users)
	}
	if reg.Projects != nil {
		coregrpc.RegisterProjectAPI(server, reg.Projects, reg.Users)
	}
	if reg.System != nil {
		coregrpc.RegisterSystemAPI(server, reg.System)
	}
	if reg.Stats != nil {
		coregrpc.RegisterStatsAPI(server, reg.Stats)
	}
	if reg.Reports != nil {
		coregrpc.RegisterReportAPI(server, reg.Reports)
	}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func InternalGRPCContext(ctx context.Context, token string, scope accessscope.Scope) context.Context {
	pairs := []string{internalauth.MetadataKey, token, internalauth.MetadataScope, string(scope.Kind)}
	if scope.UserID != uuid.Nil {
		pairs = append(pairs, internalauth.MetadataUserID, scope.UserID.String())
	}
	if scope.Username != "" {
		pairs = append(pairs, internalauth.MetadataUsername, scope.Username)
	}
	if scope.Role != "" {
		pairs = append(pairs, internalauth.MetadataRole, scope.Role)
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

func CoreHTTPRouter(composeHandler *corehttp.ComposeHandler, buildHandler *corehttp.BuildHandler, token string) http.Handler {
	return corehttp.SetupRouter(composeHandler, buildHandler, token, DiscardLogger())
}

func InternalHTTPRequest(method, path, token string, scope accessscope.Scope, body io.Reader, contentType string) *http.Request {
	req := httptest.NewRequest(method, path, body)
	if token != "" {
		req.Header.Set(internalauth.HeaderName, token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set(internalauth.HeaderScope, string(scope.Kind))
	if scope.UserID != uuid.Nil {
		req.Header.Set(internalauth.HeaderUserID, scope.UserID.String())
	}
	if scope.Username != "" {
		req.Header.Set(internalauth.HeaderUsername, scope.Username)
	}
	if scope.Role != "" {
		req.Header.Set(internalauth.HeaderRole, scope.Role)
	}
	return req
}

func PerformHTTP(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

type FormPart struct {
	Name     string
	FileName string
	Value    string
}

func MultipartBody(t *testing.T, parts []FormPart) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for _, part := range parts {
		var w io.Writer
		var err error
		if part.FileName != "" {
			w, err = writer.CreateFormFile(part.Name, part.FileName)
		} else {
			w, err = writer.CreateFormField(part.Name)
		}
		require.NoError(t, err)
		_, err = io.Copy(w, strings.NewReader(part.Value))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return &buf, writer.FormDataContentType()
}

func ObjectStorageConfigFromEnv(t *testing.T) objectstorage.Config {
	t.Helper()
	cfg := objectstorage.Config{
		Endpoint:  getenv("CORE_TEST_OBJECT_STORAGE_ENDPOINT"),
		Bucket:    getenv("CORE_TEST_OBJECT_STORAGE_BUCKET"),
		AccessKey: getenv("CORE_TEST_OBJECT_STORAGE_ACCESS_KEY"),
		SecretKey: getenv("CORE_TEST_OBJECT_STORAGE_SECRET_KEY"),
		UseSSL:    getenv("CORE_TEST_OBJECT_STORAGE_USE_SSL") == "1",
	}
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		t.Skip("set CORE_TEST_OBJECT_STORAGE_ENDPOINT, CORE_TEST_OBJECT_STORAGE_BUCKET, CORE_TEST_OBJECT_STORAGE_ACCESS_KEY and CORE_TEST_OBJECT_STORAGE_SECRET_KEY")
	}
	return cfg
}

func RabbitMQURLFromEnv(t *testing.T) string {
	t.Helper()
	url := getenv("CORE_TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("set CORE_TEST_RABBITMQ_URL to run Core RabbitMQ integration tests")
	}
	return url
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
