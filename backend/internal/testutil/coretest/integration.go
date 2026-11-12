package coretest

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	coregrpc "github.com/callmerussell04/docker-cloud-manager/backend/internal/core/grpc"
	corehttp "github.com/callmerussell04/docker-cloud-manager/backend/internal/core/http"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/userquota"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/pgpool"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/testenv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
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
	DB         *pgxpool.Pool
	LockDB     *pgxpool.Pool
	Builds     *repository.BuildRepository
	Images     *repository.ImageRepository
	Volumes    *repository.VolumeRepository
	Containers *repository.ContainerRepository
	Projects   *repository.ProjectRepository
	Staged     *repository.StagedObjectRepository
	Reports    *repository.ReportSnapshotRepository
	Grants     *repository.RegistryGrantRepository
	Lifecycle  *repository.ResourceLifecycleRepository
	Quotas     *repository.UserQuotaRepository
}

// UserQuotas returns the production effective-quota source over the test
// database and cfg.
func (r CoreRepositories) UserQuotas(t *testing.T, cfg userquota.ConfigProvider) *userquota.Service {
	t.Helper()
	quotas, err := userquota.New(r.Quotas, cfg)
	require.NoError(t, err)
	return quotas
}

// SetCustomQuota gives ownerID a custom quota in the test database.
func (r CoreRepositories) SetCustomQuota(t *testing.T, ownerID uuid.UUID, quota model.UserQuota) {
	t.Helper()
	require.NoError(t, r.Quotas.SetCustomUserQuota(context.Background(), ownerID, quota))
}

// RegistryAuthorization returns production Registry authorization over the
// test database. It claims, revokes and fences builds; it does not issue
// tokens, so its users and signer mocks fail any call.
func (r CoreRepositories) RegistryAuthorization(t *testing.T) *service.RegistryAuthorization {
	t.Helper()
	auth, err := service.NewRegistryAuthorization(service.RegistryAuthorizationDeps{
		Repository: r.Grants, Users: coremocks.NewUserInfoProvider(t), Signer: coremocks.NewRegistryTokenSigner(t), Authority: "registry.test",
	})
	require.NoError(t, err)
	return auth
}

func OpenCoreRepositories(t *testing.T) CoreRepositories {
	t.Helper()
	base := dbtest.OpenCorePostgres(t)
	queryCfg, lockCfg, err := pgpool.DefaultConfig().CoreBudgets()
	require.NoError(t, err)
	db := dbtest.OpenCorePeerPool(t, base, queryCfg)
	lockDB := dbtest.OpenCorePeerPool(t, base, lockCfg)
	return CoreRepositories{
		LockDB:     lockDB,
		DB:         db,
		Builds:     repository.NewBuildRepository(db, lockDB),
		Images:     repository.NewImageRepository(db),
		Volumes:    repository.NewVolumeRepository(db),
		Containers: repository.NewContainerRepository(db, lockDB),
		Projects:   repository.NewProjectRepository(db),
		Staged:     repository.NewStagedObjectRepository(db),
		Reports:    repository.NewReportSnapshotRepository(db, nil, DiscardLogger()),
		Grants:     repository.NewRegistryGrantRepository(db, lockDB),
		Lifecycle:  repository.NewResourceLifecycleRepository(db),
		Quotas:     repository.NewUserQuotaRepository(db),
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
	Quotas     coregrpc.QuotaLogic
	Users      coregrpc.UserDirectory
}

// The credentials Core's test servers accept, under Core's production policy
// table.
const (
	GatewayCredential = "gateway-test-credential"
	BuilderCredential = "builder-test-credential"
)

// CoreCallers returns Core's production authenticator over the test
// credentials.
func CoreCallers(t *testing.T) *internalauth.Authenticator {
	t.Helper()
	callers, err := internalauth.NewAuthenticator(coregrpc.CallerPolicies(coregrpc.CallerCredentials{
		Gateway: GatewayCredential, Builder: BuilderCredential,
	})...)
	require.NoError(t, err)
	return callers
}

// NewCoreGRPCConn serves reg behind Core's production caller policies.
func NewCoreGRPCConn(t *testing.T, reg CoreGRPCRegistration, clientOptions ...grpc.DialOption) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(CoreCallers(t).UnaryServerInterceptor()))
	if reg.Containers != nil {
		coregrpc.RegisterContainerAPI(server, reg.Containers, reg.Users)
	}
	if reg.Volumes != nil {
		coregrpc.RegisterVolumeAPI(server, reg.Volumes, reg.Users)
	}
	if reg.Images != nil || reg.Builds != nil {
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
	if reg.Quotas != nil {
		coregrpc.RegisterQuotaAPI(server, reg.Quotas)
	}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	options := []grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Bound every fixture RPC, including callers that omit a deadline.
		grpc.WithDefaultServiceConfig(`{"methodConfig":[{"name":[{}],"timeout":"5s"}]}`),
	}
	conn, err := grpc.NewClient("passthrough:///bufnet", append(options, clientOptions...)...)
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

// CoreHTTPRouter builds Core's internal router, behind Core's production
// caller policies, without container streams; a request to a stream route
// panics into the recovery middleware's 500.
func CoreHTTPRouter(t *testing.T, composeHandler *corehttp.ComposeHandler, buildHandler *corehttp.BuildHandler) http.Handler {
	t.Helper()
	return corehttp.SetupRouter(composeHandler, buildHandler, nil, CoreCallers(t), DiscardLogger())
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

// ObjectStorageConfigFromEnv returns the test object store with a bucket
// unique to t, named from CORE_TEST_OBJECT_STORAGE_BUCKET. The bucket and its
// objects are removed when t ends.
func ObjectStorageConfigFromEnv(t *testing.T) objectstorage.Config {
	t.Helper()
	cfg := objectstorage.Config{
		Endpoint:  testenv.Lookup(t, "CORE_TEST_OBJECT_STORAGE_ENDPOINT"),
		Bucket:    testenv.Lookup(t, "CORE_TEST_OBJECT_STORAGE_BUCKET") + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16],
		AccessKey: testenv.Lookup(t, "CORE_TEST_OBJECT_STORAGE_ACCESS_KEY"),
		SecretKey: testenv.Lookup(t, "CORE_TEST_OBJECT_STORAGE_SECRET_KEY"),
		UseSSL:    strings.TrimSpace(os.Getenv("CORE_TEST_OBJECT_STORAGE_USE_SSL")) == "1",
	}
	t.Cleanup(func() { removeBucket(cfg) })
	return cfg
}

// removeBucket is best effort: the bucket is unique to one test, the store is
// disposable, and the bucket may never have been created.
func removeBucket(cfg objectstorage.Config) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for object := range client.ListObjects(ctx, cfg.Bucket, minio.ListObjectsOptions{Recursive: true}) {
		if object.Err != nil {
			return
		}
		_ = client.RemoveObject(ctx, cfg.Bucket, object.Key, minio.RemoveObjectOptions{})
	}
	_ = client.RemoveBucket(ctx, cfg.Bucket)
}
