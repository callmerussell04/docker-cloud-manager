package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
)

type AuditRecorder interface {
	RecordAuditEvent(ctx context.Context, event model.AuditEvent) error
}
