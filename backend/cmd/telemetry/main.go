package main

import (
	"log/slog"
	"os"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/telemetry/app"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/telemetry/config"
)

func main() {
	logger := logging.NewLogger("telemetry", logging.ConfigFromEnv(os.LookupEnv))
	slog.SetDefault(logger)

	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		msg, args := envconfig.LogEntry(err)
		fatal(logger, msg, args...)
	}

	application, err := app.New(cfg, logger)
	if err != nil {
		fatal(logger, "failed to initialize telemetry app", "error", err)
	}

	if err := application.Run(); err != nil {
		fatal(logger, "telemetry server failed", "error", err)
	}
}

func fatal(logger *slog.Logger, msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}
