package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/app"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/pgpool"
)

func main() {
	logger := logging.NewLogger("core", logging.ConfigFromEnv(os.LookupEnv))
	slog.SetDefault(logger)

	settings, err := config.Load(os.LookupEnv)
	if err != nil {
		msg, args := envconfig.LogEntry(err)
		fatal(logger, msg, args...)
	}

	cfgManager, err := config.NewManager("./config/config.json", settings.Runtime)
	if err != nil {
		fatal(logger, "failed to initialize config manager", "error", err)
	}

	poolCfg, err := pgpool.Parse(os.LookupEnv, "CORE")
	if err != nil {
		fatal(logger, "invalid postgres pool settings", "error", err)
	}
	application, err := app.New(app.Config{
		Bootstrap:     settings.Bootstrap,
		DBPool:        poolCfg,
		ConfigManager: cfgManager,
	}, logger)
	if err != nil {
		fatal(logger, "failed to initialize core application", "error", err)
	}

	go func() {
		if err := application.Run(); err != nil {
			fatal(logger, "core grpc server failed", "error", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop

	application.Stop()
}

func fatal(logger *slog.Logger, msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}
