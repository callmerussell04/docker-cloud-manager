package httpresponse_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/httpresponse"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStatusMapsConflict(t *testing.T) {
	err := apperrors.New(apperrors.ErrResourceInUse, "image is currently used by a container")

	require.Equal(t, http.StatusConflict, httpresponse.Status(err))
}

func TestErrorResponseIncludesErrorCode(t *testing.T) {
	response := httpresponse.ErrorResponse{
		Error:     apperrors.SafeMessage(apperrors.ErrForbidden),
		ErrorCode: apperrors.Code(apperrors.ErrForbidden),
	}

	require.Equal(t, "forbidden", response.ErrorCode)
}

func respond(t *testing.T, err error) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/containers", nil)

	httpresponse.Respond(c, httpresponse.Status(err), err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return recorder.Code, body
}

// A rejection that names its limit says so in the body, beside the client
// code and status it always had.
func TestRespondNamesTheExceededLimit(t *testing.T) {
	for _, tc := range []struct {
		limit   apperrors.Limit
		status  int
		code    string
		message string
	}{
		{apperrors.LimitUserCPU, http.StatusConflict, "quota_exceeded", "user CPU quota exceeded"},
		{apperrors.LimitStagedSourceBytes, http.StatusConflict, "quota_exceeded", "staged source quota exceeded"},
		{apperrors.LimitHostCPU, http.StatusServiceUnavailable, "host_exhausted", "host CPU is fully reserved"},
		{apperrors.LimitDiskFloor, http.StatusServiceUnavailable, "host_exhausted", "host is low on free disk"},
		{apperrors.LimitQueuedContainerCreates, http.StatusConflict, "limit_exceeded", "maximum number of queued container creates reached"},
	} {
		t.Run(string(tc.limit), func(t *testing.T) {
			status, body := respond(t, apperrors.Exceeded(tc.limit))

			require.Equal(t, tc.status, status)
			require.Equal(t, map[string]any{"error": tc.message, "error_code": tc.code, "limit": string(tc.limit)}, body)
		})
	}
}

func TestRespondOmitsLimitWhenNoneIsNamed(t *testing.T) {
	status, body := respond(t, apperrors.New(apperrors.ErrConflict, "resource operation is already in progress"))

	require.Equal(t, http.StatusConflict, status)
	require.Equal(t, map[string]any{"error": "resource operation is already in progress", "error_code": "conflict"}, body)
}
