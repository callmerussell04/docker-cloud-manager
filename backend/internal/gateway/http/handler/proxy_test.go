package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestTelemetryProxyConsumesTicketAndSetsInternalScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	containerID := uuid.NewString()
	consumer := &ticketConsumerFake{
		claims: model.TelemetryTicketClaims{
			ContainerID: containerID,
			StreamType:  model.TelemetryStreamTerminal,
			Scope: accessscope.Scope{
				Kind:   accessscope.KindUser,
				UserID: uuid.New(),
			},
		},
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/containers/"+containerID+"/terminal?ticket=ticket-value", nil)
	ctx.Request = req
	ctx.Params = gin.Params{{Key: "id", Value: containerID}}

	if !authenticateTelemetryProxy(ctx, TelemetryProxyAuthOptions{
		Tickets:    consumer,
		StreamType: model.TelemetryStreamTerminal,
	}, containerID) {
		t.Fatal("authenticateTelemetryProxy() returned false")
	}
	if !consumer.consumed {
		t.Fatal("ticket was not consumed")
	}
	if got := ctx.Request.URL.Query().Get("ticket"); got != "" {
		t.Fatalf("ticket query after auth = %q, want empty", got)
	}
	prepareProxyRequest(ctx, "internal-token")
	if got := ctx.Request.Header.Get(internalauth.HeaderName); got != "internal-token" {
		t.Fatalf("internal token header = %q", got)
	}
	if got := ctx.Request.Header.Get(internalauth.HeaderScope); got != string(accessscope.KindUser) {
		t.Fatalf("scope header = %q", got)
	}
}

func TestTelemetryProxyWithoutTicketRequiresBearerAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	containerID := uuid.NewString()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/containers/"+containerID+"/logs/stream", nil)
	ctx.Params = gin.Params{{Key: "id", Value: containerID}}

	ok := authenticateTelemetryProxy(ctx, TelemetryProxyAuthOptions{
		Verifier:   &authVerifierFake{err: apperrors.ErrUnauthorized},
		StreamType: model.TelemetryStreamLogs,
	}, containerID)
	if ok {
		t.Fatal("authenticateTelemetryProxy() returned true")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

type ticketConsumerFake struct {
	claims   model.TelemetryTicketClaims
	consumed bool
}

func (f *ticketConsumerFake) Consume(ctx context.Context, token, expectedContainerID, expectedStreamType string, expectedAdminRoute bool) (model.TelemetryTicketClaims, error) {
	f.consumed = true
	if token != "ticket-value" || expectedContainerID != f.claims.ContainerID || expectedStreamType != f.claims.StreamType {
		return model.TelemetryTicketClaims{}, apperrors.ErrForbidden
	}
	return f.claims, nil
}

type authVerifierFake struct {
	err error
}

func (f *authVerifierFake) VerifyAccessToken(ctx context.Context, authHeader string) (model.AuthUser, error) {
	if f.err != nil {
		return model.AuthUser{}, f.err
	}
	return model.AuthUser{UserID: uuid.NewString(), Username: "alice", Role: "user"}, nil
}

func (f *authVerifierFake) CheckPermission(ctx context.Context, authHeader, permission string) (model.AuthUser, error) {
	if f.err != nil {
		return model.AuthUser{}, f.err
	}
	return model.AuthUser{UserID: uuid.NewString(), Username: "admin", Role: "admin"}, nil
}
