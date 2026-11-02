package containerqueue

const (
	ExchangeName = "dcm.containers"
	QueueName    = "dcm.containers.lifecycle"
	RoutingKey   = "container.lifecycle"
	DLXName      = "dcm.containers.dlx"
	DLQName      = "dcm.containers.lifecycle.dlq"
	DLQKey       = "container.lifecycle.dead"
)

type LifecycleMessage struct {
	OperationID    string `json:"operation_id"`
	ContainerID    string `json:"container_id"`
	OwnerID        string `json:"owner_id"`
	Operation      string `json:"operation,omitempty"`
	RequestID      string `json:"request_id"`
	CreatedAt      int64  `json:"created_at"`
	DomainPrefix   string `json:"domain_prefix,omitempty"`
	InternalPort   int    `json:"internal_port,omitempty"`
	PreviousStatus string `json:"previous_status,omitempty"`
}
