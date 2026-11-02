package docker_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/stretchr/testify/require"

	builderdocker "github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/apperrors"
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Api-Version", "1.47")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/_ping" {
			_, _ = w.Write([]byte("OK")) // test fake; the client reports write failures
			return
		}
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
