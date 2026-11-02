package resourcequeue

const (
	ExchangeName = "dcm.resources"
	QueueName    = "dcm.resources.lifecycle"
	RoutingKey   = "resource.lifecycle"
	DLXName      = "dcm.resources.dlx"
	DLQName      = "dcm.resources.lifecycle.dlq"
	DLQKey       = "resource.lifecycle.dead"
)

type LifecycleMessage struct {
	OperationID  string `json:"operation_id"`
	ResourceID   string `json:"resource_id"`
	ResourceType string `json:"resource_type"`
	OwnerID      string `json:"owner_id"`
	Operation    string `json:"operation"`
	RequestID    string `json:"request_id"`
	CreatedAt    int64  `json:"created_at"`
}
