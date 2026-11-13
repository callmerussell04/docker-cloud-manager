package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	gatewayhandlermocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/gateway/http/handler"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type userHandlerHarness struct {
	router     *gin.Engine
	users      *gatewayhandlermocks.MockUserDirectory
	accounts   *gatewayhandlermocks.MockUserDeactivator
	adminUsers *gatewayhandlermocks.MockAdminUserService
	audit      *gatewayhandlermocks.MockAuditRecorder
}

func newUserHandlerHarness(t *testing.T) userHandlerHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := userHandlerHarness{
		users:      gatewayhandlermocks.NewMockUserDirectory(t),
		accounts:   gatewayhandlermocks.NewMockUserDeactivator(t),
		adminUsers: gatewayhandlermocks.NewMockAdminUserService(t),
		audit:      gatewayhandlermocks.NewMockAuditRecorder(t),
	}
	userHandler := handler.NewUserManagementHandler(handler.UserManagementDeps{Users: h.users, AdminUsers: h.adminUsers, Deactivation: h.accounts, Audit: h.audit})
	h.router = gin.New()
	h.router.Use(withScope(accessscope.WithAdminScope(context.Background(), adminActorID, "root", "admin")))
	h.router.GET("/users", userHandler.ListUsers)
	h.router.GET("/users/:id", userHandler.GetUser)
	h.router.POST("/users", userHandler.CreateUser)
	h.router.PUT("/users/:id", userHandler.UpdateUser)
	h.router.POST("/users/:id/activate", userHandler.ReactivateUser)
	h.router.PUT("/users/:id/quota", userHandler.SetUserQuota)
	h.router.DELETE("/users/:id/quota", userHandler.ResetUserQuota)
	return h
}

var adminActorID = uuid.New()

