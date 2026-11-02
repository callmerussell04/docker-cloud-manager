package rabbitmq

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	platformrabbitmq "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
	amqp "github.com/rabbitmq/amqp091-go"
)

// BuildConsumerConfig describes the image build workload. Builder declares only
// the build queue.
func BuildConsumerConfig(url, instanceID string, logger *slog.Logger) platformrabbitmq.ConsumerConfig[buildqueue.ImageBuildMessage] {
	if instanceID == "" {
		instanceID = "default"
	}
	return platformrabbitmq.ConsumerConfig[buildqueue.ImageBuildMessage]{
		URL:         url,
		Queue:       buildqueue.QueueName,
		ConsumerTag: func(workerID int) string { return fmt.Sprintf("builder-%s-%d", instanceID, workerID) },
		Declare: func(ch *amqp.Channel) error {
			return platformrabbitmq.Declare(ch, buildqueue.Queue)
		},
		IsPermanent: isPermanentBuildError,
		RequestID:   func(msg buildqueue.ImageBuildMessage) string { return msg.RequestID },
		LogFields: func(msg buildqueue.ImageBuildMessage) []any {
			return []any{"build_id", msg.BuildID}
		},
		Logger: logging.WithComponent(logger, "build_queue_consumer").With("builder_instance_id", instanceID),
	}
}

func isPermanentBuildError(err error) bool {
	// Finalization can wrap a Core application error while retaining a retryable outcome.
	if errors.Is(err, apperrors.ErrUnavailable) {
		return false
	}
	return platformrabbitmq.IsPermanentAppError(err)
}
