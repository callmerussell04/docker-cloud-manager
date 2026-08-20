package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	gatewayhandlermocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/gateway/http/handler"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestUserManagementHandlerCRUDAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := gatewayhandlermocks.NewMockUserManagementService(t)
	h := handler.NewUserManagementHandler(svc)
	router := gin.New()
	router.GET("/users", h.ListUsers)
	router.GET("/users/:id", h.GetUser)
	router.POST("/users", h.CreateUser)
	router.PUT("/users/:id", h.UpdateUser)
	router.DELETE("/users/:id", h.DeactivateUser)
	router.POST("/users/:id/activate", h.ReactivateUser)
	userID := uuid.NewString()
	user := model.AdminUser{UserID: userID, Username: "alice", Email: "alice@example.com", Role: "user", Status: "active", QuotaCPU: 1, QuotaRAMMB: 1024, QuotaDiskMB: 2048}

	svc.EXPECT().ListUsers(mock.Anything, 2, 10).Return(model.PaginatedAdminUsers{Users: []model.AdminUser{user}, TotalCount: 1}, nil)
	resp := perform(router, http.MethodGet, "/users?page=2&limit=10", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"total_count":1`)

	svc.EXPECT().GetUser(mock.Anything, userID).Return(user, nil)
	resp = perform(router, http.MethodGet, "/users/"+userID, ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"username":"alice"`)

	svc.EXPECT().CreateUser(mock.Anything, mock.MatchedBy(func(input model.CreateUserInput) bool {
		return input.Username == "bob" && input.Role == "user" && input.QuotaDiskMB == 2048
	})).Return(user, nil)
	resp = perform(router, http.MethodPost, "/users", `{"username":"bob","email":"bob@example.com","password":"secret","role":"user","status":"active","quota_cpu":1,"quota_ram_mb":1024,"quota_disk_mb":2048}`, nil)
	require.Equal(t, http.StatusCreated, resp.Code)

	svc.EXPECT().UpdateUser(mock.Anything, userID, mock.MatchedBy(func(input model.UpdateUserInput) bool {
		return input.Username == "alice2" && input.Password == "newsecret"
	})).Return(user, nil)
	resp = perform(router, http.MethodPut, "/users/"+userID, `{"username":"alice2","email":"alice2@example.com","password":"newsecret","role":"admin","status":"active","quota_cpu":2,"quota_ram_mb":2048,"quota_disk_mb":4096}`, nil)
	require.Equal(t, http.StatusOK, resp.Code)

	svc.EXPECT().DeactivateUser(mock.Anything, userID).Return(user, nil)
	resp = perform(router, http.MethodDelete, "/users/"+userID, ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	svc.EXPECT().ReactivateUser(mock.Anything, userID).Return(user, nil)
	resp = perform(router, http.MethodPost, "/users/"+userID+"/activate", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)

	resp = perform(router, http.MethodGet, "/users/bad-id", ``, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	resp = perform(router, http.MethodPost, "/users", `{"username":"bad name","role":"user","status":"active"}`, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	resp = perform(router, http.MethodPost, "/users", `{"username":"alice","role":"owner","status":"active"}`, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	resp = perform(router, http.MethodPost, "/users", `{"username":"alice","role":"user","status":"disabled"}`, nil)
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