func TestUserManagementHandlerCRUDAndValidation(t *testing.T) {
	h := newUserHandlerHarness(t)
	userID := uuid.NewString()
	user := model.AdminUser{UserID: userID, Username: "alice", Email: "alice@example.com", Role: "user", Status: "active"}
	withQuota := model.AdminUserWithQuota{User: user, Quota: model.UserQuota{UserID: userID, CPU: 1.5, RAMMB: 1024, DiskMB: 2048, Custom: true}}

	h.adminUsers.EXPECT().ListUsers(mock.Anything, 2, 10).Return(model.PaginatedAdminUsersWithQuota{Users: []model.AdminUserWithQuota{withQuota}, TotalCount: 1}, nil)
	resp := perform(h.router, http.MethodGet, "/users?page=2&limit=10", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.JSONEq(t, `{"total_count":1,"users":[{"user_id":"`+userID+`","username":"alice","email":"alice@example.com","role":"user","status":"active","quota_cpu":1.5,"quota_ram_mb":1024,"quota_disk_mb":2048,"quota_custom":true}]}`, resp.Body.String())

	h.adminUsers.EXPECT().GetUser(mock.Anything, userID).Return(withQuota, nil)
	resp = perform(h.router, http.MethodGet, "/users/"+userID, ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"quota_custom":true`)

	h.users.EXPECT().CreateUser(mock.Anything, model.CreateUserInput{Username: "bob", Email: "bob@example.com", Password: "secret", Role: "user", Status: "active"}).Return(user, nil)
	resp = perform(h.router, http.MethodPost, "/users", `{"username":"bob","email":"bob@example.com","password":"secret","role":"user","status":"active"}`, nil)
	require.Equal(t, http.StatusCreated, resp.Code)
	require.NotContains(t, resp.Body.String(), "quota")

	h.accounts.EXPECT().UpdateUser(mock.Anything, userID, model.UpdateUserInput{Username: "alice2", Email: "alice2@example.com", Password: "newsecret", Role: "admin", Status: "active"}).Return(model.UserUpdate{User: user}, nil)
	resp = perform(h.router, http.MethodPut, "/users/"+userID, `{"username":"alice2","email":"alice2@example.com","password":"newsecret","role":"admin","status":"active"}`, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.NotContains(t, resp.Body.String(), "quota")
	require.NotContains(t, resp.Body.String(), "workloads", "an update that leaves the account active stops nothing")

	h.users.EXPECT().ReactivateUser(mock.Anything, userID).Return(user, nil)
	resp = perform(h.router, http.MethodPost, "/users/"+userID+"/activate", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)

	resp = perform(h.router, http.MethodGet, "/users/bad-id", ``, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	resp = perform(h.router, http.MethodPost, "/users", `{"username":"bad name","role":"user","status":"active"}`, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	resp = perform(h.router, http.MethodPost, "/users", `{"username":"alice","role":"owner","status":"active"}`, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	resp = perform(h.router, http.MethodPost, "/users", `{"username":"alice","role":"user","status":"disabled"}`, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestUserReadsFailAsUnavailableWhenCoreCannotAnswer(t *testing.T) {
	h := newUserHandlerHarness(t)
	userID := uuid.NewString()
	h.adminUsers.EXPECT().ListUsers(mock.Anything, 1, 20).Return(model.PaginatedAdminUsersWithQuota{}, apperrors.ErrUnavailable)
	h.adminUsers.EXPECT().GetUser(mock.Anything, userID).Return(model.AdminUserWithQuota{}, apperrors.ErrUnavailable)

	resp := perform(h.router, http.MethodGet, "/users", ``, nil)
	require.Equal(t, http.StatusServiceUnavailable, resp.Code)
	require.NotContains(t, resp.Body.String(), "quota_cpu")
	resp = perform(h.router, http.MethodGet, "/users/"+userID, ``, nil)
	require.Equal(t, http.StatusServiceUnavailable, resp.Code)
	require.NotContains(t, resp.Body.String(), "quota_cpu")
}

// auditedQuotaEvent matches the audit event of a quota change on userID by
// the harness's administrator.
func auditedQuotaEvent(action, userID, outcome string, details func(string) bool) any {
	return mock.MatchedBy(func(event model.AuditEvent) bool {
		return event.Action == action && event.ResourceType == "user" && event.ResourceID == userID && event.OwnerID == userID &&
			event.ActorUserID == adminActorID.String() && event.ActorScope == "admin" && event.Outcome == outcome && details(event.DetailsJSON)
	})
}

func TestSetAndResetUserQuotaAreAudited(t *testing.T) {
	h := newUserHandlerHarness(t)
	userID := uuid.NewString()
	h.adminUsers.EXPECT().SetUserQuota(mock.Anything, userID, model.UserQuotaLimits{CPU: 2.5, RAMMB: 4096, DiskMB: 10240}).
		Return(model.UserQuota{UserID: userID, CPU: 2.5, RAMMB: 4096, DiskMB: 10240, Custom: true}, nil)
	h.audit.EXPECT().RecordAuditEvent(mock.Anything, auditedQuotaEvent("user.quota_set", userID, "success", func(details string) bool {
		return details == `{"quota_cpu":"2.5","quota_disk_mb":"10240","quota_ram_mb":"4096"}`
	})).Return(nil).Once()

	resp := perform(h.router, http.MethodPut, "/users/"+userID+"/quota", `{"quota_cpu":2.5,"quota_ram_mb":4096,"quota_disk_mb":10240}`, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.JSONEq(t, `{"user_id":"`+userID+`","quota_cpu":2.5,"quota_ram_mb":4096,"quota_disk_mb":10240,"quota_custom":true}`, resp.Body.String())

	h.adminUsers.EXPECT().ResetUserQuota(mock.Anything, userID).Return(model.UserQuota{UserID: userID, CPU: 1, RAMMB: 2048, DiskMB: 5120}, nil)
	h.audit.EXPECT().RecordAuditEvent(mock.Anything, auditedQuotaEvent("user.quota_reset", userID, "success", func(details string) bool { return details == "{}" })).Return(nil).Once()

	resp = perform(h.router, http.MethodDelete, "/users/"+userID+"/quota", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.JSONEq(t, `{"user_id":"`+userID+`","quota_cpu":1,"quota_ram_mb":2048,"quota_disk_mb":5120,"quota_custom":false}`, resp.Body.String())
}

func TestQuotaRoutesAnswerNotFoundForAnUnknownUserAndAuditTheFailure(t *testing.T) {
	h := newUserHandlerHarness(t)
	userID := uuid.NewString()
	h.adminUsers.EXPECT().SetUserQuota(mock.Anything, userID, mock.Anything).Return(model.UserQuota{}, apperrors.ErrNotFound)
	h.adminUsers.EXPECT().ResetUserQuota(mock.Anything, userID).Return(model.UserQuota{}, apperrors.ErrNotFound)
	noDetails := func(details string) bool { return details == "{}" }
	h.audit.EXPECT().RecordAuditEvent(mock.Anything, auditedQuotaEvent("user.quota_set", userID, "failure", noDetails)).Return(nil).Once()
	h.audit.EXPECT().RecordAuditEvent(mock.Anything, auditedQuotaEvent("user.quota_reset", userID, "failure", noDetails)).Return(nil).Once()

	resp := perform(h.router, http.MethodPut, "/users/"+userID+"/quota", `{"quota_cpu":1,"quota_ram_mb":1,"quota_disk_mb":1}`, nil)
	require.Equal(t, http.StatusNotFound, resp.Code)
	resp = perform(h.router, http.MethodDelete, "/users/"+userID+"/quota", ``, nil)
	require.Equal(t, http.StatusNotFound, resp.Code)
}

// A rejected quota is a bad request whose answer never repeats the value.
func TestSetUserQuotaRejectsANonPositiveValueWithoutEchoingIt(t *testing.T) {
	h := newUserHandlerHarness(t)
	userID := uuid.NewString()
	for name, body := range map[string]string{
		"negative cpu":    `{"quota_cpu":-31.5,"quota_ram_mb":1024,"quota_disk_mb":1024}`,
		"zero cpu":        `{"quota_cpu":0,"quota_ram_mb":1024,"quota_disk_mb":1024}`,
		"negative memory": `{"quota_cpu":1,"quota_ram_mb":-315,"quota_disk_mb":1024}`,
		"negative disk":   `{"quota_cpu":1,"quota_ram_mb":1024,"quota_disk_mb":-3150}`,
		"missing disk":    `{"quota_cpu":1,"quota_ram_mb":1024}`,
		"not a number":    `{"quota_cpu":"315x","quota_ram_mb":1024,"quota_disk_mb":1024}`,
	} {
		t.Run(name, func(t *testing.T) {
			resp := perform(h.router, http.MethodPut, "/users/"+userID+"/quota", body, nil)

			require.Equal(t, http.StatusBadRequest, resp.Code)
			require.NotContains(t, resp.Body.String(), "315")
		})
	}
	resp := perform(h.router, http.MethodPut, "/users/not-a-uuid/quota", `{"quota_cpu":1,"quota_ram_mb":1,"quota_disk_mb":1}`, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestTelemetryTicketHandlerIssuesUserAndAdminTickets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := gatewayhandlermocks.NewMockTelemetryTicketIssuer(t)
	h := handler.NewTelemetryTicketHandler(store)
	router := gin.New()
	router.POST("/containers/:id/logs/stream/ticket", withScope(accessscope.WithUserScope(context.Background(), uuid.New(), "alice", "user")), h.IssueLogsTicket(false))
	router.POST("/admin/containers/:id/terminal/ticket", withScope(accessscope.WithAdminScope(context.Background(), uuid.New(), "admin", "admin")), h.IssueTerminalTicket(true))
	containerID := uuid.NewString()
	expiresAt := time.Unix(123, 0)

	store.EXPECT().Issue(mock.Anything, mock.MatchedBy(func(claims model.TelemetryTicketClaims) bool {
		return claims.ContainerID == containerID && claims.StreamType == model.TelemetryStreamLogs && !claims.AdminRoute && claims.Scope.Kind == accessscope.KindUser
	})).Return(model.TelemetryTicket{Ticket: "ticket-1", ExpiresAt: expiresAt}, nil)
	resp := perform(router, http.MethodPost, "/containers/"+containerID+"/logs/stream/ticket", ``, nil)
	require.Equal(t, http.StatusCreated, resp.Code)
	require.Contains(t, resp.Body.String(), `"ticket":"ticket-1"`)

	store.EXPECT().Issue(mock.Anything, mock.MatchedBy(func(claims model.TelemetryTicketClaims) bool {
		return claims.ContainerID == containerID && claims.StreamType == model.TelemetryStreamTerminal && claims.AdminRoute && claims.Scope.Kind == accessscope.KindAdmin
	})).Return(model.TelemetryTicket{Ticket: "ticket-2", ExpiresAt: expiresAt}, nil)
	resp = perform(router, http.MethodPost, "/admin/containers/"+containerID+"/terminal/ticket", ``, nil)
	require.Equal(t, http.StatusCreated, resp.Code)
	require.Contains(t, resp.Body.String(), `"ticket":"ticket-2"`)
}

func TestTelemetryTicketHandlerRejectsMissingScopeInvalidIDAndStoreError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := gatewayhandlermocks.NewMockTelemetryTicketIssuer(t)
	h := handler.NewTelemetryTicketHandler(store)
	router := gin.New()
	router.POST("/containers/:id/logs/stream/ticket", h.IssueLogsTicket(false))
	router.POST("/scoped/containers/:id/logs/stream/ticket", withScope(accessscope.WithUserScope(context.Background(), uuid.New(), "alice", "user")), h.IssueLogsTicket(false))
	containerID := uuid.NewString()

	resp := perform(router, http.MethodPost, "/containers/not-a-uuid/logs/stream/ticket", ``, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	resp = perform(router, http.MethodPost, "/containers/"+containerID+"/logs/stream/ticket", ``, nil)
	require.Equal(t, http.StatusUnauthorized, resp.Code)

	store.EXPECT().Issue(mock.Anything, mock.AnythingOfType("model.TelemetryTicketClaims")).Return(model.TelemetryTicket{}, apperrors.ErrUnavailable)
	resp = perform(router, http.MethodPost, "/scoped/containers/"+containerID+"/logs/stream/ticket", ``, nil)
	require.Equal(t, http.StatusServiceUnavailable, resp.Code)
}

func withScope(ctx context.Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
