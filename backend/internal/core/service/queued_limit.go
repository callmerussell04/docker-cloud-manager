package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
)

// checkQueuedLimit rejects with limit when the owner's queued work, as count
// reads it, plus adding would pass allowed. An allowed of zero or less is no
// limit, and nothing is counted.
func checkQueuedLimit(ctx context.Context, ownerID uuid.UUID, count func(context.Context, uuid.UUID) (int, error), adding, allowed int, limit apperrors.Limit) error {
	if allowed <= 0 {
		return nil
	}
	queued, err := count(ctx, ownerID)
	if err != nil {
		return err
	}
	if queued+adding > allowed {
		return apperrors.Exceeded(limit)
	}
	return nil
}
