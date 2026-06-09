package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	gatewayservicemocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/gateway/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAuthServiceDelegatesToSSOProvider(t *testing.T) {
	ctx := context.Background()
	provider := gatewayservicemocks.NewSSOProvider(t)
	svc := service.NewAuth(provider)
	tokens := model.Tokens{AccessToken: "access", RefreshToken: "refresh"}
	authUser := model.AuthUser{UserID: uuid.NewString(), Username: "alice", Role: "admin"}

	provider.EXPECT().Register(mock.Anything, "alice", "alice@example.com", "secret").Return("user-id", nil)
	provider.EXPECT().Login(mock.Anything, "alice", "secret").Return(tokens, nil)
	provider.EXPECT().Refresh(mock.Anything, "refresh").Return(tokens, nil)
	provider.EXPECT().GetAuthConfig(mock.Anything).Return(model.AuthConfig{LocalLoginEnabled: true}, nil)
	provider.EXPECT().StartOIDCLogin(mock.Anything, "keycloak", "/auth/callback").Return(model.OIDCLoginStartResult{AuthURL: "https://keycloak/auth", StateBinding: "binding"}, nil)
	provider.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", "code", "state", "binding").Return(model.OIDCCallbackResult{Tokens: tokens, RedirectAfter: "/auth/callback"}, nil)
	provider.EXPECT().VerifyAccessToken(mock.Anything, "Bearer access").Return(authUser, nil)
	provider.EXPECT().CheckPermission(mock.Anything, "Bearer access", "users.admin.list").Return(authUser, nil)

	userID, err := svc.Register(ctx, "alice", "alice@example.com", "secret")
	require.NoError(t, err)
	require.Equal(t, "user-id", userID)
	gotTokens, err := svc.Login(ctx, "alice", "secret")
	require.NoError(t, err)
	require.Equal(t, tokens, gotTokens)
	gotTokens, err = svc.Refresh(ctx, "refresh")
	require.NoError(t, err)
	require.Equal(t, tokens, gotTokens)
	cfg, err := svc.GetAuthConfig(ctx)
	require.NoError(t, err)
	require.True(t, cfg.LocalLoginEnabled)
	start, err := svc.StartOIDCLogin(ctx, "keycloak", "/auth/callback")
	require.NoError(t, err)
	require.Equal(t, "binding", start.StateBinding)
	callback, err := svc.CompleteOIDCCallback(ctx, "keycloak", "code", "state", "binding")
	require.NoError(t, err)
	require.Equal(t, "refresh", callback.Tokens.RefreshToken)
	user, err := svc.VerifyAccessToken(ctx, "Bearer access")
	require.NoError(t, err)
	require.Equal(t, authUser, user)
	user, err = svc.CheckPermission(ctx, "Bearer access", "users.admin.list")
	require.NoError(t, err)
	require.Equal(t, authUser, user)
}

