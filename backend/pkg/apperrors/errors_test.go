package apperrors

import (
	"errors"
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
}
