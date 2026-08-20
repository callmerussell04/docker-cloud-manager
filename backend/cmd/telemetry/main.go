package main

import (
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/telemetry/app"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
)

func main() {
	logger := logging.NewLogger("telemetry", logging.ConfigFromEnv())
	slog.SetDefault(logger)

	cfg := app.Config{
		Port:          intEnv("TELEMETRY_PORT", 8084),
		CoreTarget:    requiredEnv(logger, "CORE_GRPC_TARGET"),
		InternalToken: requiredEnv(logger, "INTERNAL_SERVICE_TOKEN"),
		MaxLogStreams: intEnv("TELEMETRY_MAX_GLOBAL_LOG_STREAMS", 100),
		MaxTerminals:  intEnv("TELEMETRY_MAX_GLOBAL_TERMINAL_SESSIONS", 50),
		HTTP: app.HTTPServerConfig{
			ReadHeaderTimeout: durationSecondsEnv("TELEMETRY_READ_HEADER_TIMEOUT_SECONDS", 5*time.Second),
			IdleTimeout:       durationSecondsEnv("TELEMETRY_IDLE_TIMEOUT_SECONDS", 120*time.Second),
			ShutdownTimeout:   durationSecondsEnv("TELEMETRY_SHUTDOWN_TIMEOUT_SECONDS", 15*time.Second),
			MaxHeaderBytes:    intEnv("TELEMETRY_MAX_HEADER_BYTES", 1<<20),
		},
		GRPC: app.GRPCConfig{
			RequestTimeout:    durationSecondsEnv("TELEMETRY_GRPC_REQUEST_TIMEOUT_SECONDS", 10*time.Second),
			MinConnectTimeout: durationSecondsEnv("TELEMETRY_GRPC_MIN_CONNECT_TIMEOUT_SECONDS", 5*time.Second),
		},
	}

	application, err := app.New(cfg, logger)
	if err != nil {
		fatal(logger, "failed to initialize telemetry app", "error", err)
	}

	if err := application.Run(); err != nil {
		fatal(logger, "telemetry server failed", "error", err)
	}
}

func requiredEnv(logger *slog.Logger, key string) string {
	value := os.Getenv(key)
	if value == "" {
		fatal(logger, "required environment variable is not set", "env_var", key)
	}
	return value
}

func intEnv(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func durationSecondsEnv(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return time.Duration(value) * time.Second
}

func fatal(logger *slog.Logger, msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}
