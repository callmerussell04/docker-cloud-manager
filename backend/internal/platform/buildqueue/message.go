package buildqueue

import "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"

const (
	ExchangeName = "dcm.builds"
	QueueName    = "dcm.builds.image"
	RoutingKey   = "build.image"
	DLXName      = "dcm.builds.dlx"
	DLQName      = "dcm.builds.image.dlq"
	DLQKey       = "build.image.dead"
)

// Queue describes the build queue that Core publishes to and Builder consumes.
var Queue = rabbitmq.Queue{
	Label:      "build",
	Exchange:   ExchangeName,
	Name:       QueueName,
	RoutingKey: RoutingKey,
	DLX:        DLXName,
	DLQ:        DLQName,
	DLQKey:     DLQKey,
}

type ImageBuildMessage struct {
	BuildID          string            `json:"build_id"`
	ImageID          string            `json:"image_id"`
	OwnerID          string            `json:"owner_id"`
	Tag              string            `json:"tag"`
	ArchiveObjectKey string            `json:"archive_object_key"`
	LogObjectKey     string            `json:"log_object_key"`
	ContextDir       string            `json:"context_dir"`
	Dockerfile       string            `json:"dockerfile"`
	BuildArgs        map[string]string `json:"build_args"`
	RequestID        string            `json:"request_id"`
	CreatedAt        int64             `json:"created_at"`
}
