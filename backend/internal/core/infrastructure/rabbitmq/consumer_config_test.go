package rabbitmq_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	platformrabbitmq "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
	"github.com/stretchr/testify/require"
)

// The literals pin what the broker and in-flight messages see.
func TestCoreQueueDescriptions(t *testing.T) {
	tests := []struct {
		name string
		got  platformrabbitmq.Queue
		want platformrabbitmq.Queue
	}{
		{"compose", queue.Compose, platformrabbitmq.Queue{
			Label: "compose", Exchange: "dcm.compose", Name: "dcm.compose.deploy", RoutingKey: "compose.deploy",
			DLX: "dcm.compose.dlx", DLQ: "dcm.compose.deploy.dlq", DLQKey: "compose.deploy.dead",
		}},
		{"container", queue.Container, platformrabbitmq.Queue{
			Label: "container", Exchange: "dcm.containers", Name: "dcm.containers.lifecycle", RoutingKey: "container.lifecycle",
			DLX: "dcm.containers.dlx", DLQ: "dcm.containers.lifecycle.dlq", DLQKey: "container.lifecycle.dead",
		}},
		{"resource", queue.Resource, platformrabbitmq.Queue{
			Label: "resource", Exchange: "dcm.resources", Name: "dcm.resources.lifecycle", RoutingKey: "resource.lifecycle",
			DLX: "dcm.resources.dlx", DLQ: "dcm.resources.lifecycle.dlq", DLQKey: "resource.lifecycle.dead",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.got)
		})
	}
}

func TestComposeConsumerConfig(t *testing.T) {
	cfg := rabbitmq.ComposeConsumerConfig("amqp://test", "", rabbitmq.Topology{}, nil)
	msg := queue.DeploymentMessage{JobID: "job", ProjectID: "project", RequestID: "request"}

	require.Equal(t, "dcm.compose.deploy", cfg.Queue)
	require.Equal(t, "core-core-2", cfg.ConsumerTag(2))
	require.Equal(t, "request", cfg.RequestID(msg))
	require.Equal(t, []any{"job_id", "job", "project_id", "project"}, cfg.LogFields(msg))
	requireCorePermanentRule(t, cfg.IsPermanent)
}

func TestContainerConsumerConfig(t *testing.T) {
	cfg := rabbitmq.ContainerConsumerConfig("amqp://test", "", rabbitmq.Topology{}, nil)
	msg := queue.ContainerLifecycleMessage{ContainerID: "container", OperationID: "operation", RequestID: "request"}

	require.Equal(t, "dcm.containers.lifecycle", cfg.Queue)
	require.Equal(t, "core-core-container-2", cfg.ConsumerTag(2))
	require.Equal(t, "request", cfg.RequestID(msg))
	require.Equal(t, []any{"container_id", "container", "operation_id", "operation"}, cfg.LogFields(msg))
	requireCorePermanentRule(t, cfg.IsPermanent)
}

func TestResourceConsumerConfig(t *testing.T) {
	cfg := rabbitmq.ResourceConsumerConfig("amqp://test", "", rabbitmq.Topology{}, nil)
	msg := queue.ResourceLifecycleMessage{ResourceType: "volume", ResourceID: "resource", OperationID: "operation", RequestID: "request"}

	require.Equal(t, "dcm.resources.lifecycle", cfg.Queue)
	require.Equal(t, "core-core-resource-2", cfg.ConsumerTag(2))
	require.Equal(t, "request", cfg.RequestID(msg))
	require.Equal(t, []any{"resource_type", "volume", "resource_id", "resource", "operation_id", "operation"}, cfg.LogFields(msg))
	requireCorePermanentRule(t, cfg.IsPermanent)
}

func TestCoreConsumerConfigsUseGivenInstanceID(t *testing.T) {
	require.Equal(t, "core-a-1", rabbitmq.ComposeConsumerConfig("", "a", rabbitmq.Topology{}, nil).ConsumerTag(1))
	require.Equal(t, "core-a-container-1", rabbitmq.ContainerConsumerConfig("", "a", rabbitmq.Topology{}, nil).ConsumerTag(1))
	require.Equal(t, "core-a-resource-1", rabbitmq.ResourceConsumerConfig("", "a", rabbitmq.Topology{}, nil).ConsumerTag(1))
}

func TestCoreConsumerConfigsConsumeConfiguredTopology(t *testing.T) {
	topology := rabbitmq.Topology{
		Compose:   platformrabbitmq.Queue{Label: "compose", Exchange: "cx", Name: "cq", RoutingKey: "ck", DLX: "cdx", DLQ: "cdq", DLQKey: "cdk"},
		Container: platformrabbitmq.Queue{Label: "container", Exchange: "tx", Name: "tq", RoutingKey: "tk", DLX: "tdx", DLQ: "tdq", DLQKey: "tdk"},
		Resource:  platformrabbitmq.Queue{Label: "resource", Exchange: "rx", Name: "rq", RoutingKey: "rk", DLX: "rdx", DLQ: "rdq", DLQKey: "rdk"},
	}

	require.Equal(t, "cq", rabbitmq.ComposeConsumerConfig("", "", topology, nil).Queue)
	require.Equal(t, "tq", rabbitmq.ContainerConsumerConfig("", "", topology, nil).Queue)
	require.Equal(t, "rq", rabbitmq.ResourceConsumerConfig("", "", topology, nil).Queue)
}

func requireCorePermanentRule(t *testing.T, isPermanent func(error) bool) {
	t.Helper()
	for _, err := range []error{
		apperrors.ErrBadRequest,
		apperrors.ErrForbidden,
		apperrors.ErrNotFound,
		apperrors.ErrAlreadyExists,
		apperrors.ErrConflict,
		apperrors.ErrLimitExceeded,
		fmt.Errorf("wrapped: %w", apperrors.ErrConflict),
		// Unlike Builder, Core has no retryable outcome that overrides an application error.
		errors.Join(apperrors.ErrUnavailable, apperrors.ErrConflict),
	} {
		require.True(t, isPermanent(err), "%v must be permanent", err)
	}
	for _, err := range []error{
		apperrors.ErrUnavailable,
		apperrors.ErrTimeout,
		errors.New("temporary downstream failure"),
	} {
		require.False(t, isPermanent(err), "%v must be retried", err)
	}
}
