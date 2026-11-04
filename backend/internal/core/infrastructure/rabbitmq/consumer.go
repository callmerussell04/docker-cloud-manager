package rabbitmq

import (
	"fmt"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	platformrabbitmq "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
)

// ComposeConsumerConfig describes the Compose deployment workload. Every Core
// consumer declares the whole topology.
func ComposeConsumerConfig(url, instanceID string, topology Topology, logger *slog.Logger) platformrabbitmq.ConsumerConfig[queue.DeploymentMessage] {
	instanceID = coreInstanceID(instanceID)
	topology = topology.withDefaults()
	return platformrabbitmq.ConsumerConfig[queue.DeploymentMessage]{
		URL:         url,
		Queue:       topology.Compose.Name,
		ConsumerTag: func(workerID int) string { return fmt.Sprintf("core-%s-%d", instanceID, workerID) },
		Declare:     topology.declare,
		IsPermanent: platformrabbitmq.IsPermanentAppError,
		RequestID:   func(msg queue.DeploymentMessage) string { return msg.RequestID },
		LogFields: func(msg queue.DeploymentMessage) []any {
			return []any{"job_id", msg.JobID, "project_id", msg.ProjectID}
		},
		Logger: consumerLogger(logger, "compose_queue_consumer", instanceID),
	}
}

// ContainerConsumerConfig describes the container lifecycle workload.
func ContainerConsumerConfig(url, instanceID string, topology Topology, logger *slog.Logger) platformrabbitmq.ConsumerConfig[queue.ContainerLifecycleMessage] {
	instanceID = coreInstanceID(instanceID)
	topology = topology.withDefaults()
	return platformrabbitmq.ConsumerConfig[queue.ContainerLifecycleMessage]{
		URL:         url,
		Queue:       topology.Container.Name,
		ConsumerTag: func(workerID int) string { return fmt.Sprintf("core-%s-container-%d", instanceID, workerID) },
		Declare:     topology.declare,
		IsPermanent: platformrabbitmq.IsPermanentAppError,
		RequestID:   func(msg queue.ContainerLifecycleMessage) string { return msg.RequestID },
		LogFields: func(msg queue.ContainerLifecycleMessage) []any {
			return []any{"container_id", msg.ContainerID, "operation_id", msg.OperationID}
		},
		Logger: consumerLogger(logger, "container_queue_consumer", instanceID),
	}
}

// ResourceConsumerConfig describes the volume and image lifecycle workload.
func ResourceConsumerConfig(url, instanceID string, topology Topology, logger *slog.Logger) platformrabbitmq.ConsumerConfig[queue.ResourceLifecycleMessage] {
	instanceID = coreInstanceID(instanceID)
	topology = topology.withDefaults()
	return platformrabbitmq.ConsumerConfig[queue.ResourceLifecycleMessage]{
		URL:         url,
		Queue:       topology.Resource.Name,
		ConsumerTag: func(workerID int) string { return fmt.Sprintf("core-%s-resource-%d", instanceID, workerID) },
		Declare:     topology.declare,
		IsPermanent: platformrabbitmq.IsPermanentAppError,
		RequestID:   func(msg queue.ResourceLifecycleMessage) string { return msg.RequestID },
		LogFields: func(msg queue.ResourceLifecycleMessage) []any {
			return []any{"resource_type", msg.ResourceType, "resource_id", msg.ResourceID, "operation_id", msg.OperationID}
		},
		Logger: consumerLogger(logger, "resource_queue_consumer", instanceID),
	}
}

func coreInstanceID(instanceID string) string {
	if instanceID == "" {
		return "core"
	}
	return instanceID
}

func consumerLogger(logger *slog.Logger, component, instanceID string) *slog.Logger {
	return logging.WithComponent(logger, component).With("core_instance_id", instanceID)
}
