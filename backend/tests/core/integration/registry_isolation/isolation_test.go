package registry_isolation_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	builderdocker "github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/docker"
	buildermodel "github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
	corehttp "github.com/callmerussell04/docker-cloud-manager/internal/core/http"
	coredocker "github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/registry"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/dbtest"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// This test uses only a fresh schema, network, registry and executor containers.
// It never contacts the platform's Registry or changes host firewall rules.
func TestDockerfileCannotAccessAnotherOwnersRegistry(t *testing.T) {
	if os.Getenv("CORE_TEST_REGISTRY_ISOLATION") != "1" {
		t.Skip("set CORE_TEST_REGISTRY_ISOLATION=1 and CORE_TEST_DATABASE_URL for the isolated Docker test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	db := dbtest.OpenCorePostgres(t)
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	networkName := "dcm-registry-test-" + uuid.NewString()
	nw, err := cli.NetworkCreate(ctx, networkName, network.CreateOptions{Driver: "bridge"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.NetworkRemove(context.Background(), nw.ID) })
	info, err := cli.NetworkInspect(ctx, nw.ID, network.InspectOptions{})
	require.NoError(t, err)
	gateway := info.IPAM.Config[0].Gateway
	listener, err := net.Listen("tcp", net.JoinHostPort(gateway, "0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	authority := listener.Addr().String()
	certFile, keyFile := testCertificate(t, gateway)
	signer, err := registry.NewSigner(certFile, keyFile, "dcm-core")
	require.NoError(t, err)
	grants := repository.NewRegistryGrantRepository(db, db)
	users := coremocks.NewUserInfoProvider(t)
	users.EXPECT().GetUser(mock.Anything, mock.Anything).Return(model.UserInfo{Status: "active"}, nil)
	auth := service.NewRegistryAuthorization(grants, users, signer, authority)
	for _, img := range []string{"registry:2.8.3", "gcr.io/kaniko-project/executor@sha256:4e7a52dd1f14872430652bb3b027405b8dfd17c4538751c620ac005741ef9698"} {
		_, err := cli.ImageInspect(ctx, img)
		if err != nil {
			stream, err := cli.ImagePull(ctx, img, image.PullOptions{})
			require.NoError(t, err)
			_, err = io.Copy(io.Discard, stream)
			require.NoError(t, err)
			require.NoError(t, stream.Close())
		}
	}
	created, err := cli.ContainerCreate(ctx, &container.Config{Image: "registry:2.8.3", Env: []string{
		"REGISTRY_STORAGE_DELETE_ENABLED=true", "REGISTRY_HTTP_SECRET=isolated-test-upload-state-secret",
		"REGISTRY_HTTP_HOST=https://" + authority, "REGISTRY_HTTP_TLS_CERTIFICATE=/etc/docker/registry/test.crt", "REGISTRY_HTTP_TLS_KEY=/etc/docker/registry/test.key",
		"REGISTRY_AUTH=token", "REGISTRY_AUTH_TOKEN_REALM=https://" + authority + "/token", "REGISTRY_AUTH_TOKEN_SERVICE=dcm-registry", "REGISTRY_AUTH_TOKEN_ISSUER=dcm-core", "REGISTRY_AUTH_TOKEN_ROOTCERTBUNDLE=/etc/docker/registry/test.crt",
	}, ExposedPorts: nat.PortSet{"5000/tcp": {}}}, &container.HostConfig{PortBindings: nat.PortMap{"5000/tcp": {{HostIP: "127.0.0.1", HostPort: ""}}}}, &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{networkName: {}}}, nil, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = cli.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true, RemoveVolumes: true})
	})
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for name, path := range map[string]string{"test.crt": certFile, "test.key": keyFile} {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data))}))
		_, err = tw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, cli.CopyToContainer(ctx, created.ID, "/etc/docker/registry", &archive, container.CopyToContainerOptions{}))
	require.NoError(t, cli.ContainerStart(ctx, created.ID, container.StartOptions{}))
	inspect, err := cli.ContainerInspect(ctx, created.ID)
	require.NoError(t, err)
	backendURL := "https://127.0.0.1:" + inspect.NetworkSettings.Ports["5000/tcp"][0].HostPort
	httpClient, err := registry.TLSClient(certFile)
	require.NoError(t, err)
	target, err := url.Parse(backendURL)
	require.NoError(t, err)
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)
	edge := &http.Server{Handler: corehttp.RegistryEdgeHandler(target, authority, httpClient.Transport, auth), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: time.Minute, WriteTimeout: time.Minute}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = edge.Serve(tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}))
	}()
	t.Cleanup(func() { _ = edge.Close(); <-done })
	require.Eventually(t, func() bool {
		resp, err := httpClient.Get(backendURL + "/v2/")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == 401
	}, 10*time.Second, 100*time.Millisecond)
	adapter, err := builderdocker.NewAdapter(builderdocker.Config{RegistryCAFile: certFile})
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() })
	metadata, err := registry.NewSecureAdapter(backendURL, httpClient, auth.TrustedToken)
	require.NoError(t, err)
	buildRepo := repository.NewBuildRepository(db, db)
	imageRepo := repository.NewImageRepository(db)
	ownerA, ownerB := uuid.New(), uuid.New()
	seed := func(owner uuid.UUID, tag, dockerfile, source string) (model.Build, string, string) {
		imageID, buildID := uuid.New(), uuid.New()
		require.NoError(t, buildRepo.CreateQueuedBuild(ctx, model.Image{ID: imageID, OwnerID: owner, Tag: tag}, model.Build{ID: buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending, StartedAt: time.Now()}, model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: "test", RoutingKey: "test", Payload: []byte(`{}`)}))
		claim, started, err := auth.ClaimBuild(ctx, buildID, 10*time.Minute)
		require.NoError(t, err)
		require.True(t, started)
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0600))
		if source != "" {
			data, err := os.ReadFile(source)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "payload"), data, 0700))
		} else {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "payload"), []byte("private fixture"), 0600))
		}
		id, logs, err := adapter.RunBuildContainer(ctx, buildermodel.BuildRuntimeSpec{BuildID: buildID.String(), OwnerID: owner.String(), WorkspaceDir: dir, Dockerfile: "Dockerfile", DestinationTag: claim.RegistryCredential.Destination, RegistryUsername: claim.RegistryCredential.Username, RegistryPassword: claim.RegistryCredential.Password, KanikoImage: "gcr.io/kaniko-project/executor@sha256:4e7a52dd1f14872430652bb3b027405b8dfd17c4538751c620ac005741ef9698", MemoryBytes: 256 << 20, MemorySwapMultiplier: 2, CPUPeriod: 100000, CPUQuota: 100000, PidsLimit: 128, NetworkName: networkName})
		require.NoError(t, err)
		t.Cleanup(func() { _ = adapter.CleanBuildContainer(context.Background(), id) })
		output, err := io.ReadAll(logs)
		require.NoError(t, err)
		require.NoError(t, logs.Close())
		require.NoError(t, adapter.WaitForBuild(ctx, id), "executor output: %s", output)
		base := strings.Split(tag, ":")[0]
		repo := strings.ToLower(owner.String() + "_" + base)
		_, digest, err := metadata.GetImageSizeAndDigest(ctx, repo, "latest")
		require.NoError(t, err)
		return claim, repo, digest
	}
	b, bRepo, bDigest := seed(ownerB, "private:latest", "FROM scratch\nCOPY payload /secret\n", "")
	// Preserve an active B upload URL to test continuation with A's stolen credential.
	bToken, err := auth.Exchange(ctx, b.RegistryCredential.Username, b.RegistryCredential.Password, service.RegistryService, []string{"repository:" + bRepo + ":pull,push"})
	require.NoError(t, err)
	request, _ := http.NewRequestWithContext(ctx, "POST", backendURL+"/v2/"+bRepo+"/blobs/uploads/", nil)
	request.Header.Set("Authorization", "Bearer "+bToken)
	response, err := httpClient.Do(request)
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, 202, response.StatusCode)
	uploadURL, err := url.Parse(response.Header.Get("Location"))
	require.NoError(t, err)
	uploadPath := uploadURL.RequestURI()
	require.NoError(t, auth.RevokeBuild(ctx, b.ID, false))
	require.NoError(t, imageRepo.UpdateBuildAndImageSizeTx(ctx, b.ID, b.ImageID, model.BuildStatusSuccess, 1))
	// Daemon gets a separate exact-repository pull identity. Loopback addresses
	// are Docker's test-only implicit insecure registries; no host CA is changed.
	pullImageName := strings.TrimPrefix(backendURL, "https://") + "/" + bRepo + ":latest"
	puller, err := coredocker.NewAdapter(coredocker.WithPullAuthorization(func(ctx context.Context, name string) (string, error) {
		credential, err := auth.PullCredential(ctx, bRepo)
		if err != nil {
			return "", err
		}
		return registry.DockerAuth(credential.Username, credential.Password, strings.TrimPrefix(backendURL, "https://")), nil
	}))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = puller.Close()
		_, _ = cli.ImageRemove(context.Background(), pullImageName, image.RemoveOptions{Force: true})
	})
	require.NoError(t, puller.PullImage(ctx, pullImageName))

	aBase, aBaseRepo, _ := seed(ownerA, "base:latest", "FROM scratch\nCOPY payload /base\n", "")
	require.NoError(t, auth.RevokeBuild(ctx, aBase.ID, false))
	require.NoError(t, imageRepo.UpdateBuildAndImageSizeTx(ctx, aBase.ID, aBase.ImageID, model.BuildStatusSuccess, 1))
	_, file, _, _ := runtime.Caller(0)
	probePath := filepath.Join(t.TempDir(), "probe")
	compile := exec.CommandContext(ctx, "go", "build", "-o", probePath, filepath.Join(filepath.Dir(file), "../../../testdata/registry-probe/main.go"))
	compile.Env = append(os.Environ(), "CGO_ENABLED=0", "GOCACHE=/tmp/go-build-cache")
	compileOutput, err := compile.CombinedOutput()
	require.NoError(t, err, "compile probe: %s", compileOutput)
	aRepo := strings.ToLower(ownerA.String() + "_destination")
	args, _ := json.Marshal([]string{"/probe", authority, aRepo, bRepo, bDigest, uploadPath})
	a, _, _ := seed(ownerA, "destination:latest", "FROM "+authority+"/"+aBaseRepo+":latest\nCOPY payload /probe\nRUN "+string(args)+"\n", probePath)
	_, after, err := metadata.GetImageSizeAndDigest(ctx, bRepo, "latest")
	require.NoError(t, err)
	require.Equal(t, bDigest, after, "B must be unchanged")
	require.NoError(t, auth.RevokeBuild(ctx, a.ID, true))
	_, err = auth.Exchange(ctx, a.RegistryCredential.Username, a.RegistryCredential.Password, service.RegistryService, nil)
	require.ErrorIs(t, err, apperrors.ErrForbidden)
	// A previously issued bearer remains scoped; a token outside the full skew
	// allowance must be rejected by the actual pinned Registry verifier.
	expired, err := signer.SignRegistryToken(ctx, model.RegistryTokenGrant{Subject: "test", Service: service.RegistryService, IssuedAt: time.Now().Add(-4 * time.Minute), ExpiresAt: time.Now().Add(-2 * time.Minute), Access: []model.RegistryAccess{{Repository: bRepo, Actions: []string{"pull"}}}})
	require.NoError(t, err)
	request, _ = http.NewRequestWithContext(ctx, "GET", backendURL+"/v2/"+bRepo+"/manifests/latest", nil)
	request.Header.Set("Authorization", "Bearer "+expired)
	response, err = httpClient.Do(request)
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, 401, response.StatusCode)
	// Trusted cleanup succeeds independently of the revoked job credential.
	require.NoError(t, metadata.DeleteManifest(ctx, bRepo, bDigest))
	_, _, err = metadata.GetImageSizeAndDigest(ctx, bRepo, "latest")
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func testCertificate(t *testing.T, gateway string) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "isolated-registry-test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP(gateway), net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	require.NoError(t, err)
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "registry.crt"), filepath.Join(dir, "registry.key")
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0600))
	return certFile, keyFile
}
