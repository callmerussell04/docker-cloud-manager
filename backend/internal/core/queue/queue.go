// Package queue holds the RabbitMQ contracts private to Core: the compose,
// container and resource queues and their messages.
package queue

import "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"

const (
	ComposeExchangeName = "dcm.compose"
	ComposeQueueName    = "dcm.compose.deploy"
	ComposeRoutingKey   = "compose.deploy"
	ComposeDLXName      = "dcm.compose.dlx"
	ComposeDLQName      = "dcm.compose.deploy.dlq"
	ComposeDLQKey       = "compose.deploy.dead"

	ContainerExchangeName = "dcm.containers"
	ContainerQueueName    = "dcm.containers.lifecycle"
	ContainerRoutingKey   = "container.lifecycle"
	ContainerDLXName      = "dcm.containers.dlx"
	ContainerDLQName      = "dcm.containers.lifecycle.dlq"
	ContainerDLQKey       = "container.lifecycle.dead"

	ResourceExchangeName = "dcm.resources"
	ResourceQueueName    = "dcm.resources.lifecycle"
	ResourceRoutingKey   = "resource.lifecycle"
	ResourceDLXName      = "dcm.resources.dlx"
	ResourceDLQName      = "dcm.resources.lifecycle.dlq"
	ResourceDLQKey       = "resource.lifecycle.dead"
)

var (
	Compose = rabbitmq.Queue{
		Label:      "compose",
		Exchange:   ComposeExchangeName,
		Name:       ComposeQueueName,
		RoutingKey: ComposeRoutingKey,
		DLX:        ComposeDLXName,
		DLQ:        ComposeDLQName,
		DLQKey:     ComposeDLQKey,
	}
	Container = rabbitmq.Queue{
		Label:      "container",
		Exchange:   ContainerExchangeName,
		Name:       ContainerQueueName,
		RoutingKey: ContainerRoutingKey,
		DLX:        ContainerDLXName,
		DLQ:        ContainerDLQName,
		DLQKey:     ContainerDLQKey,
	}
	Resource = rabbitmq.Queue{
		Label:      "resource",
		Exchange:   ResourceExchangeName,
		Name:       ResourceQueueName,
		RoutingKey: ResourceRoutingKey,
		DLX:        ResourceDLXName,
		DLQ:        ResourceDLQName,
		DLQKey:     ResourceDLQKey,
	}
)

type DeploymentMessage struct {
	JobID     string `json:"job_id"`
	ProjectID string `json:"project_id"`
	OwnerID   string `json:"owner_id"`
	RequestID string `json:"request_id"`
	CreatedAt int64  `json:"created_at"`
}

type ContainerLifecycleMessage struct {
	OperationID    string `json:"operation_id"`
	ContainerID    string `json:"container_id"`
	OwnerID        string `json:"owner_id"`
	Operation      string `json:"operation,omitempty"`
	RequestID      string `json:"request_id"`
	CreatedAt      int64  `json:"created_at"`
	DomainPrefix   string `json:"domain_prefix,omitempty"`
	InternalPort   int    `json:"internal_port,omitempty"`
	PreviousStatus string `json:"previous_status,omitempty"`
	// Reason, on a stop the platform queued itself, is the safe message
	// recorded as the stopped container's last error.
	Reason string `json:"reason,omitempty"`
}

type ResourceLifecycleMessage struct {
	OperationID  string `json:"operation_id"`
	ResourceID   string `json:"resource_id"`
	ResourceType string `json:"resource_type"`
	OwnerID      string `json:"owner_id"`
	Operation    string `json:"operation"`
	RequestID    string `json:"request_id"`
	CreatedAt    int64  `json:"created_at"`
}
