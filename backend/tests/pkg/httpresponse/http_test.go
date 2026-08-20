package httpresponse_test

import (
	"net/http"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
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
