package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/pgpool"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/app"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/config"
)

func main() {
	logger := logging.NewLogger("sso", logging.ConfigFromEnv(os.LookupEnv))
	slog.SetDefault(logger)

	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		msg, args := envconfig.LogEntry(err)
		fatal(logger, msg, args...)
	}

	poolCfg, err := pgpool.Parse(os.LookupEnv, "SSO")
	if err != nil {
		fatal(logger, "invalid postgres pool settings", "error", err)
	}
	application, err := app.New(app.Config{Bootstrap: cfg, DBPool: poolCfg}, logger)
	if err != nil {
		fatal(logger, "failed to initialize sso application", "error", err)
	}

	go func() {
		if err := application.Run(); err != nil {
			fatal(logger, "sso grpc server failed", "error", err)
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
