package rabbitmq

import (
	"fmt"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	platformrabbitmq "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
)

// ComposeConsumerConfig describes the Compose deployment workload.
func ComposeConsumerConfig(url, instanceID string, logger *slog.Logger) platformrabbitmq.ConsumerConfig[queue.DeploymentMessage] {
	instanceID = coreInstanceID(instanceID)
	return platformrabbitmq.ConsumerConfig[queue.DeploymentMessage]{
		URL:         url,
		Queue:       queue.ComposeQueueName,
		ConsumerTag: func(workerID int) string { return fmt.Sprintf("core-%s-%d", instanceID, workerID) },
		Declare:     declareTopology,
		IsPermanent: platformrabbitmq.IsPermanentAppError,
		RequestID:   func(msg queue.DeploymentMessage) string { return msg.RequestID },
		LogFields: func(msg queue.DeploymentMessage) []any {
			return []any{"job_id", msg.JobID, "project_id", msg.ProjectID}
		},
		Logger: consumerLogger(logger, "compose_queue_consumer", instanceID),
	}
}

// ContainerConsumerConfig describes the container lifecycle workload.
func ContainerConsumerConfig(url, instanceID string, logger *slog.Logger) platformrabbitmq.ConsumerConfig[queue.ContainerLifecycleMessage] {
	instanceID = coreInstanceID(instanceID)
	return platformrabbitmq.ConsumerConfig[queue.ContainerLifecycleMessage]{
		URL:         url,
		Queue:       queue.ContainerQueueName,
		ConsumerTag: func(workerID int) string { return fmt.Sprintf("core-%s-container-%d", instanceID, workerID) },
		Declare:     declareTopology,
		IsPermanent: platformrabbitmq.IsPermanentAppError,
		RequestID:   func(msg queue.ContainerLifecycleMessage) string { return msg.RequestID },
		LogFields: func(msg queue.ContainerLifecycleMessage) []any {
			return []any{"container_id", msg.ContainerID, "operation_id", msg.OperationID}
		},
		Logger: consumerLogger(logger, "container_queue_consumer", instanceID),
	}
}

// ResourceConsumerConfig describes the volume and image lifecycle workload.
func ResourceConsumerConfig(url, instanceID string, logger *slog.Logger) platformrabbitmq.ConsumerConfig[queue.ResourceLifecycleMessage] {
	instanceID = coreInstanceID(instanceID)
	return platformrabbitmq.ConsumerConfig[queue.ResourceLifecycleMessage]{
		URL:         url,
		Queue:       queue.ResourceQueueName,
		ConsumerTag: func(workerID int) string { return fmt.Sprintf("core-%s-resource-%d", instanceID, workerID) },
		Declare:     declareTopology,
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
