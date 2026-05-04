package grpcerrors

import (
	"errors"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Code(err error) codes.Code {
	switch {
	case errors.Is(err, apperrors.ErrBadRequest), errors.Is(err, apperrors.ErrInvalidFileFormat):
		return codes.InvalidArgument
	case errors.Is(err, apperrors.ErrInvalidCredentials), errors.Is(err, apperrors.ErrInvalidToken), errors.Is(err, apperrors.ErrUnauthorized):
		return codes.Unauthenticated
	case errors.Is(err, apperrors.ErrForbidden):
		return codes.PermissionDenied
	case errors.Is(err, apperrors.ErrNotFound):
		return codes.NotFound
	case errors.Is(err, apperrors.ErrAlreadyExists):
		return codes.AlreadyExists
	case errors.Is(err, apperrors.ErrLimitExceeded), errors.Is(err, apperrors.ErrQuotaExceeded), errors.Is(err, apperrors.ErrResourceExhausted), errors.Is(err, apperrors.ErrHostExhausted):
		return codes.ResourceExhausted
	case errors.Is(err, apperrors.ErrConflict), errors.Is(err, apperrors.ErrResourceInUse):
		return codes.FailedPrecondition
	case errors.Is(err, apperrors.ErrTimeout):
		return codes.DeadlineExceeded
	case errors.Is(err, apperrors.ErrUnavailable):
		return codes.Unavailable
	default:
		return codes.Internal
	}
}

func ToGRPC(err error) error {
	return status.Error(Code(err), apperrors.SafeMessage(err))
}

func FromGRPC(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return apperrors.Wrap(apperrors.ErrInternal, apperrors.ErrInternal.Error(), err)
	}

	message := st.Message()
	if message == "" {
		message = statusMessage(st.Code())
	}

	switch st.Code() {
	case codes.InvalidArgument:
		return apperrors.New(apperrors.ErrBadRequest, message)
	case codes.Unauthenticated:
		return apperrors.New(unauthenticatedKind(message), message)
	case codes.PermissionDenied:
		return apperrors.New(apperrors.ErrForbidden, message)
	case codes.NotFound:
		return apperrors.New(apperrors.ErrNotFound, message)
	case codes.AlreadyExists:
		return apperrors.New(apperrors.ErrAlreadyExists, message)
	case codes.ResourceExhausted:
		return apperrors.New(resourceExhaustedKind(message), message)
	case codes.FailedPrecondition, codes.Aborted:
		return apperrors.New(conflictKind(message), message)
	case codes.DeadlineExceeded:
		return apperrors.New(apperrors.ErrTimeout, message)
	case codes.Unavailable:
		return apperrors.New(apperrors.ErrUnavailable, message)
	default:
		return apperrors.Wrap(apperrors.ErrInternal, apperrors.ErrInternal.Error(), err)
	}
}

func unauthenticatedKind(message string) error {
	switch message {
	case apperrors.ErrInvalidCredentials.Error():
		return apperrors.ErrInvalidCredentials
	case apperrors.ErrInvalidToken.Error():
		return apperrors.ErrInvalidToken
	default:
		return apperrors.ErrUnauthorized
	}
}

func resourceExhaustedKind(message string) error {
	switch {
	case message == apperrors.ErrLimitExceeded.Error():
		return apperrors.ErrLimitExceeded
	case message == apperrors.ErrHostExhausted.Error():
		return apperrors.ErrHostExhausted
	case strings.Contains(strings.ToLower(message), "quota"):
		return apperrors.ErrQuotaExceeded
	default:
		return apperrors.ErrResourceExhausted
	}
}

func conflictKind(message string) error {
	if strings.Contains(strings.ToLower(message), "used by") {
		return apperrors.ErrResourceInUse
	}
	return apperrors.ErrConflict
}

func statusMessage(code codes.Code) string {
	switch code {
	case codes.InvalidArgument:
		return apperrors.ErrBadRequest.Error()
	case codes.Unauthenticated:
		return apperrors.ErrUnauthorized.Error()
	case codes.PermissionDenied:
		return apperrors.ErrForbidden.Error()
	case codes.NotFound:
		return apperrors.ErrNotFound.Error()
	case codes.AlreadyExists:
		return apperrors.ErrAlreadyExists.Error()
	case codes.ResourceExhausted:
		return apperrors.ErrResourceExhausted.Error()
	case codes.FailedPrecondition, codes.Aborted:
		return apperrors.ErrConflict.Error()
	case codes.DeadlineExceeded:
		return apperrors.ErrTimeout.Error()
	case codes.Unavailable:
		return apperrors.ErrUnavailable.Error()
	default:
		return apperrors.ErrInternal.Error()
	}
}
