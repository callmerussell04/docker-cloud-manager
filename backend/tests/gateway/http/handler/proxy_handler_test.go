package handler_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	gatewayhandlermocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/gateway/http/handler"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCoreProxyStripsSpoofableIdentityHeadersAndForwardsInternalScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	var upstream *http.Request
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		upstream = req.Clone(req.Context())
		upstream.Body = req.Body
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		return proxyResponse(http.StatusCreated, `{"path":"`+req.URL.Path+`","body":"`+string(body)+`"}`), nil
	})
	proxy, err := handler.NewCoreProxyHandler(handler.ProxyOptions{
		TargetURL:     "http://core.internal",
		InternalToken: "internal-token",
		Transport:     transport,
	})
	require.NoError(t, err)
	router := gin.New()
	router.POST("/images/build", withScope(accessscope.WithUserScope(context.Background(), userID, "alice", "user")), proxy)

	req := httptestRequest(http.MethodPost, "/images/build?x=1", "payload")
	req.Header.Set("Authorization", "Bearer attacker")
	req.Header.Set("Cookie", "refresh=attacker")
	req.Header.Set(internalauth.HeaderScope, "admin")
	req.Header.Set(internalauth.HeaderUserID, uuid.NewString())
	req.Header.Set("X-Forwarded-User", "mallory")
	rec := performRequest(router, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.NotNil(t, upstream)
	require.Equal(t, "/images/build", upstream.URL.Path)
	require.Equal(t, "x=1", upstream.URL.RawQuery)
	require.Equal(t, "internal-token", upstream.Header.Get(internalauth.HeaderName))
	require.Equal(t, string(accessscope.KindUser), upstream.Header.Get(internalauth.HeaderScope))
	require.Equal(t, userID.String(), upstream.Header.Get(internalauth.HeaderUserID))
	require.Empty(t, upstream.Header.Get("Authorization"))
	require.Empty(t, upstream.Header.Get("Cookie"))
	require.Empty(t, upstream.Header.Get("X-Forwarded-User"))
}

func TestTelemetryLogsProxyConsumesTicketOnceAndRemovesTicketFromUpstreamQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	containerID := uuid.NewString()
	tickets := gatewayhandlermocks.NewMockTelemetryTicketConsumer(t)
	var upstream *http.Request
	proxy, err := handler.NewTelemetryLogsProxyHandler(handler.ProxyOptions{
		TargetURL:     "http://telemetry.internal",
		InternalToken: "internal-token",
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			upstream = req.Clone(req.Context())
			return proxyResponse(http.StatusOK, `stream`), nil
		}),
	}, handler.TelemetryProxyAuthOptions{Tickets: tickets, StreamType: model.TelemetryStreamLogs})
	require.NoError(t, err)
	router := gin.New()
	router.GET("/containers/:id/logs/stream", proxy)

	tickets.EXPECT().
		Consume(mock.Anything, "ticket-value", containerID, model.TelemetryStreamLogs, false).
		Return(model.TelemetryTicketClaims{
			ContainerID: containerID,
			StreamType:  model.TelemetryStreamLogs,
			Scope:       accessscope.Scope{Kind: accessscope.KindUser, UserID: userID, Username: "alice", Role: "user"},
		}, nil)
	resp := performProxy(router, http.MethodGet, "/containers/"+containerID+"/logs/stream?ticket=ticket-value&tail=10&follow=true", "")

	require.Equal(t, http.StatusOK, resp.Code)
	require.NotNil(t, upstream)
	require.Equal(t, "follow=true&tail=10", upstream.URL.RawQuery)
	require.Equal(t, "internal-token", upstream.Header.Get(internalauth.HeaderName))
	require.Equal(t, string(accessscope.KindUser), upstream.Header.Get(internalauth.HeaderScope))
	require.Equal(t, userID.String(), upstream.Header.Get(internalauth.HeaderUserID))
}

