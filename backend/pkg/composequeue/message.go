package composequeue

const (
	ExchangeName = "dcm.compose"
	QueueName    = "dcm.compose.deploy"
	RoutingKey   = "compose.deploy"
	DLXName      = "dcm.compose.dlx"
	DLQName      = "dcm.compose.deploy.dlq"
	DLQKey       = "compose.deploy.dead"
)

type DeploymentMessage struct {
	JobID     string `json:"job_id"`
	ProjectID string `json:"project_id"`
	OwnerID   string `json:"owner_id"`
	RequestID string `json:"request_id"`
	CreatedAt int64  `json:"created_at"`
}
