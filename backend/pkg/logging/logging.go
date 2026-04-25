package logging

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	RequestIDHeader = "X-Request-Id"
	RequestIDKey    = "request_id"
)

type contextKey string

const requestIDContextKey contextKey = "request_id"

type Config struct {
	Level  string
	Format string
	Env    string
}

func ConfigFromEnv() Config {
	return Config{
		Level:  getenv("LOG_LEVEL", "info"),
		Format: getenv("LOG_FORMAT", "json"),
		Env:    getenv("APP_ENV", "production"),
	}
}

func NewLogger(serviceName string, cfg Config) *slog.Logger {
	level := new(slog.LevelVar)
	level.Set(parseLevel(cfg.Level))

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	if strings.EqualFold(cfg.Format, "text") {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return slog.New(handler).With(
		slog.String("service", serviceName),
		slog.String("env", cfg.Env),
	)
}

func WithComponent(logger *slog.Logger, component string) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger.With(slog.String("component", component))
}

func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey, requestID)
}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requestID, _ := ctx.Value(requestIDContextKey).(string)
	return requestID
}

func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(RequestIDHeader)
		if requestID == "" {
			requestID = uuid.NewString()
		}

		c.Set(RequestIDKey, requestID)
		c.Header(RequestIDHeader, requestID)
		c.Request.Header.Set(RequestIDHeader, requestID)
		c.Request = c.Request.WithContext(ContextWithRequestID(c.Request.Context(), requestID))
		c.Next()
	}
}

func AccessLogMiddleware(logger *slog.Logger) gin.HandlerFunc {
	logger = WithComponent(logger, "http")

	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()

		statusCode := c.Writer.Status()
		if statusCode == 0 {
			statusCode = http.StatusOK
		}

		attrs := []slog.Attr{
			slog.String(RequestIDKey, RequestIDFromContext(c.Request.Context())),
			slog.String("method", c.Request.Method),
			slog.String("path", c.FullPath()),
			slog.String("raw_path", c.Request.URL.Path),
			slog.Int("status", statusCode),
			slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
			slog.Int("bytes", c.Writer.Size()),
		}

		if userID, ok := c.Get("user_id"); ok {
			attrs = append(attrs, slog.Any("user_id", userID))
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, slog.Any("error", c.Errors.Last().Err))
		}

		level := httpLevel(statusCode)
		logger.LogAttrs(c.Request.Context(), level, "http request completed", attrs...)
	}
}

func RecoveryMiddleware(logger *slog.Logger) gin.HandlerFunc {
	logger = WithComponent(logger, "http")

	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				err := fmt.Errorf("panic: %v", rec)
				_ = c.Error(err)
				logger.ErrorContext(
					c.Request.Context(),
					"http request panicked",
					RequestIDKey, RequestIDFromContext(c.Request.Context()),
					"method", c.Request.Method,
					"path", c.Request.URL.Path,
					"error", err,
					"stack", string(debug.Stack()),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": apperrors.ErrInternal.Error()})
			}
		}()

		c.Next()
	}
}

func UnaryServerInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	logger = WithComponent(logger, "grpc_server")

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		startedAt := time.Now()
		ctx = contextWithIncomingRequestID(ctx)

		resp, err := handler(ctx, req)
		code := status.Code(err)

		attrs := []slog.Attr{
			slog.String(RequestIDKey, RequestIDFromContext(ctx)),
			slog.String("grpc_method", info.FullMethod),
			slog.String("grpc_code", code.String()),
			slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		}
		if err != nil {
			attrs = append(attrs, slog.Any("error", err))
		}

		logger.LogAttrs(ctx, grpcLevel(code), "grpc request completed", attrs...)
		return resp, err
	}
}

func UnaryClientInterceptor(logger *slog.Logger) grpc.UnaryClientInterceptor {
	logger = WithComponent(logger, "grpc_client")

	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if requestID := RequestIDFromContext(ctx); requestID != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, strings.ToLower(RequestIDHeader), requestID)
		}

		startedAt := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		if err == nil {
			return nil
		}

		code := status.Code(err)
		logger.LogAttrs(ctx, grpcLevel(code), "grpc client request failed",
			slog.String(RequestIDKey, RequestIDFromContext(ctx)),
			slog.String("grpc_method", method),
			slog.String("grpc_code", code.String()),
			slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
			slog.Any("error", err),
		)
		return err
	}
}

func contextWithIncomingRequestID(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}

	values := md.Get(strings.ToLower(RequestIDHeader))
	if len(values) == 0 {
		values = md.Get(RequestIDHeader)
	}
	if len(values) == 0 || values[0] == "" {
		return ctx
	}
	return ContextWithRequestID(ctx, values[0])
}

func httpLevel(statusCode int) slog.Level {
	switch {
	case statusCode >= http.StatusInternalServerError:
		return slog.LevelError
	case statusCode >= http.StatusBadRequest:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

func grpcLevel(code codes.Code) slog.Level {
	switch code {
	case codes.OK:
		return slog.LevelInfo
	case codes.Canceled, codes.InvalidArgument, codes.NotFound, codes.AlreadyExists, codes.PermissionDenied, codes.Unauthenticated, codes.ResourceExhausted, codes.FailedPrecondition, codes.Aborted, codes.OutOfRange:
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}

func parseLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
