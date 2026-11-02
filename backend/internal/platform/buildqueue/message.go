package buildqueue

const (
	ExchangeName = "dcm.builds"
	QueueName    = "dcm.builds.image"
	RoutingKey   = "build.image"
	DLXName      = "dcm.builds.dlx"
	DLQName      = "dcm.builds.image.dlq"
	DLQKey       = "build.image.dead"
)

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
