package main

import (
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
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

	adminUsername := getEnvString("SSO_ADMIN_USERNAME", "admin")
	adminEmail := getEnvString("SSO_ADMIN_EMAIL", "admin@example.local")
	adminPassword := os.Getenv("SSO_ADMIN_PASSWORD")
	if adminPassword == "" {
		fatal(logger, "required environment variable is not set", "env_var", "SSO_ADMIN_PASSWORD")
	}

	accessTTL := 15 * time.Minute
	refreshTTL := 30 * 24 * time.Hour
	oidcEnabled := getEnvBool("SSO_OIDC_ENABLED", false)

	application, err := app.New(app.Config{
		Port:          port,
		DBURL:         dbURL,
		JWTSecret:     jwtSecret,
		InternalToken: internalToken,
		AccessTTL:     accessTTL,
		RefreshTTL:    refreshTTL,
		BootstrapAdmin: app.BootstrapAdminConfig{
			Username: adminUsername,
			Email:    adminEmail,
			Password: adminPassword,
		},
		Auth: app.AuthConfig{
			LocalLoginEnabled:    getEnvBool("SSO_LOCAL_LOGIN_ENABLED", true),
			LocalRegisterEnabled: getEnvBool("SSO_LOCAL_REGISTER_ENABLED", true),
		},
		OIDC: app.OIDCConfig{
			Enabled:      oidcEnabled,
			IssuerURL:    getEnvString("SSO_OIDC_ISSUER_URL", ""),
			ClientID:     getEnvString("SSO_OIDC_CLIENT_ID", ""),
			ClientSecret: os.Getenv("SSO_OIDC_CLIENT_SECRET"),
			RedirectURL:  getEnvString("SSO_OIDC_REDIRECT_URL", ""),
			AdminGroups:  getEnvList("SSO_OIDC_ADMIN_GROUPS"),
			DefaultRole:  getEnvString("SSO_OIDC_DEFAULT_ROLE", "user"),
		},
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

func getEnvString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvList(key string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
