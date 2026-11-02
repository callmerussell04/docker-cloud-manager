package compose

import (
	"context"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/google/uuid"
)

type AuditRecorder interface {
	RecordAuditEvent(ctx context.Context, event model.AuditEvent) error
}

func (c *ComposeDeploymentCoordinator) recordComposeExposeAudit(ctx context.Context, ownerID, projectID uuid.UUID, projectName, sourceType string, srv model.ComposeService, containerID uuid.UUID, containerName string) {
	if c.auditor == nil || srv.DomainPrefix == "" || srv.InternalPort <= 0 {
		return
	}
	actorID, actorUsername, actorScope := auditActorFromContext(ctx)
	ownerUsername := actorUsername
	if ownerUsername == "" && c.users != nil {
		if user, err := c.users.GetUser(ctx, ownerID); err == nil {
			ownerUsername = user.Username
			if actorUsername == "" && actorID != nil && *actorID == ownerID {
				actorUsername = user.Username
			}
		}
	}
	// Audit is best effort: a failed write is logged and never fails the deployment.
	err := c.auditor.RecordAuditEvent(ctx, model.AuditEvent{
		ActorUserID:   actorID,
		ActorUsername: actorUsername,
		ActorScope:    actorScope,
		Action:        auditlog.ActionContainerExpose,
		Outcome:       auditlog.OutcomeSuccess,
		ResourceType:  auditlog.ResourceContainer,
		ResourceID:    containerID.String(),
		ResourceName:  containerName,
		OwnerID:       ownerPtr(ownerID),
		OwnerUsername: ownerUsername,
		RequestID:     requestIDFromContext(ctx),
		DetailsJSON: auditlog.SafeDetailsJSON(map[string]string{
			auditlog.DetailSourceType:     sourceType,
			auditlog.DetailProjectID:      projectID.String(),
			auditlog.DetailProjectName:    projectName,
			auditlog.DetailComposeService: srv.Name,
			auditlog.DetailContainerID:    containerID.String(),
			auditlog.DetailContainerName:  containerName,
			auditlog.DetailDomainPrefix:   srv.DomainPrefix,
			auditlog.DetailFullDomain:     composeFullDomain(srv.DomainPrefix, c.cfg.Get().BaseDomain),
			auditlog.DetailInternalPort:   auditlog.IntDetail(srv.InternalPort),
		}),
	})
	if err != nil {
		c.logger.WarnContext(ctx, "failed to record audit event", "project_id", projectID, "container_id", containerID, "action", auditlog.ActionContainerExpose, "error", err)
	}
}

func auditActorFromContext(ctx context.Context) (*uuid.UUID, string, string) {
	scope, ok := accessscope.FromContext(ctx)
	if !ok || scope.Kind == "" {
		return nil, "", auditlog.ActorScopeUnknown
	}
	var actorID *uuid.UUID
	if scope.UserID != uuid.Nil {
		value := scope.UserID
		actorID = &value
	}
	return actorID, scope.Username, string(scope.Kind)
}

func ownerPtr(ownerID uuid.UUID) *uuid.UUID {
	if ownerID == uuid.Nil {
		return nil
	}
	value := ownerID
	return &value
}

func composeFullDomain(prefix, baseDomain string) string {
	if prefix == "" {
		return ""
	}
	if baseDomain == "" {
		return prefix
	}
	return fmt.Sprintf("%s.%s", prefix, baseDomain)
}

func requestIDFromContext(ctx context.Context) string {
	return logging.RequestIDFromContext(ctx)
}
