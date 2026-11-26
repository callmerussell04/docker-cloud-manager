package rabbitmq

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	platformrabbitmq "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
	amqp "github.com/rabbitmq/amqp091-go"
)

// retryDelay is how long Builder holds a build message that failed with a
// retryable error before returning it to the queue.
const retryDelay = 5 * time.Second

// BuildConsumerConfig describes the image build workload. Builder declares and
// consumes only queue; a zero queue selects buildqueue.Queue.
func BuildConsumerConfig(url, instanceID string, queue platformrabbitmq.Queue, logger *slog.Logger) platformrabbitmq.ConsumerConfig[buildqueue.ImageBuildMessage] {
	if instanceID == "" {
		instanceID = "default"
	}
	if queue == (platformrabbitmq.Queue{}) {
		queue = buildqueue.Queue
	}
	return platformrabbitmq.ConsumerConfig[buildqueue.ImageBuildMessage]{
		URL:         url,
		Queue:       queue.Name,
		ConsumerTag: func(workerID int) string { return fmt.Sprintf("builder-%s-%d", instanceID, workerID) },
		Declare: func(ch *amqp.Channel) error {
			return platformrabbitmq.Declare(ch, queue)
		},
		IsPermanent: isPermanentBuildError,
		RequestID:   func(msg buildqueue.ImageBuildMessage) string { return msg.RequestID },
		LogFields: func(msg buildqueue.ImageBuildMessage) []any {
			return []any{"build_id", msg.BuildID}
		},
		Logger:     logging.WithComponent(logger, "build_queue_consumer").With("builder_instance_id", instanceID),
		RetryDelay: retryDelay,
	}
}

func isPermanentBuildError(err error) bool {
	// Finalization can wrap a Core application error while retaining a retryable outcome.
	if errors.Is(err, apperrors.ErrUnavailable) {
		return false
	}
	return platformrabbitmq.IsPermanentAppError(err)
}
