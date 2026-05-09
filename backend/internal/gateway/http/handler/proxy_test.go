package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCoreProxyForwardsComposePathAndBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(r.URL.Path + "\n" + string(body) + "\n" + r.Header.Get(internalauth.HeaderName))),
			Request:    r,
		}, nil
	})

	proxy, err := NewCoreProxyHandler(ProxyOptions{
		TargetURL:     "http://core:8083",
		InternalToken: "internal-token",
		Transport:     transport,
	})
	if err != nil {
		t.Fatalf("NewCoreProxyHandler() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/compose", strings.NewReader("compose-body"))
	rec := newCloseNotifyRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = req

	proxy(ctx)

	if rec.ResponseRecorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.ResponseRecorder.Code, http.StatusAccepted)
	}
	got := rec.ResponseRecorder.Body.String()
	want := "/api/v1/projects/compose\ncompose-body\ninternal-token"
	if got != want {
		t.Fatalf("proxied payload = %q, want %q", got, want)
	}
}

func TestCoreProxyForwardsBuildPathAndBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(r.URL.Path + "\n" + string(body) + "\n" + r.Header.Get(internalauth.HeaderName))),
			Request:    r,
		}, nil
	})

	proxy, err := NewCoreProxyHandler(ProxyOptions{
		TargetURL:     "http://core:8083",
		InternalToken: "internal-token",
		Transport:     transport,
	})
	if err != nil {
		t.Fatalf("NewCoreProxyHandler() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/images/build/git", strings.NewReader("build-body"))
	rec := newCloseNotifyRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = req

	proxy(ctx)

	if rec.ResponseRecorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.ResponseRecorder.Code, http.StatusAccepted)
	}
	got := rec.ResponseRecorder.Body.String()
	want := "/api/v1/images/build/git\nbuild-body\ninternal-token"
	if got != want {
		t.Fatalf("proxied payload = %q, want %q", got, want)
	}
}

func TestProxyStripsSpoofableIdentityHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Cookie", "refresh=token")
	req.Header.Set(internalauth.HeaderName, "spoofed")
	req.Header.Set(internalauth.HeaderScope, "admin")
	req.Header.Set(internalauth.HeaderUserID, uuid.NewString())
	req.Header.Set("X-Forwarded-User", "alice")

	clearProxyIdentityHeaders(req.Header)

	for _, header := range []string{
		"Authorization",
		"Cookie",
		internalauth.HeaderName,
		internalauth.HeaderScope,
		internalauth.HeaderUserID,
		"X-Forwarded-User",
	} {
		if got := req.Header.Get(header); got != "" {
			t.Fatalf("%s header = %q, want empty", header, got)
		}
	}
}

func TestTelemetryProxyWithoutTicketIsUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	containerID := uuid.NewString()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/containers/"+containerID+"/logs/stream", nil)
	ctx.Request.Header.Set("Authorization", "Bearer token")
	ctx.Params = gin.Params{{Key: "id", Value: containerID}}

	ok := authenticateTelemetryProxy(ctx, TelemetryProxyAuthOptions{
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	closeCh chan bool
}

func newCloseNotifyRecorder() *closeNotifyRecorder {
	return &closeNotifyRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closeCh:          make(chan bool, 1),
	}
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool {
	return r.closeCh
}
