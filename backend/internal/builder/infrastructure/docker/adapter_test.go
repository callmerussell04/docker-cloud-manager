package docker_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/stretchr/testify/require"

	builderdocker "github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
)

const (
	isolatedBuildNetwork = `{"Name":"build_net","Internal":true,"EnableIPv6":false,"Options":{"com.docker.network.bridge.name":"br-dcm-build"}}`
	policyContainers     = `[{"Id":"policy"}]`
	healthyPolicy        = `{"Id":"policy","State":{"Running":true,"Health":{"Status":"healthy"}}}`
)

type daemonResponse struct {
	status int
	body   string
}

// A RUN step inherits nothing from the Kaniko process environment, so only
// build args give it the route out of the internal build network.
func TestAdapterRunBuildContainerPassesProxyToRunSteps(t *testing.T) {
	const egress = "http://build-egress:3128"
	proxyArgs := []string{
		"--build-arg=HTTPS_PROXY=" + egress,
		"--build-arg=HTTP_PROXY=" + egress,
		"--build-arg=NO_PROXY=registry:5000",
		"--build-arg=http_proxy=" + egress,
		"--build-arg=https_proxy=" + egress,
		"--build-arg=no_proxy=registry:5000",
	}
	for _, tt := range []struct {
		name      string
		buildArgs map[string]string
		want      []string
	}{
		{name: "without user args", want: proxyArgs},
		{
			name:      "user args are kept",
			buildArgs: map[string]string{"NODE_ENV": "production"},
			want: []string{
				"--build-arg=HTTPS_PROXY=" + egress,
				"--build-arg=HTTP_PROXY=" + egress,
				"--build-arg=NODE_ENV=production",
				"--build-arg=NO_PROXY=registry:5000",
				"--build-arg=http_proxy=" + egress,
				"--build-arg=https_proxy=" + egress,
				"--build-arg=no_proxy=registry:5000",
			},
		},
		{
			name:      "user proxy args cannot replace the egress proxy",
			buildArgs: map[string]string{"HTTP_PROXY": "http://other:1", "no_proxy": "*"},
			want:      proxyArgs,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			created := make(chan []byte, 1)
			serveRecordingFakeDaemon(t, map[string]daemonResponse{
				"/networks/build_net": {http.StatusOK, isolatedBuildNetwork},
				"/images/kaniko/json": {http.StatusOK, `{"Id":"kaniko"}`},
				// Failing here ends the run once the container request is recorded.
				"/containers/create": {http.StatusInternalServerError, `{"message":"stop after create"}`},
			}, func(r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/containers/create") {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Errorf("read create request: %v", err)
					}
					created <- body
				}
			})
			adapter, err := builderdocker.NewAdapter(builderdocker.Config{RegistryCAFile: writeTestCA(t)})
			require.NoError(t, err)
			t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

			_, _, err = adapter.RunBuildContainer(context.Background(), model.BuildRuntimeSpec{
				RegistryUsername: "builder",
				RegistryPassword: "password",
				DestinationTag:   "registry:5000/owner_image:latest",
				KanikoImage:      "kaniko",
				Dockerfile:       "Dockerfile",
				NetworkName:      "build_net",
				BuildArgs:        tt.buildArgs,
			})
			require.Error(t, err)

			var request struct{ Cmd []string }
			require.NoError(t, json.Unmarshal(<-created, &request))
			var got []string
			for _, arg := range request.Cmd {
				if strings.HasPrefix(arg, "--build-arg=") {
					got = append(got, arg)
				}
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestAdapterValidateBuildNetwork(t *testing.T) {
	notFound := daemonResponse{http.StatusNotFound, `{"message":"network build_net not found"}`}
	daemonFailure := daemonResponse{http.StatusInternalServerError, `{"message":"daemon failure"}`}
	ok := func(body string) daemonResponse { return daemonResponse{http.StatusOK, body} }
	for _, tt := range []struct {
		name      string
		network   daemonResponse
		list      daemonResponse
		policy    daemonResponse
		wantErr   string
		wantCause func(error) bool
	}{
		{
			name:    "isolated network with healthy policy",
			network: ok(isolatedBuildNetwork), list: ok(policyContainers), policy: ok(healthyPolicy),
		},
		{
			name:      "missing network keeps inspection cause",
			network:   notFound,
			wantErr:   "build network inspection failed",
			wantCause: cerrdefs.IsNotFound,
		},
		{
			name:    "unisolated network",
			network: ok(`{"Name":"build_net","Internal":false,"Options":{"com.docker.network.bridge.name":"br-dcm-build"}}`),
			wantErr: "build network is not isolated",
		},
		{
			name:      "policy listing failure keeps cause",
			network:   ok(isolatedBuildNetwork),
			list:      daemonFailure,
			wantErr:   "build network policy is unavailable",
			wantCause: cerrdefs.IsInternal,
		},
		{
			name:      "policy inspection failure keeps cause",
			network:   ok(isolatedBuildNetwork),
			list:      ok(policyContainers),
			policy:    daemonFailure,
			wantErr:   "build network policy is unavailable",
			wantCause: cerrdefs.IsInternal,
		},
		{
			name:    "unhealthy policy",
			network: ok(isolatedBuildNetwork), list: ok(policyContainers),
			policy:  ok(`{"Id":"policy","State":{"Running":true,"Health":{"Status":"starting"}}}`),
			wantErr: "build network policy is unavailable",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			serveFakeDaemon(t, map[string]daemonResponse{
				"/networks/build_net":     tt.network,
				"/containers/json":        tt.list,
				"/containers/policy/json": tt.policy,
			})
			adapter, err := builderdocker.NewAdapter(builderdocker.Config{RequireNetworkPolicy: true})
			require.NoError(t, err)
			t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

			err = adapter.ValidateBuildNetwork(context.Background(), "build_net")

			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, apperrors.ErrUnavailable)
			require.EqualError(t, err, tt.wantErr)
			if tt.wantCause != nil {
				require.True(t, tt.wantCause(err), "cause missing from %v", err)
			}
		})
	}
}

// serveFakeDaemon points the Docker client environment at a daemon that
// answers versioned API paths from routes.
func serveFakeDaemon(t *testing.T, routes map[string]daemonResponse) {
	t.Helper()
	serveRecordingFakeDaemon(t, routes, func(*http.Request) {})
}

// serveRecordingFakeDaemon also passes each API request to record before
// answering it.
func serveRecordingFakeDaemon(t *testing.T, routes map[string]daemonResponse, record func(*http.Request)) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Api-Version", "1.47")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/_ping" {
			_, _ = w.Write([]byte("OK")) // test fake; the client reports write failures
			return
		}
		record(r)
		_, path, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		response, found := routes["/"+path]
		if !found || response.status == 0 {
			t.Errorf("unexpected Docker API call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		w.WriteHeader(response.status)
		_, _ = w.Write([]byte(response.body)) // test fake; the client reports write failures
	}))
	t.Cleanup(server.Close)
	t.Setenv("DOCKER_HOST", "tcp://"+server.Listener.Addr().String())
	t.Setenv("DOCKER_API_VERSION", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
}

func writeTestCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test registry CA"},
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return path
}
