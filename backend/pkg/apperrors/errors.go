package apperrors

import "errors"

var (
	ErrNotFound           = errors.New("not found")
	ErrAlreadyExists      = errors.New("already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrInvalidToken       = errors.New("invalid token")
	ErrBadRequest         = errors.New("bad request")
	ErrInternal           = errors.New("internal server error")
	ErrResourceExhausted  = errors.New("server capacity reached, cannot allocate resources")
	ErrQuotaExceeded      = errors.New("user memory quota exceeded")
	ErrHostExhausted      = errors.New("host server is out of memory")
)
