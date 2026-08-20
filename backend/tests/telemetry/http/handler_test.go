package http_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	telemetryhttp "github.com/callmerussell04/docker-cloud-manager/internal/telemetry/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/telemetry/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	telemetryhttpmocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/telemetry/http"
	telemetrymodelmocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/telemetry/model"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/test/bufconn"
)

const (
	internalToken = "internal-token"
	containerID   = "00000000-0000-0000-0000-000000000001"
	userID        = "11111111-1111-1111-1111-111111111111"
)

func TestRouterRequiresInternalTokenAndRegistersRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := telemetryhttpmocks.NewTelemetryService(t)
	router := newRouter(service)

	routes := map[string]struct{}{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}
	require.Contains(t, routes, "GET /api/v1/containers/:id/logs/stream")
	require.Contains(t, routes, "GET /api/v1/containers/:id/terminal")
	require.Contains(t, routes, "GET /api/v1/admin/containers/:id/logs/stream")
	require.Contains(t, routes, "GET /api/v1/admin/containers/:id/terminal")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/containers/"+containerID+"/logs/stream", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestRouterRestoresScopeFromInternalHeaders(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		headers map[string]string
		want    accessscope.Kind
	}{
		{
			name: "user scope",
			path: "/api/v1/containers/" + containerID + "/logs/stream",
			headers: map[string]string{
				internalauth.HeaderScope:    string(accessscope.KindUser),
				internalauth.HeaderUserID:   userID,
				internalauth.HeaderUsername: "alice",
				internalauth.HeaderRole:     "user",
			},
			want: accessscope.KindUser,
		},
		{
			name: "admin scope",
			path: "/api/v1/admin/containers/" + containerID + "/logs/stream",
			headers: map[string]string{
				internalauth.HeaderScope:    string(accessscope.KindAdmin),
				internalauth.HeaderUserID:   userID,
				internalauth.HeaderUsername: "admin",
				internalauth.HeaderRole:     "admin",
			},
			want: accessscope.KindAdmin,
		},
		{
			name:    "system default",
			path:    "/api/v1/containers/" + containerID + "/logs/stream",
			headers: map[string]string{},
			want:    accessscope.KindSystem,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := telemetryhttpmocks.NewTelemetryService(t)
			router := newRouter(service)
			service.EXPECT().
				StreamLogs(mock.Anything, containerID, mock.Anything).
				Run(func(ctx context.Context, _ string, _ model.LogOptions) {
					scope, ok := accessscope.FromContext(ctx)
					require.True(t, ok)
					require.Equal(t, tt.want, scope.Kind)
				}).
				Return(io.NopCloser(strings.NewReader("line\n")), nil).
				Once()

			resp := httpRequest(router, http.MethodGet, tt.path, tt.headers)
			require.Equal(t, http.StatusOK, resp.Code)
		})
	}
}

