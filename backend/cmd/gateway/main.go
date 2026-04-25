package main

import (
	"log/slog"
	"os"
	"strconv"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/app"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
)

func main() {
	logger := logging.NewLogger("gateway", logging.ConfigFromEnv())
	slog.SetDefault(logger)

	portStr := os.Getenv("GATEWAY_PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 8081
	}

	ssoTarget := os.Getenv("SSO_GRPC_TARGET")
	if ssoTarget == "" {
		fatal(logger, "required environment variable is not set", "env_var", "SSO_GRPC_TARGET")
	}

	coreTarget := os.Getenv("CORE_GRPC_TARGET")
	if coreTarget == "" {
		fatal(logger, "required environment variable is not set", "env_var", "CORE_GRPC_TARGET")
	}

	internalToken := os.Getenv("INTERNAL_SERVICE_TOKEN")
	if internalToken == "" {
		fatal(logger, "required environment variable is not set", "env_var", "INTERNAL_SERVICE_TOKEN")
	}

	builderHttpTarget := os.Getenv("BUILDER_HTTP_TARGET")
	if builderHttpTarget == "" {
		fatal(logger, "required environment variable is not set", "env_var", "BUILDER_HTTP_TARGET")
	}

	coreHttpTarget := os.Getenv("CORE_HTTP_TARGET")
	if coreHttpTarget == "" {
		fatal(logger, "required environment variable is not set", "env_var", "CORE_HTTP_TARGET")
	}

	application, err := app.New(app.Config{
		Port:              port,
		SSOTarget:         ssoTarget,
		CoreTarget:        coreTarget,
		BuilderHTTPTarget: builderHttpTarget,
		CoreHTTPTarget:    coreHttpTarget,
		InternalToken:     internalToken,
	}, logger)
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
