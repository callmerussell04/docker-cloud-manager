package grpcerrors_test

import (
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
	"github.com/stretchr/testify/require"
)

func TestRoundTripPreservesSafeConflict(t *testing.T) {
	source := apperrors.New(apperrors.ErrResourceInUse, "volume is currently used by a container")
	roundTripped := grpcerrors.FromGRPC(grpcerrors.ToGRPC(source))

	require.True(t, errors.Is(roundTripped, apperrors.ErrResourceInUse))
	require.Equal(t, "volume is currently used by a container", apperrors.SafeMessage(roundTripped))
}

func TestRoundTripPreservesAuthReason(t *testing.T) {
	roundTripped := grpcerrors.FromGRPC(grpcerrors.ToGRPC(apperrors.ErrInvalidCredentials))

	require.True(t, errors.Is(roundTripped, apperrors.ErrInvalidCredentials))
	require.Equal(t, apperrors.ErrInvalidCredentials.Error(), apperrors.SafeMessage(roundTripped))
}