func TestStreamLogsValidatesQueryAndWritesSSE(t *testing.T) {
	t.Run("valid query writes SSE", func(t *testing.T) {
		service := telemetryhttpmocks.NewTelemetryService(t)
		router := newRouter(service)
		since := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

		service.EXPECT().
			StreamLogs(mock.Anything, containerID, mock.MatchedBy(func(opts model.LogOptions) bool {
				return opts.Tail == 10 && !opts.Follow && !opts.Timestamps && opts.Since.Equal(since)
			})).
			Return(io.NopCloser(strings.NewReader("line one\nline two\n")), nil).
			Once()

		path := "/api/v1/containers/" + containerID + "/logs/stream?tail=10&follow=false&timestamps=false&since=" + since.Format(time.RFC3339)
		resp := httpRequest(router, http.MethodGet, path, scopeHeaders())
		require.Equal(t, http.StatusOK, resp.Code)
		require.Equal(t, "text/event-stream", resp.Header().Get("Content-Type"))
		require.Contains(t, resp.Body.String(), "event: log\n")
		require.Contains(t, resp.Body.String(), "data: line one\n")
		require.Contains(t, resp.Body.String(), "data: line two\n")
		require.Contains(t, resp.Body.String(), "event: end\n")
	})

	t.Run("unix since is accepted", func(t *testing.T) {
		service := telemetryhttpmocks.NewTelemetryService(t)
		router := newRouter(service)
		service.EXPECT().
			StreamLogs(mock.Anything, containerID, mock.MatchedBy(func(opts model.LogOptions) bool {
				return opts.Since.Equal(time.Unix(42, 0))
			})).
			Return(io.NopCloser(strings.NewReader("")), nil).
			Once()

		resp := httpRequest(router, http.MethodGet, "/api/v1/containers/"+containerID+"/logs/stream?since=42", scopeHeaders())
		require.Equal(t, http.StatusOK, resp.Code)
	})

	t.Run("service error maps to safe JSON", func(t *testing.T) {
		service := telemetryhttpmocks.NewTelemetryService(t)
		router := newRouter(service)
		service.EXPECT().StreamLogs(mock.Anything, containerID, mock.Anything).Return(nil, apperrors.ErrUnavailable).Once()

		resp := httpRequest(router, http.MethodGet, "/api/v1/containers/"+containerID+"/logs/stream", scopeHeaders())
		require.Equal(t, http.StatusServiceUnavailable, resp.Code)
		require.Contains(t, resp.Body.String(), `"error_code":"unavailable"`)
	})

	invalid := []string{
		"/api/v1/containers/not-a-uuid/logs/stream",
		"/api/v1/containers/" + containerID + "/logs/stream?tail=0",
		"/api/v1/containers/" + containerID + "/logs/stream?tail=bad",
		"/api/v1/containers/" + containerID + "/logs/stream?follow=bad",
		"/api/v1/containers/" + containerID + "/logs/stream?timestamps=bad",
		"/api/v1/containers/" + containerID + "/logs/stream?since=not-time",
	}
	for _, path := range invalid {
		t.Run(path, func(t *testing.T) {
			service := telemetryhttpmocks.NewTelemetryService(t)
			router := newRouter(service)
			resp := httpRequest(router, http.MethodGet, path, scopeHeaders())
			require.Equal(t, http.StatusBadRequest, resp.Code)
			service.AssertNotCalled(t, "StreamLogs", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestOpenTerminalValidatesBoundaryAndWritesServiceError(t *testing.T) {
	t.Run("requires websocket upgrade", func(t *testing.T) {
		service := telemetryhttpmocks.NewTelemetryService(t)
		router := newRouter(service)
		resp := httpRequest(router, http.MethodGet, "/api/v1/containers/"+containerID+"/terminal", scopeHeaders())
		require.Equal(t, http.StatusBadRequest, resp.Code)
	})

	invalid := []string{
		"/api/v1/containers/not-a-uuid/terminal",
		"/api/v1/containers/" + containerID + "/terminal?arg=-i",
		"/api/v1/containers/" + containerID + "/terminal?rows=bad",
		"/api/v1/containers/" + containerID + "/terminal?cols=bad",
	}
	for _, path := range invalid {
		t.Run(path, func(t *testing.T) {
			service := telemetryhttpmocks.NewTelemetryService(t)
			router := newRouter(service)
			resp := httpRequest(router, http.MethodGet, path, scopeHeaders())
			require.Equal(t, http.StatusBadRequest, resp.Code)
			service.AssertNotCalled(t, "OpenTerminal", mock.Anything, mock.Anything, mock.Anything)
		})
	}

	t.Run("service error is written to websocket", func(t *testing.T) {
		service := telemetryhttpmocks.NewTelemetryService(t)
		router := newRouter(service)
		server := newMemoryServer(t, router)

		service.EXPECT().
			OpenTerminal(mock.Anything, containerID, model.TerminalOptions{Rows: 30, Cols: 100, Command: []string{"/bin/sh", "-i"}}).
			Return(nil, model.RuntimeConfig{}, apperrors.ErrForbidden).
			Once()

		conn := dialTerminal(t, server, "/api/v1/containers/"+containerID+"/terminal?cmd=/bin/sh&arg=-i&rows=30&cols=100")
		defer conn.Close()

		var event wsEvent
		require.NoError(t, conn.ReadJSON(&event))
		require.Equal(t, "error", event.Type)
		require.NotEmpty(t, event.Error)
	})
}

func TestOpenTerminalWebSocketBridge(t *testing.T) {
	t.Run("binary input resize and close control", func(t *testing.T) {
		service := telemetryhttpmocks.NewTelemetryService(t)
		session := telemetrymodelmocks.NewTerminalSession(t)
		router := newRouter(service)
		server := newMemoryServer(t, router)
		releaseRead := make(chan struct{})
		writeDone := make(chan struct{})
		resizeDone := make(chan struct{})
		closeWriteDone := make(chan struct{})

		service.EXPECT().
			OpenTerminal(mock.Anything, containerID, model.TerminalOptions{Rows: 24, Cols: 80}).
			Return(session, model.RuntimeConfig{TerminalIdleTimeout: time.Second, TerminalMaxDuration: time.Second, WSReadLimitBytes: 64}, nil).
			Once()
		session.EXPECT().Read(mock.Anything).RunAndReturn(func([]byte) (int, error) {
			<-releaseRead
			return 0, io.EOF
		}).Maybe()
		session.EXPECT().Write(mock.MatchedBy(func(payload []byte) bool {
			return string(payload) == "input"
		})).Run(func([]byte) { close(writeDone) }).Return(5, nil).Once()
		session.EXPECT().Resize(mock.Anything, uint(40), uint(120)).Run(func(context.Context, uint, uint) {
			close(resizeDone)
		}).Return(nil).Once()
		session.EXPECT().CloseWrite().Run(func() { close(closeWriteDone) }).Return(nil).Once()
		session.EXPECT().Close().Run(func() { closeOnce(releaseRead) }).Return(nil).Maybe()

		conn := dialTerminal(t, server, "/api/v1/containers/"+containerID+"/terminal")
		defer conn.Close()
		requireReady(t, conn)

		require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("input")))
		require.NoError(t, conn.WriteJSON(map[string]any{"type": "resize", "rows": 40, "cols": 120}))
		require.NoError(t, conn.WriteJSON(map[string]any{"type": "close"}))
		requireSignal(t, writeDone)
		requireSignal(t, resizeDone)
		requireSignal(t, closeWriteDone)
	})

	t.Run("process exit sends exit event", func(t *testing.T) {
		service := telemetryhttpmocks.NewTelemetryService(t)
		session := telemetrymodelmocks.NewTerminalSession(t)
		router := newRouter(service)
		server := newMemoryServer(t, router)

		service.EXPECT().
			OpenTerminal(mock.Anything, containerID, model.TerminalOptions{Rows: 24, Cols: 80}).
			Return(session, model.RuntimeConfig{TerminalIdleTimeout: time.Second, TerminalMaxDuration: time.Second}, nil).
			Once()
		session.EXPECT().Read(mock.Anything).Return(0, io.EOF).Once()
		session.EXPECT().Inspect(mock.Anything).Return(model.ExecState{Running: false, ExitCode: 7}, nil).Once()
		session.EXPECT().Close().Return(nil).Maybe()

		conn := dialTerminal(t, server, "/api/v1/containers/"+containerID+"/terminal")
		defer conn.Close()
		requireReady(t, conn)

		var event wsEvent
		require.NoError(t, conn.ReadJSON(&event))
		require.Equal(t, "exit", event.Type)
		require.NotNil(t, event.ExitCode)
		require.Equal(t, 7, *event.ExitCode)
	})

	t.Run("timeout sends safe error", func(t *testing.T) {
		service := telemetryhttpmocks.NewTelemetryService(t)
		session := telemetrymodelmocks.NewTerminalSession(t)
		router := newRouter(service)
		server := newMemoryServer(t, router)
		releaseRead := make(chan struct{})

		service.EXPECT().
			OpenTerminal(mock.Anything, containerID, model.TerminalOptions{Rows: 24, Cols: 80}).
			Return(session, model.RuntimeConfig{TerminalIdleTimeout: time.Second, TerminalMaxDuration: 20 * time.Millisecond}, nil).
			Once()
		session.EXPECT().Read(mock.Anything).RunAndReturn(func([]byte) (int, error) {
			<-releaseRead
			return 0, io.EOF
		}).Maybe()
		session.EXPECT().Close().Run(func() { closeOnce(releaseRead) }).Return(nil).Maybe()

		conn := dialTerminal(t, server, "/api/v1/containers/"+containerID+"/terminal")
		defer conn.Close()
		requireReady(t, conn)

		var event wsEvent
		require.NoError(t, conn.ReadJSON(&event))
		require.Equal(t, "error", event.Type)
		require.Equal(t, "terminal session timed out", event.Error)
	})
}

func newRouter(service *telemetryhttpmocks.TelemetryService) *gin.Engine {
	handler := telemetryhttp.NewHandler(service, discardLogger())
	return telemetryhttp.NewRouter(handler, internalToken, discardLogger())
}

func httpRequest(router http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set(internalauth.HeaderName, internalToken)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func scopeHeaders() map[string]string {
	return map[string]string{
		internalauth.HeaderScope:    string(accessscope.KindUser),
		internalauth.HeaderUserID:   userID,
		internalauth.HeaderUsername: "alice",
		internalauth.HeaderRole:     "user",
	}
}

type memoryServer struct {
	listener *bufconn.Listener
	server   *http.Server
}

func newMemoryServer(t *testing.T, handler http.Handler) *memoryServer {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := &http.Server{Handler: handler}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})
	return &memoryServer{listener: listener, server: server}
}

func dialTerminal(t *testing.T, server *memoryServer, path string) *websocket.Conn {
	t.Helper()
	wsURL := "ws://bufnet" + path
	header := http.Header{}
	header.Set(internalauth.HeaderName, internalToken)
	for key, value := range scopeHeaders() {
		header.Set(key, value)
	}
	dialer := websocket.Dialer{
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return server.listener.DialContext(ctx)
		},
	}
	conn, resp, err := dialer.Dial(wsURL, header)
	if resp != nil {
		defer resp.Body.Close()
	}
	require.NoError(t, err)
	return conn
}

func requireReady(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	var event wsEvent
	require.NoError(t, conn.ReadJSON(&event))
	require.Equal(t, "ready", event.Type)
}

func requireSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for terminal bridge call")
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func closeOnce(ch chan struct{}) {
	defer func() { _ = recover() }()
	close(ch)
}

type wsEvent struct {
	Type     string `json:"type"`
	Error    string `json:"error,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
}
