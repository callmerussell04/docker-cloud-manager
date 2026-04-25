package apperrors

import (
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GRPCCode(err error) codes.Code {
	switch {
	case errors.Is(err, ErrBadRequest), errors.Is(err, ErrInvalidFileFormat):
		return codes.InvalidArgument
	case errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrInvalidToken), errors.Is(err, ErrUnauthorized):
		return codes.Unauthenticated
	case errors.Is(err, ErrForbidden):
		return codes.PermissionDenied
	case errors.Is(err, ErrNotFound):
		return codes.NotFound
	case errors.Is(err, ErrAlreadyExists):
		return codes.AlreadyExists
	case errors.Is(err, ErrLimitExceeded), errors.Is(err, ErrQuotaExceeded), errors.Is(err, ErrResourceExhausted), errors.Is(err, ErrHostExhausted):
		return codes.ResourceExhausted
	case errors.Is(err, ErrConflict), errors.Is(err, ErrResourceInUse):
		return codes.FailedPrecondition
	default:
		return codes.Internal
	}
}

func ToGRPC(err error) error {
	return status.Error(GRPCCode(err), SafeMessage(err))
}

func FromGRPC(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return Wrap(ErrInternal, ErrInternal.Error(), err)
	}

	message := st.Message()
	if message == "" {
		message = statusMessage(st.Code())
	}

	switch st.Code() {
	case codes.InvalidArgument:
		return New(ErrBadRequest, message)
	case codes.Unauthenticated:
		return New(unauthenticatedKind(message), message)
	case codes.PermissionDenied:
		return New(ErrForbidden, message)
	case codes.NotFound:
		return New(ErrNotFound, message)
	case codes.AlreadyExists:
		return New(ErrAlreadyExists, message)
	case codes.ResourceExhausted:
		return New(resourceExhaustedKind(message), message)
	case codes.FailedPrecondition, codes.Aborted:
		return New(conflictKind(message), message)
	default:
		return Wrap(ErrInternal, ErrInternal.Error(), err)
	}
}

func unauthenticatedKind(message string) error {
	switch message {
	case ErrInvalidCredentials.Error():
		return ErrInvalidCredentials
	case ErrInvalidToken.Error():
		return ErrInvalidToken
	default:
		return ErrUnauthorized
	}
}

func resourceExhaustedKind(message string) error {
	switch {
	case message == ErrLimitExceeded.Error():
		return ErrLimitExceeded
	case message == ErrHostExhausted.Error():
		return ErrHostExhausted
	case strings.Contains(strings.ToLower(message), "quota"):
		return ErrQuotaExceeded
	default:
		return ErrResourceExhausted
	}
}

func conflictKind(message string) error {
	if strings.Contains(strings.ToLower(message), "used by") {
		return ErrResourceInUse
	}
	return ErrConflict
}

func statusMessage(code codes.Code) string {
	switch code {
	case codes.InvalidArgument:
		return ErrBadRequest.Error()
	case codes.Unauthenticated:
		return ErrUnauthorized.Error()
	case codes.PermissionDenied:
		return ErrForbidden.Error()
	case codes.NotFound:
		return ErrNotFound.Error()
	case codes.AlreadyExists:
		return ErrAlreadyExists.Error()
	case codes.ResourceExhausted:
		return ErrResourceExhausted.Error()
	case codes.FailedPrecondition, codes.Aborted:
		return ErrConflict.Error()
	default:
		return ErrInternal.Error()
	}
}
