package apperrors_test

import (
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/stretchr/testify/require"
)

func TestSafeMessageHidesRawErrors(t *testing.T) {
	err := errors.New("pq: relation users does not exist")

	require.Equal(t, apperrors.ErrInternal.Error(), apperrors.SafeMessage(err))
}

func TestSafeMessageKeepsClientErrors(t *testing.T) {
	err := apperrors.New(apperrors.ErrResourceInUse, "image is currently used by a container")

	require.Equal(t, "image is currently used by a container", apperrors.SafeMessage(err))
	require.True(t, errors.Is(err, apperrors.ErrResourceInUse))
}

func TestCodeReturnsStableClientCode(t *testing.T) {
	err := apperrors.New(apperrors.ErrResourceInUse, "image is currently used by a container")

	require.Equal(t, "resource_in_use", apperrors.Code(err))
}
