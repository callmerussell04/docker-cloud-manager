package apperrors_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
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

// Every limit a request can exceed has its own name, a fixed message that
// names it, and the error kind and client code the rejection had before it
// was named.
func TestExceededNamesTheLimit(t *testing.T) {
	for _, tc := range []struct {
		limit   apperrors.Limit
		kind    error
		code    string
		message string
	}{
		{apperrors.LimitUserMemory, apperrors.ErrQuotaExceeded, "quota_exceeded", "user memory quota exceeded"},
		{apperrors.LimitUserCPU, apperrors.ErrQuotaExceeded, "quota_exceeded", "user CPU quota exceeded"},
		{apperrors.LimitUserDisk, apperrors.ErrQuotaExceeded, "quota_exceeded", "user disk quota exceeded"},
		{apperrors.LimitHostMemory, apperrors.ErrHostExhausted, "host_exhausted", "host memory is fully reserved"},
		{apperrors.LimitHostCPU, apperrors.ErrHostExhausted, "host_exhausted", "host CPU is fully reserved"},
		{apperrors.LimitDiskFloor, apperrors.ErrHostExhausted, "host_exhausted", "host is low on free disk"},
		{apperrors.LimitContainersPerUser, apperrors.ErrLimitExceeded, "limit_exceeded", "maximum number of containers reached"},
		{apperrors.LimitVolumesPerUser, apperrors.ErrLimitExceeded, "limit_exceeded", "maximum number of volumes reached"},
		{apperrors.LimitQueuedContainerCreates, apperrors.ErrLimitExceeded, "limit_exceeded", "maximum number of queued container creates reached"},
		{apperrors.LimitQueuedBuilds, apperrors.ErrLimitExceeded, "limit_exceeded", "maximum number of queued builds reached"},
		{apperrors.LimitQueuedComposeDeployments, apperrors.ErrLimitExceeded, "limit_exceeded", "maximum number of queued compose deployments reached"},
		{apperrors.LimitStagedSourceBytes, apperrors.ErrQuotaExceeded, "quota_exceeded", "staged source quota exceeded"},
	} {
		t.Run(string(tc.limit), func(t *testing.T) {
			err := apperrors.Exceeded(tc.limit)

			require.ErrorIs(t, err, tc.kind)
			require.Equal(t, tc.code, apperrors.Code(err))
			require.Equal(t, tc.message, apperrors.SafeMessage(err))
			require.Equal(t, tc.limit, apperrors.LimitOf(err))
		})
	}
}

func TestLimitNamesAreTheWireVocabulary(t *testing.T) {
	require.Equal(t, []apperrors.Limit{
		"user_memory", "user_cpu", "user_disk", "host_memory", "host_cpu", "disk_floor",
		"containers_per_user", "volumes_per_user", "queued_container_creates",
		"queued_builds", "queued_compose_deployments", "staged_source_bytes",
	}, apperrors.Limits())
}

func TestLimitOfReadsThroughWrapping(t *testing.T) {
	wrapped := fmt.Errorf("failed to create service web: %w", apperrors.Exceeded(apperrors.LimitQueuedContainerCreates))

	require.Equal(t, apperrors.LimitQueuedContainerCreates, apperrors.LimitOf(wrapped))
	require.ErrorIs(t, wrapped, apperrors.ErrLimitExceeded)
}

func TestLimitOfIsEmptyWithoutANamedLimit(t *testing.T) {
	require.Empty(t, apperrors.LimitOf(nil))
	require.Empty(t, apperrors.LimitOf(apperrors.ErrQuotaExceeded))
	require.Empty(t, apperrors.LimitOf(apperrors.New(apperrors.ErrLimitExceeded, "maximum telemetry log streams reached")))
	require.Empty(t, apperrors.LimitOf(errors.New("pq: connection reset")))
}

// A limit name that is not in the vocabulary names nothing: the error is the
// generic exhausted kind and carries no limit.
func TestExceededWithAnUnknownLimit(t *testing.T) {
	err := apperrors.Exceeded("made_up")

	require.ErrorIs(t, err, apperrors.ErrResourceExhausted)
	require.Empty(t, apperrors.LimitOf(err))
	require.False(t, apperrors.Limit("made_up").Known())
}

// The kinds' own texts name no particular limit, because each stands for
// several.
func TestQuotaAndHostKindsHaveNeutralTexts(t *testing.T) {
	require.Equal(t, "user quota exceeded", apperrors.ErrQuotaExceeded.Error())
	require.Equal(t, "host capacity exhausted", apperrors.ErrHostExhausted.Error())
}
