package apperrors

import (
	"errors"
)

var (
	ErrNotFound           = errors.New("not found")
	ErrAlreadyExists      = errors.New("already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
	ErrInvalidToken       = errors.New("invalid token")
	ErrBadRequest         = errors.New("bad request")
	ErrInternal           = errors.New("internal server error")
	ErrResourceExhausted  = errors.New("server capacity reached, cannot allocate resources")
	ErrQuotaExceeded      = errors.New("user memory quota exceeded")
	ErrLimitExceeded      = errors.New("maximum number of resources reached")
	ErrHostExhausted      = errors.New("host server is out of memory")
	ErrInvalidFileFormat  = errors.New("invalid file format, allowed: .zip, .tar, .tar.gz")
	ErrConflict           = errors.New("conflict")
	ErrResourceInUse      = errors.New("resource is currently in use")
	ErrTimeout            = errors.New("request timed out")
	ErrUnavailable        = errors.New("service unavailable")
)

type Error struct {
	kind    error
	message string
	cause   error
}

func New(kind error, message string) error {
	return &Error{
		kind:    kind,
		message: message,
	}
}

func Wrap(kind error, message string, cause error) error {
	return &Error{
		kind:    kind,
		message: message,
		cause:   cause,
	}
}

func (e *Error) Error() string {
	if e.message != "" {
		return e.message
	}
	if e.kind != nil {
		return e.kind.Error()
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return ErrInternal.Error()
}

func (e *Error) Unwrap() error {
	if e.cause != nil {
		return e.cause
	}
	return e.kind
}

func (e *Error) Is(target error) bool {
	if target == nil {
		return false
	}
	return errors.Is(e.kind, target) || errors.Is(e.cause, target)
}

func SafeMessage(err error) string {
	if err == nil {
		return ""
	}

	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Error()
	}

	switch {
	case errors.Is(err, ErrInternal):
		return ErrInternal.Error()
	case errors.Is(err, ErrBadRequest):
		return err.Error()
	case errors.Is(err, ErrInvalidFileFormat):
		return ErrInvalidFileFormat.Error()
	case errors.Is(err, ErrNotFound):
		return ErrNotFound.Error()
	case errors.Is(err, ErrAlreadyExists):
		return ErrAlreadyExists.Error()
	case errors.Is(err, ErrInvalidCredentials):
		return ErrInvalidCredentials.Error()
	case errors.Is(err, ErrInvalidToken):
		return ErrInvalidToken.Error()
	case errors.Is(err, ErrUnauthorized):
		return ErrUnauthorized.Error()
	case errors.Is(err, ErrForbidden):
		return ErrForbidden.Error()
	case errors.Is(err, ErrLimitExceeded):
		return ErrLimitExceeded.Error()
	case errors.Is(err, ErrQuotaExceeded):
		return ErrQuotaExceeded.Error()
	case errors.Is(err, ErrHostExhausted):
		return ErrHostExhausted.Error()
	case errors.Is(err, ErrResourceExhausted):
		return ErrResourceExhausted.Error()
	case errors.Is(err, ErrResourceInUse):
		return ErrResourceInUse.Error()
	case errors.Is(err, ErrConflict):
		return ErrConflict.Error()
	case errors.Is(err, ErrTimeout):
		return ErrTimeout.Error()
	case errors.Is(err, ErrUnavailable):
		return ErrUnavailable.Error()
	default:
		return ErrInternal.Error()
	}
}
