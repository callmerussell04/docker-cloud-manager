package main

import (
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/app"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	_ "github.com/lib/pq"
)

func main() {
	logger := logging.NewLogger("sso", logging.ConfigFromEnv())
	slog.SetDefault(logger)

	portStr := os.Getenv("SSO_GRPC_PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 50051
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fatal(logger, "required environment variable is not set", "env_var", "DATABASE_URL")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		fatal(logger, "required environment variable is not set", "env_var", "JWT_SECRET")
	}

	internalToken := os.Getenv("INTERNAL_SERVICE_TOKEN")
	if internalToken == "" {
		fatal(logger, "required environment variable is not set", "env_var", "INTERNAL_SERVICE_TOKEN")
	}

	accessTTL := 15 * time.Minute
	refreshTTL := 30 * 24 * time.Hour

	application, err := app.New(app.Config{
		Port:          port,
		DBURL:         dbURL,
		JWTSecret:     jwtSecret,
		InternalToken: internalToken,
		AccessTTL:     accessTTL,
		RefreshTTL:    refreshTTL,
	}, logger)
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
