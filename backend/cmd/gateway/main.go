package main

import (
	"log/slog"
	"os"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/app"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
)

func main() {
	logger := logging.NewLogger("gateway", logging.ConfigFromEnv(os.LookupEnv))
	slog.SetDefault(logger)

	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		msg, args := envconfig.LogEntry(err)
		fatal(logger, msg, args...)
	}

	if cfg.AppEnv == "production" && !cfg.Cookie.Secure {
		logger.Warn("refresh token cookie is not secure in production environment")
	}

	application, err := app.New(cfg, logger)
	if err != nil {
		fatal(logger, "failed to initialize gateway app", "error", err)
	}

	if err := application.Run(); err != nil {
		fatal(logger, "gateway server failed", "error", err)
	}
}

func fatal(logger *slog.Logger, msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}
