package grpcerrors

import (
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

func TestRoundTripPreservesSafeConflict(t *testing.T) {
	source := apperrors.New(apperrors.ErrResourceInUse, "volume is currently used by a container")
	roundTripped := FromGRPC(ToGRPC(source))

	if !errors.Is(roundTripped, apperrors.ErrResourceInUse) {
		t.Fatalf("roundTripped error does not match ErrResourceInUse")
	}
	if got := apperrors.SafeMessage(roundTripped); got != "volume is currently used by a container" {
		t.Fatalf("SafeMessage() = %q", got)
	}
}

func TestRoundTripPreservesAuthReason(t *testing.T) {
	roundTripped := FromGRPC(ToGRPC(apperrors.ErrInvalidCredentials))

	if !errors.Is(roundTripped, apperrors.ErrInvalidCredentials) {
		t.Fatalf("roundTripped error does not match ErrInvalidCredentials")
	}
	if got := apperrors.SafeMessage(roundTripped); got != apperrors.ErrInvalidCredentials.Error() {
		t.Fatalf("SafeMessage() = %q", got)
	}
}
