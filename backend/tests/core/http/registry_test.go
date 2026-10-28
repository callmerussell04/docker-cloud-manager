package http_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corehttp "github.com/callmerussell04/docker-cloud-manager/internal/core/http"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	corehttpmocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/http"
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
