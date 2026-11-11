package http_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	corehttp "github.com/callmerussell04/docker-cloud-manager/backend/internal/core/http"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	corehttpmocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/http"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRegistryRealmClassifiesDenialAndOutageWithoutLeakingErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		cause  error
		status int
	}{
		{"revoked", apperrors.ErrForbidden, http.StatusUnauthorized},
		{"outage", fmt.Errorf("upstream credential=private: %w", apperrors.ErrUnavailable), http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			logic := corehttpmocks.NewMockRegistryTokenLogic(t)
			logic.EXPECT().Exchange(mock.Anything, "job-id", "job-secret", "dcm-registry", []string{"repository:owner_image:pull,push"}).Return("", test.cause)
			req := httptest.NewRequest(http.MethodGet, "/token?service=dcm-registry&scope=repository:owner_image:pull,push", nil)
			req.SetBasicAuth("job-id", "job-secret")
			response := httptest.NewRecorder()
			corehttp.RegistryTokenHandler(logic, slog.New(slog.DiscardHandler)).ServeHTTP(response, req)
			require.Equal(t, test.status, response.Code)
			require.NotContains(t, response.Body.String(), "private")
			require.NotContains(t, response.Body.String(), "job-secret")
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		})
	}
}

// failingResponseWriter records the status but fails every body write, as a
// connection closed by the client does.
type failingResponseWriter struct {
	header http.Header
	status int
}

func (w *failingResponseWriter) Header() http.Header       { return w.header }
func (w *failingResponseWriter) WriteHeader(status int)    { w.status = status }
func (w *failingResponseWriter) Write([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestRegistryRealmReportsFailedTokenResponseWriteWithoutToken(t *testing.T) {
	logic := corehttpmocks.NewMockRegistryTokenLogic(t)
	logic.EXPECT().Exchange(mock.Anything, "job-id", "job-secret", "dcm-registry", []string{"repository:owner_image:pull"}).Return("signed-registry-token", nil)
	req := httptest.NewRequest(http.MethodGet, "/token?service=dcm-registry&scope=repository:owner_image:pull", nil)
	req.SetBasicAuth("job-id", "job-secret")
	var logs bytes.Buffer
	response := &failingResponseWriter{header: http.Header{}}

	corehttp.RegistryTokenHandler(logic, slog.New(slog.NewTextHandler(&logs, nil))).ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.status)
	require.Equal(t, 1, strings.Count(logs.String(), "failed to write registry token response"), "the write failure is reported once")
	require.Contains(t, logs.String(), "connection reset")
	require.NotContains(t, logs.String(), "signed-registry-token")
	require.NotContains(t, logs.String(), "job-secret")
}

// The realm runs on a raw net/http listener with no request-logging
// middleware, so it logs an outage itself; a denial is an expected answer.
func TestRegistryRealmLogsOutageButNotDenial(t *testing.T) {
	for _, test := range []struct {
		name   string
		cause  error
		logged bool
	}{
		{"denied", apperrors.ErrForbidden, false},
		{"outage", fmt.Errorf("sign registry token: %w", apperrors.ErrUnavailable), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			logic := corehttpmocks.NewMockRegistryTokenLogic(t)
			logic.EXPECT().Exchange(mock.Anything, "job-id", "job-secret", "dcm-registry", []string{}).Return("", test.cause)
			req := httptest.NewRequest(http.MethodGet, "/token?service=dcm-registry", nil)
			req.SetBasicAuth("job-id", "job-secret")
			var logs bytes.Buffer

			corehttp.RegistryTokenHandler(logic, slog.New(slog.NewTextHandler(&logs, nil))).ServeHTTP(httptest.NewRecorder(), req)

			if !test.logged {
				require.Empty(t, logs.String())
				return
			}
			require.Equal(t, 1, strings.Count(logs.String(), "registry token exchange failed"))
			require.Contains(t, logs.String(), "sign registry token")
			require.NotContains(t, logs.String(), "job-secret")
		})
	}
}

func TestRegistryEdgeLogsUnavailableBackend(t *testing.T) {
	target, err := url.Parse("https://registry-backend.test")
	require.NoError(t, err)
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("connection refused") })
	var logs bytes.Buffer
	handler := corehttp.RegistryEdgeHandler(target, "registry:5000", transport, corehttpmocks.NewMockRegistryTokenLogic(t), slog.New(slog.NewTextHandler(&logs, nil)))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/owner_image/manifests/latest", nil))

	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Equal(t, 1, strings.Count(logs.String(), "registry backend request failed"))
	require.Contains(t, logs.String(), "connection refused")
}

// The Registry cleanup fence (service.RegistryCleanupDelay) counts on the edge
// ending every admitted request within a minute: a build whose credential was
// revoked cannot keep writing through a request that was already let in. The
// same limit applies to reads, so one blob transfer cannot outlast a minute.
func TestRegistryEdgeEndsARequestTheBackendHasNotFinishedInAMinute(t *testing.T) {
	for _, tt := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "blob download", method: http.MethodGet, path: "/v2/owner_image/blobs/sha256:0000"},
		{name: "blob upload", method: http.MethodPatch, path: "/v2/owner_image/blobs/uploads/session"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				target, err := url.Parse("https://registry-backend.test")
				require.NoError(t, err)
				// The backend answers at once and then never finishes the body,
				// as a transfer slower than the limit would.
				transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: stalledBody{ctx: req.Context()}, Request: req}, nil
				})
				handler := corehttp.RegistryEdgeHandler(target, "registry:5000", transport, corehttpmocks.NewMockRegistryTokenLogic(t), slog.New(slog.DiscardHandler))
				started := time.Now()

				handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tt.method, tt.path, nil))

				require.Equal(t, time.Minute, time.Since(started))
			})
		})
	}
}

// stalledBody blocks until its request is canceled, like a transport body
// read interrupted by the request context.
type stalledBody struct{ ctx context.Context }

func (b stalledBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (stalledBody) Close() error { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
