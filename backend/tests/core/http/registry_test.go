package http_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
			corehttp.RegistryTokenHandler(logic).ServeHTTP(response, req)
			require.Equal(t, test.status, response.Code)
			require.NotContains(t, response.Body.String(), "private")
			require.NotContains(t, response.Body.String(), "job-secret")
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		})
	}
}
