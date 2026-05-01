package httpresponse

import (
	"errors"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/gin-gonic/gin"
)

type ErrorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

func Respond(c *gin.Context, statusCode int, err error) {
	_ = c.Error(err)
	c.AbortWithStatusJSON(statusCode, ErrorResponse{
		Error:     apperrors.SafeMessage(err),
		RequestID: logging.RequestIDFromContext(c.Request.Context()),
	})
}

func Status(err error) int {
	switch {
	case errors.Is(err, apperrors.ErrBadRequest), errors.Is(err, apperrors.ErrInvalidFileFormat):
		return http.StatusBadRequest
	case errors.Is(err, apperrors.ErrUnauthorized), errors.Is(err, apperrors.ErrInvalidCredentials), errors.Is(err, apperrors.ErrInvalidToken):
		return http.StatusUnauthorized
	case errors.Is(err, apperrors.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, apperrors.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, apperrors.ErrAlreadyExists), errors.Is(err, apperrors.ErrLimitExceeded), errors.Is(err, apperrors.ErrQuotaExceeded), errors.Is(err, apperrors.ErrResourceExhausted), errors.Is(err, apperrors.ErrConflict), errors.Is(err, apperrors.ErrResourceInUse):
		return http.StatusConflict
	case errors.Is(err, apperrors.ErrHostExhausted):
		return http.StatusServiceUnavailable
	case errors.Is(err, apperrors.ErrUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, apperrors.ErrTimeout):
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}