func TestCoreServiceDelegatesResourcesAndReports(t *testing.T) {
	ctx := context.Background()
	provider := gatewayservicemocks.NewCoreProvider(t)
	svc := service.NewCore(provider)
	containerInput := model.CreateContainerInput{Name: "web", ImageTag: "nginx:latest", InternalPort: 80}
	volumeInput := model.CreateVolumeInput{Name: "data"}
	config := model.SystemConfig{BaseDomain: "example.test"}
	auditFilters := model.AuditEventFilters{Action: "containers.create", Page: 2, Limit: 10}
	auditEvent := model.AuditEvent{Action: "containers.create"}

	provider.EXPECT().CreateContainer(mock.Anything, containerInput).Return("container-id", nil)
	provider.EXPECT().ActionContainer(mock.Anything, "container-id", "start").Return(nil)
	provider.EXPECT().ExposeContainer(mock.Anything, "container-id", "web", 80).Return(nil)
	provider.EXPECT().ListContainers(mock.Anything, 1, 20, "project-id").Return(model.PaginatedContainers{TotalCount: 1}, nil)
	provider.EXPECT().GetContainerStats(mock.Anything, "container-id").Return(model.ContainerStats{CPUPercentage: 12.5}, nil)
	provider.EXPECT().CreateVolume(mock.Anything, volumeInput).Return("volume-id", nil)
	provider.EXPECT().DeleteVolume(mock.Anything, "volume-id").Return(nil)
	provider.EXPECT().ListVolumes(mock.Anything, 1, 20).Return(model.PaginatedVolumes{TotalCount: 1}, nil)
	provider.EXPECT().ListImages(mock.Anything, 1, 20).Return(model.PaginatedImages{TotalCount: 1}, nil)
	provider.EXPECT().DeleteImage(mock.Anything, "image-id").Return(nil)
	provider.EXPECT().ListBuilds(mock.Anything, 1, 20).Return(model.PaginatedBuilds{TotalCount: 1}, nil)
	provider.EXPECT().GetBuild(mock.Anything, "build-id").Return(model.Build{ID: "build-id"}, nil)
	provider.EXPECT().CancelBuildRecord(mock.Anything, "build-id").Return(nil)
	provider.EXPECT().DeleteBuild(mock.Anything, "build-id").Return(nil)
	provider.EXPECT().ListProjects(mock.Anything, 1, 20).Return(model.PaginatedProjects{TotalCount: 1}, nil)
	provider.EXPECT().StartProject(mock.Anything, "project-id").Return(nil)
	provider.EXPECT().StopProject(mock.Anything, "project-id").Return(nil)
	provider.EXPECT().CancelProject(mock.Anything, "project-id").Return(nil)
	provider.EXPECT().DeleteProject(mock.Anything, "project-id").Return(nil)
	provider.EXPECT().GetSystemConfig(mock.Anything).Return(config, nil)
	provider.EXPECT().UpdateSystemConfig(mock.Anything, config).Return(nil)
	provider.EXPECT().GetUserStats(mock.Anything).Return(model.UserStats{ContainersTotal: 2}, nil)
	provider.EXPECT().GetSystemMonitoring(mock.Anything).Return(model.SystemMonitoring{ContainersTotal: 3}, nil)
	provider.EXPECT().GetReportsOverview(mock.Anything, int64(1), int64(2)).Return(model.ReportsOverview{From: 1, To: 2}, nil)
	provider.EXPECT().ListUserUsageReport(mock.Anything, int64(1), int64(2), "cpu", "alice", 1, 20).Return([]model.UserUsageReportItem{{OwnerUsername: "alice"}}, 1, nil)
	provider.EXPECT().GetUserUsageTimeline(mock.Anything, "owner-id", int64(1), int64(2)).Return([]model.UserUsagePoint{{BucketStart: 1}}, nil)
	provider.EXPECT().ListAuditEvents(mock.Anything, auditFilters).Return([]model.AuditEvent{{Action: "containers.create"}}, 1, nil)
	provider.EXPECT().RecordAuditEvent(mock.Anything, auditEvent).Return(nil)
	provider.EXPECT().RefreshUsageSnapshots(mock.Anything).Return(model.RefreshUsageSnapshotsResult{SnapshotsCount: 1}, nil)

	id, err := svc.CreateContainer(ctx, containerInput)
	require.NoError(t, err)
	require.Equal(t, "container-id", id)
	require.NoError(t, svc.ActionContainer(ctx, "container-id", "start"))
	require.NoError(t, svc.ExposeContainer(ctx, "container-id", "web", 80))
	containers, err := svc.ListContainers(ctx, 1, 20, "project-id")
	require.NoError(t, err)
	require.EqualValues(t, 1, containers.TotalCount)
	stats, err := svc.GetContainerStats(ctx, "container-id")
	require.NoError(t, err)
	require.Equal(t, 12.5, stats.CPUPercentage)
	id, err = svc.CreateVolume(ctx, volumeInput)
	require.NoError(t, err)
	require.Equal(t, "volume-id", id)
	require.NoError(t, svc.DeleteVolume(ctx, "volume-id"))
	_, err = svc.ListVolumes(ctx, 1, 20)
	require.NoError(t, err)
	_, err = svc.ListImages(ctx, 1, 20)
	require.NoError(t, err)
	require.NoError(t, svc.DeleteImage(ctx, "image-id"))
	_, err = svc.ListBuilds(ctx, 1, 20)
	require.NoError(t, err)
	_, err = svc.GetBuild(ctx, "build-id")
	require.NoError(t, err)
	require.NoError(t, svc.CancelBuildRecord(ctx, "build-id"))
	require.NoError(t, svc.DeleteBuild(ctx, "build-id"))
	_, err = svc.ListProjects(ctx, 1, 20)
	require.NoError(t, err)
	require.NoError(t, svc.StartProject(ctx, "project-id"))
	require.NoError(t, svc.StopProject(ctx, "project-id"))
	require.NoError(t, svc.CancelProject(ctx, "project-id"))
	require.NoError(t, svc.DeleteProject(ctx, "project-id"))
	gotCfg, err := svc.GetSystemConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, config, gotCfg)
	require.NoError(t, svc.UpdateSystemConfig(ctx, config))
	_, err = svc.GetUserStats(ctx)
	require.NoError(t, err)
	_, err = svc.GetSystemMonitoring(ctx)
	require.NoError(t, err)
	_, err = svc.GetReportsOverview(ctx, 1, 2)
	require.NoError(t, err)
	_, total, err := svc.ListUserUsageReport(ctx, 1, 2, "cpu", "alice", 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	_, err = svc.GetUserUsageTimeline(ctx, "owner-id", 1, 2)
	require.NoError(t, err)
	_, total, err = svc.ListAuditEvents(ctx, auditFilters)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.NoError(t, svc.RecordAuditEvent(ctx, auditEvent))
	refresh, err := svc.RefreshUsageSnapshots(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, refresh.SnapshotsCount)
}

