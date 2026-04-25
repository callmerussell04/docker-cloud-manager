package apperrors

import (
	"errors"
	"net/http"
	"testing"
)

func TestSafeMessageHidesRawErrors(t *testing.T) {
	err := errors.New("pq: relation users does not exist")

	if got := SafeMessage(err); got != ErrInternal.Error() {
		t.Fatalf("SafeMessage() = %q, want %q", got, ErrInternal.Error())
	}
}

func TestSafeMessageKeepsClientErrors(t *testing.T) {
	err := New(ErrResourceInUse, "image is currently used by a container")

	if got := SafeMessage(err); got != "image is currently used by a container" {
		t.Fatalf("SafeMessage() = %q", got)
	}
	if !errors.Is(err, ErrResourceInUse) {
		t.Fatalf("error does not match ErrResourceInUse")
	}
	if got := HTTPStatus(err); got != http.StatusConflict {
		t.Fatalf("HTTPStatus() = %d, want %d", got, http.StatusConflict)
	}
}

func TestGRPCRoundTripPreservesSafeConflict(t *testing.T) {
	source := New(ErrResourceInUse, "volume is currently used by a container")
	roundTripped := FromGRPC(ToGRPC(source))

	if !errors.Is(roundTripped, ErrResourceInUse) {
		t.Fatalf("roundTripped error does not match ErrResourceInUse")
	}
	if got := SafeMessage(roundTripped); got != "volume is currently used by a container" {
		t.Fatalf("SafeMessage() = %q", got)
	}
	if got := HTTPStatus(roundTripped); got != http.StatusConflict {
		t.Fatalf("HTTPStatus() = %d, want %d", got, http.StatusConflict)
	}
}

func TestGRPCRoundTripPreservesAuthReason(t *testing.T) {
	roundTripped := FromGRPC(ToGRPC(ErrInvalidCredentials))

	if !errors.Is(roundTripped, ErrInvalidCredentials) {
		t.Fatalf("roundTripped error does not match ErrInvalidCredentials")
	}
	if got := SafeMessage(roundTripped); got != ErrInvalidCredentials.Error() {
		t.Fatalf("SafeMessage() = %q", got)
	}
}
