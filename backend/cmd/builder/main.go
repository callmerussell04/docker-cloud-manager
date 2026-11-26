package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/app"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
)

func main() {
	logger := logging.NewLogger("builder", logging.ConfigFromEnv(os.LookupEnv))
	slog.SetDefault(logger)

	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		msg, args := envconfig.LogEntry(err)
		fatal(logger, msg, args...)
	}

	application, err := app.New(cfg, logger)
	if err != nil {
		fatal(logger, "failed to initialize builder app", "error", err)
	}

	go func() {
		if err := application.Run(); err != nil {
			fatal(logger, "builder server failed", "error", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	application.Stop(shutdownCtx)
}

func fatal(logger *slog.Logger, msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}