func TestUserManagementServiceDelegates(t *testing.T) {
	ctx := context.Background()
	provider := gatewayservicemocks.NewUserProvider(t)
	svc := service.NewUserManagement(provider)
	input := model.CreateUserInput{Username: "alice", Email: "alice@example.com", Password: "secret", Role: "user", Status: "active", QuotaCPU: 1, QuotaRAMMB: 1024, QuotaDiskMB: 2048}
	update := model.UpdateUserInput{Username: "alice2", Email: "alice2@example.com", Role: "admin", Status: "active", QuotaCPU: 2, QuotaRAMMB: 2048, QuotaDiskMB: 4096}
	user := model.AdminUser{UserID: "user-id", Username: "alice"}

	provider.EXPECT().ListUsers(mock.Anything, 1, 20).Return(model.PaginatedAdminUsers{Users: []model.AdminUser{user}, TotalCount: 1}, nil)
	provider.EXPECT().GetUser(mock.Anything, "user-id").Return(user, nil)
	provider.EXPECT().CreateUser(mock.Anything, input).Return(user, nil)
	provider.EXPECT().UpdateUser(mock.Anything, "user-id", update).Return(user, nil)
	provider.EXPECT().DeactivateUser(mock.Anything, "user-id").Return(user, nil)
	provider.EXPECT().ReactivateUser(mock.Anything, "user-id").Return(user, nil)

	list, err := svc.ListUsers(ctx, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, list.TotalCount)
	got, err := svc.GetUser(ctx, "user-id")
	require.NoError(t, err)
	require.Equal(t, user, got)
	got, err = svc.CreateUser(ctx, input)
	require.NoError(t, err)
	require.Equal(t, user, got)
	got, err = svc.UpdateUser(ctx, "user-id", update)
	require.NoError(t, err)
	require.Equal(t, user, got)
	got, err = svc.DeactivateUser(ctx, "user-id")
	require.NoError(t, err)
	require.Equal(t, user, got)
	got, err = svc.ReactivateUser(ctx, "user-id")
	require.NoError(t, err)
	require.Equal(t, user, got)
}

func TestTelemetryTicketStoreIssueConsumeAndRejectsInvalidTickets(t *testing.T) {
	store := service.NewTelemetryTicketStore(time.Minute)
	ownerID := uuid.New()

	ticket, err := store.Issue(context.Background(), model.TelemetryTicketClaims{
		ContainerID: "container-id",
		StreamType:  model.TelemetryStreamTerminal,
		Scope:       accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID},
	})
	require.NoError(t, err)
	require.NotEmpty(t, ticket.Ticket)

	claims, err := store.Consume(context.Background(), ticket.Ticket, "container-id", model.TelemetryStreamTerminal, false)
	require.NoError(t, err)
	require.Equal(t, ownerID, claims.Scope.UserID)
	_, err = store.Consume(context.Background(), ticket.Ticket, "container-id", model.TelemetryStreamTerminal, false)
	require.ErrorIs(t, err, apperrors.ErrUnauthorized)

	_, err = store.Consume(context.Background(), "bad-ticket", "container-id", model.TelemetryStreamLogs, false)
	require.ErrorIs(t, err, apperrors.ErrUnauthorized)

	ticket, err = store.Issue(context.Background(), model.TelemetryTicketClaims{ContainerID: "container-id", StreamType: model.TelemetryStreamLogs, Scope: accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID}})
	require.NoError(t, err)
	_, err = store.Consume(context.Background(), ticket.Ticket, "other-id", model.TelemetryStreamLogs, false)
	require.ErrorIs(t, err, apperrors.ErrForbidden)

	expiringStore := service.NewTelemetryTicketStore(time.Nanosecond)
	ticket, err = expiringStore.Issue(context.Background(), model.TelemetryTicketClaims{ContainerID: "container-id", StreamType: model.TelemetryStreamLogs, Scope: accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID}})
	require.NoError(t, err)
	time.Sleep(time.Millisecond)
	_, err = expiringStore.Consume(context.Background(), ticket.Ticket, "container-id", model.TelemetryStreamLogs, false)
	require.ErrorIs(t, err, apperrors.ErrUnauthorized)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.Issue(ctx, model.TelemetryTicketClaims{})
	require.ErrorIs(t, err, context.Canceled)
	_, err = store.Consume(ctx, ticket.Ticket, "container-id", model.TelemetryStreamLogs, false)
	require.ErrorIs(t, err, context.Canceled)
}

func TestCoreServicePropagatesProviderError(t *testing.T) {
	provider := gatewayservicemocks.NewCoreProvider(t)
	svc := service.NewCore(provider)
	provider.EXPECT().DeleteImage(mock.Anything, "image-id").Return(errors.New("boom"))

	err := svc.DeleteImage(context.Background(), "image-id")
	require.EqualError(t, err, "boom")
}
