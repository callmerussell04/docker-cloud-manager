package http

import (
	"context"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/internal/telemetry/model"
)

func TestRouterRequiresInternalToken(t *testing.T) {
	router := NewRouter(NewHandler(&telemetryFake{}, nil), "internal-token", nil)
	req := httptest.NewRequest(nethttp.MethodGet, "/api/v1/containers/00000000-0000-0000-0000-000000000001/logs/stream", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, nethttp.StatusUnauthorized)
	}
}

func TestStreamLogsWritesSSEHeaders(t *testing.T) {
	router := NewRouter(NewHandler(&telemetryFake{}, nil), "internal-token", nil)
	req := httptest.NewRequest(nethttp.MethodGet, "/api/v1/containers/00000000-0000-0000-0000-000000000001/logs/stream", nil)
	req.Header.Set(internalauth.HeaderName, "internal-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, nethttp.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if !strings.Contains(rec.Body.String(), "event: log") {
		t.Fatalf("SSE body missing log event: %q", rec.Body.String())
	}
}

func TestTerminalRequiresWebSocketUpgrade(t *testing.T) {
	router := NewRouter(NewHandler(&telemetryFake{}, nil), "internal-token", nil)
	req := httptest.NewRequest(nethttp.MethodGet, "/api/v1/containers/00000000-0000-0000-0000-000000000001/terminal", nil)
	req.Header.Set(internalauth.HeaderName, "internal-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, nethttp.StatusBadRequest)
	}
}

type telemetryFake struct{}

func (f *telemetryFake) StreamLogs(ctx context.Context, containerID string, opts model.LogOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("log line\n")), nil
}

func (f *telemetryFake) OpenTerminal(ctx context.Context, containerID string, opts model.TerminalOptions) (model.TerminalSession, model.RuntimeConfig, error) {
	return nil, model.RuntimeConfig{}, nil
}