func TestTelemetryProxyRejectsMissingTicketAndInvalidWebSocketOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tickets := gatewayhandlermocks.NewMockTelemetryTicketConsumer(t)
	logsProxy, err := handler.NewTelemetryLogsProxyHandler(handler.ProxyOptions{
		TargetURL: "http://telemetry.internal",
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatal("upstream must not be called")
			return nil, nil
		}),
	}, handler.TelemetryProxyAuthOptions{Tickets: tickets, StreamType: model.TelemetryStreamLogs})
	require.NoError(t, err)
	terminalProxy, err := handler.NewTelemetryTerminalProxyHandler(handler.ProxyOptions{
		TargetURL:      "http://telemetry.internal",
		AllowedOrigins: []string{"http://frontend.example"},
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatal("upstream must not be called")
			return nil, nil
		}),
	}, handler.TelemetryProxyAuthOptions{Tickets: tickets, StreamType: model.TelemetryStreamTerminal})
	require.NoError(t, err)
	router := gin.New()
	router.GET("/containers/:id/logs/stream", logsProxy)
	router.GET("/containers/:id/terminal", terminalProxy)
	containerID := uuid.NewString()

	resp := performProxy(router, http.MethodGet, "/containers/"+containerID+"/logs/stream", "")
	require.Equal(t, http.StatusUnauthorized, resp.Code)

	req := httptestRequest(http.MethodGet, "/containers/"+containerID+"/terminal?ticket=ticket-value", "")
	req.Header.Set("Origin", "http://evil.example")
	resp = performRequest(router, req)
	require.Equal(t, http.StatusForbidden, resp.Code)
}

func TestTelemetryTerminalProxyAllowsConfiguredOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	containerID := uuid.NewString()
	tickets := gatewayhandlermocks.NewMockTelemetryTicketConsumer(t)
	called := false
	proxy, err := handler.NewTelemetryTerminalProxyHandler(handler.ProxyOptions{
		TargetURL:      "http://telemetry.internal",
		InternalToken:  "internal-token",
		AllowedOrigins: []string{"http://frontend.example"},
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			require.Equal(t, "cmd=%2Fbin%2Fsh&cols=80&rows=24", req.URL.RawQuery)
			return proxyResponse(http.StatusOK, "terminal"), nil
		}),
	}, handler.TelemetryProxyAuthOptions{Tickets: tickets, StreamType: model.TelemetryStreamTerminal})
	require.NoError(t, err)
	router := gin.New()
	router.GET("/containers/:id/terminal", proxy)
	tickets.EXPECT().
		Consume(mock.Anything, "ticket-value", containerID, model.TelemetryStreamTerminal, false).
		Return(model.TelemetryTicketClaims{
			ContainerID: containerID,
			StreamType:  model.TelemetryStreamTerminal,
			Scope:       accessscope.Scope{Kind: accessscope.KindUser, UserID: uuid.New(), Username: "alice", Role: "user"},
		}, nil)

	req := httptestRequest(http.MethodGet, "/containers/"+containerID+"/terminal?ticket=ticket-value&cmd=/bin/sh&rows=24&cols=80", "")
	req.Header.Set("Origin", "http://frontend.example")
	resp := performRequest(router, req)

	require.Equal(t, http.StatusOK, resp.Code)
	require.True(t, called)
}

func TestProxyMapsUpstreamTimeoutToGatewayTimeoutJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	proxy, err := handler.NewCoreProxyHandler(handler.ProxyOptions{
		TargetURL: "http://core.internal",
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		}),
	})
	require.NoError(t, err)
	router := gin.New()
	router.GET("/builds/:id/logs", proxy)

	resp := performProxy(router, http.MethodGet, "/builds/"+uuid.NewString()+"/logs", "")
	require.Equal(t, http.StatusGatewayTimeout, resp.Code)
	require.Contains(t, resp.Body.String(), `"error_code":"timeout"`)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func proxyResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func httptestRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func performRequest(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	base := httptest.NewRecorder()
	rec := &closeNotifyRecorder{ResponseRecorder: base, closed: make(chan bool, 1)}
	router.ServeHTTP(rec, req)
	return base
}

func performProxy(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	return performRequest(router, httptestRequest(method, path, body))
}

type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	closed chan bool
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool {
	return r.closed
}
