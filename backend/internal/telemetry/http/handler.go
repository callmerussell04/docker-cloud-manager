package http

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/telemetry/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/telemetry/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type TelemetryService interface {
	StreamLogs(ctx context.Context, containerID string, opts model.LogOptions) (io.ReadCloser, error)
	OpenTerminal(ctx context.Context, containerID string, opts model.TerminalOptions) (model.TerminalSession, model.RuntimeConfig, error)
}

type Handler struct {
	service  TelemetryService
	upgrader websocket.Upgrader
	logger   *slog.Logger
}

func NewHandler(service TelemetryService, logger *slog.Logger) *Handler {
	return &Handler{
		service: service,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
		logger: logging.WithComponent(logger, "telemetry_http"),
	}
}

func (h *Handler) StreamLogs(c *gin.Context) {
	containerID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	opts, ok := logOptions(c)
	if !ok {
		return
	}

	stream, err := h.service.StreamLogs(c.Request.Context(), containerID, opts)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	defer stream.Close()

	w := c.Writer
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpresponse.Respond(c, http.StatusInternalServerError, apperrors.ErrInternal)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	h.writeLogEvents(c.Request.Context(), w, flusher, stream)
}

func (h *Handler) OpenTerminal(c *gin.Context) {
	containerID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	opts, ok := terminalOptions(c)
	if !ok {
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	session, cfg, err := h.service.OpenTerminal(c.Request.Context(), containerID, opts)
	if err != nil {
		_ = conn.WriteJSON(wsEvent{Type: "error", Error: apperrors.SafeMessage(err)})
		return
	}
	defer session.Close()

	sessionID := uuid.NewString()
	started := time.Now()
	reason := h.bridgeTerminal(c.Request.Context(), conn, session, cfg)
	h.logger.InfoContext(c.Request.Context(), "terminal session closed", service.AuditAttrs(c.Request.Context(), containerID, sessionID, started, reason)...)
}

func (h *Handler) writeLogEvents(ctx context.Context, w gin.ResponseWriter, flusher http.Flusher, stream io.Reader) {
	lines := make(chan string)
	errCh := make(chan error, 1)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
		errCh <- scanner.Err()
	}()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			_, _ = io.WriteString(w, ": heartbeat\n\n")
			flusher.Flush()
		case line, ok := <-lines:
			if !ok {
				if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
					writeSSE(w, "error", apperrors.SafeMessage(apperrors.ErrInternal))
				}
				writeSSE(w, "end", "")
				flusher.Flush()
				return
			}
			writeSSE(w, "log", line)
			flusher.Flush()
		}
	}
}

func (h *Handler) bridgeTerminal(ctx context.Context, conn *websocket.Conn, session model.TerminalSession, cfg model.RuntimeConfig) string {
	if cfg.WSReadLimitBytes > 0 {
		conn.SetReadLimit(cfg.WSReadLimitBytes)
	}

	sessionCtx, cancel := context.WithTimeout(ctx, cfg.TerminalMaxDuration)
	defer cancel()

	var writeMu sync.Mutex
	writeJSON := func(event wsEvent) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.WriteJSON(event)
	}
	writeBinary := func(data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.BinaryMessage, data)
	}

	writeJSON(wsEvent{Type: "ready"})
	inputDone := make(chan string, 1)
	outputDone := make(chan string, 1)

	go func() {
		inputDone <- readTerminalInput(sessionCtx, conn, session, cfg, writeJSON)
	}()
	go func() {
		outputDone <- writeTerminalOutput(sessionCtx, session, writeBinary)
	}()

	select {
	case reason := <-inputDone:
		return reason
	case reason := <-outputDone:
		state, err := session.Inspect(context.Background())
		if err == nil && !state.Running {
			writeJSON(wsEvent{Type: "exit", ExitCode: &state.ExitCode})
		}
		return reason
	case <-sessionCtx.Done():
		if errors.Is(sessionCtx.Err(), context.DeadlineExceeded) {
			writeJSON(wsEvent{Type: "error", Error: "terminal session timed out"})
			return "timeout"
		}
		return "context_cancelled"
	}
}

func readTerminalInput(ctx context.Context, conn *websocket.Conn, session model.TerminalSession, cfg model.RuntimeConfig, writeJSON func(wsEvent)) string {
	idle := cfg.TerminalIdleTimeout
	if idle <= 0 {
		idle = 5 * time.Minute
	}
	_ = conn.SetReadDeadline(time.Now().Add(idle))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(idle))
	})

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return "client_disconnect"
		}
		_ = conn.SetReadDeadline(time.Now().Add(idle))
		select {
		case <-ctx.Done():
			return "context_cancelled"
		default:
		}

		switch messageType {
		case websocket.BinaryMessage:
			if _, err := session.Write(payload); err != nil {
				return "stdin_error"
			}
		case websocket.TextMessage:
			reason, done := handleTerminalControl(ctx, session, payload, writeJSON)
			if done {
				return reason
			}
		case websocket.CloseMessage:
			return "client_close"
		}
	}
}

