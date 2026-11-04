package rabbitmq_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	platformrabbitmq "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
	"github.com/stretchr/testify/require"
)

// The literals pin what the broker and in-flight messages see.
func TestBuildQueueDescription(t *testing.T) {
	require.Equal(t, platformrabbitmq.Queue{
		Label: "build", Exchange: "dcm.builds", Name: "dcm.builds.image", RoutingKey: "build.image",
		DLX: "dcm.builds.dlx", DLQ: "dcm.builds.image.dlq", DLQKey: "build.image.dead",
	}, buildqueue.Queue)
}

func TestBuildConsumerConfig(t *testing.T) {
	cfg := rabbitmq.BuildConsumerConfig("amqp://test", "", platformrabbitmq.Queue{}, nil)
	msg := buildqueue.ImageBuildMessage{BuildID: "build", RequestID: "request"}

	require.Equal(t, "dcm.builds.image", cfg.Queue)
	require.Equal(t, "builder-default-2", cfg.ConsumerTag(2))
	require.Equal(t, "builder-a-1", rabbitmq.BuildConsumerConfig("", "a", platformrabbitmq.Queue{}, nil).ConsumerTag(1))
	require.Equal(t, "request", cfg.RequestID(msg))
	require.Equal(t, []any{"build_id", "build"}, cfg.LogFields(msg))
}

func TestBuildConsumerConfigConsumesConfiguredQueue(t *testing.T) {
	configured := platformrabbitmq.Queue{Label: "build", Exchange: "x", Name: "q", RoutingKey: "k", DLX: "dx", DLQ: "dq", DLQKey: "dk"}

	cfg := rabbitmq.BuildConsumerConfig("amqp://test", "", configured, nil)

	require.Equal(t, "q", cfg.Queue)
}

func TestBuildConsumerPermanentRule(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"bad request", apperrors.ErrBadRequest, true},
		{"forbidden", apperrors.ErrForbidden, true},
		{"not found", apperrors.ErrNotFound, true},
		{"already exists", apperrors.ErrAlreadyExists, true},
		{"conflict", apperrors.ErrConflict, true},
		{"limit exceeded", apperrors.ErrLimitExceeded, true},
		{"wrapped conflict", fmt.Errorf("wrapped: %w", apperrors.ErrConflict), true},
		{"unavailable", apperrors.ErrUnavailable, false},
		// Finalization wraps a Core application error while retaining a retryable outcome.
		{"finalization wrapping a conflict", errors.Join(apperrors.ErrUnavailable, apperrors.ErrConflict), false},
		{"finalization wrapping not found", fmt.Errorf("finalize: %w: %w", apperrors.ErrUnavailable, apperrors.ErrNotFound), false},
		{"unknown failure", errors.New("temporary downstream failure"), false},
	}
	isPermanent := rabbitmq.BuildConsumerConfig("", "", platformrabbitmq.Queue{}, nil).IsPermanent
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isPermanent(tt.err))
		})
	}
}