func writeTerminalOutput(ctx context.Context, session model.TerminalSession, writeBinary func([]byte) error) string {
	buf := make([]byte, 32*1024)
	for {
		n, err := session.Read(buf)
		if n > 0 {
			if writeErr := writeBinary(buf[:n]); writeErr != nil {
				return "client_disconnect"
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "process_exit"
			}
			select {
			case <-ctx.Done():
				return "context_cancelled"
			default:
				return "stream_error"
			}
		}
	}
}

func handleTerminalControl(ctx context.Context, session model.TerminalSession, payload []byte, writeJSON func(wsEvent)) (string, bool) {
	var msg terminalControl
	if err := json.Unmarshal(payload, &msg); err != nil {
		writeJSON(wsEvent{Type: "error", Error: "invalid terminal control message"})
		return "", false
	}
	switch msg.Type {
	case "resize":
		if msg.Rows == 0 || msg.Cols == 0 {
			writeJSON(wsEvent{Type: "error", Error: "invalid terminal size"})
			return "", false
		}
		if err := session.Resize(ctx, msg.Rows, msg.Cols); err != nil {
			writeJSON(wsEvent{Type: "error", Error: apperrors.SafeMessage(err)})
		}
		return "", false
	case "close":
		_ = session.CloseWrite()
		return "client_close", true
	default:
		writeJSON(wsEvent{Type: "error", Error: "unknown terminal control message"})
		return "", false
	}
}

func writeSSE(w io.Writer, event, data string) {
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	if data != "" {
		data = strings.ReplaceAll(data, "\r\n", "\n")
		data = strings.ReplaceAll(data, "\r", "\n")
		for _, line := range strings.Split(data, "\n") {
			_, _ = fmt.Fprintf(w, "data: %s\n", line)
		}
	}
	_, _ = io.WriteString(w, "\n")
}

func pathUUID(c *gin.Context, name string) (string, bool) {
	value := c.Param(name)
	if _, err := uuid.Parse(value); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return "", false
	}
	return value, true
}

func logOptions(c *gin.Context) (model.LogOptions, bool) {
	opts := model.LogOptions{
		Tail:       200,
		Follow:     true,
		Timestamps: true,
	}
	if raw := c.Query("tail"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
			return model.LogOptions{}, false
		}
		opts.Tail = value
	}
	if raw := c.Query("follow"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
			return model.LogOptions{}, false
		}
		opts.Follow = value
	}
	if raw := c.Query("timestamps"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
			return model.LogOptions{}, false
		}
		opts.Timestamps = value
	}
	if raw := c.Query("since"); raw != "" {
		since, err := parseSince(raw)
		if err != nil {
			httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
			return model.LogOptions{}, false
		}
		opts.Since = since
	}
	return opts, true
}

func parseSince(raw string) (time.Time, error) {
	if unix, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Unix(unix, 0), nil
	}
	return time.Parse(time.RFC3339, raw)
}

func terminalOptions(c *gin.Context) (model.TerminalOptions, bool) {
	opts := model.TerminalOptions{
		Rows: 24,
		Cols: 80,
	}
	cmd := c.Query("cmd")
	args := c.QueryArray("arg")
	if cmd == "" && len(args) > 0 {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return model.TerminalOptions{}, false
	}
	if cmd != "" {
		opts.Command = append([]string{cmd}, args...)
	}
	if raw := c.Query("rows"); raw != "" {
		value, err := service.ParseSize(raw)
		if err != nil {
			httpresponse.Respond(c, http.StatusBadRequest, err)
			return model.TerminalOptions{}, false
		}
		opts.Rows = value
	}
	if raw := c.Query("cols"); raw != "" {
		value, err := service.ParseSize(raw)
		if err != nil {
			httpresponse.Respond(c, http.StatusBadRequest, err)
			return model.TerminalOptions{}, false
		}
		opts.Cols = value
	}
	return opts, true
}

type terminalControl struct {
	Type string `json:"type"`
	Rows uint   `json:"rows"`
	Cols uint   `json:"cols"`
}

type wsEvent struct {
	Type     string `json:"type"`
	Error    string `json:"error,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
}
