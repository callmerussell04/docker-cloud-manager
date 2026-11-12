package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/pgpool"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	ssogrpc "github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/grpc"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/jwt"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/oidc"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
)

type App struct {
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	gRPCServer *grpc.Server
	db         *pgxpool.Pool
	port       int
	logger     *slog.Logger
}

// Config adds the database pool budget, which config cannot import, to the loaded settings.
type Config struct {
	config.Bootstrap
	DBPool pgpool.Config
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	db, err := pgpool.Open(context.Background(), cfg.DBURL, cfg.DBPool, pgpool.PingTimeout)
	if err != nil {
		return nil, err
	}
	initialized := false
	defer func() {
		if !initialized {
			db.Close()
		}
	}()

	repo := repository.NewUserRepository(db)
	tokenProvider := jwt.NewProvider(cfg.JWTSecret)
	oidcProvider, err := buildOIDCProvider(cfg.OIDC)
	if err != nil {
		return nil, err
	}
	authService := service.NewAuthServiceWithConfig(repo, tokenProvider, repository.NewSessionRepository(db), oidcProvider, service.Config{
		LocalLoginEnabled: cfg.Auth.LocalLoginEnabled,
		AccessTTL:         cfg.AccessTTL, SessionTTL: cfg.SessionTTL, SessionIdleTTL: cfg.SessionIdleTTL,
		LocalRegisterEnabled: cfg.Auth.LocalRegisterEnabled,
		OIDCEnabled:          cfg.OIDC.Enabled,
		OIDCProviderName:     "keycloak",
		OIDCAdminGroups:      cfg.OIDC.AdminGroups,
		OIDCDefaultRole:      cfg.OIDC.DefaultRole,
	})
	if err := ensureBootstrapAdmin(authService, cfg.BootstrapAdmin, logger); err != nil {
		return nil, err
	}

	authenticator, err := internalauth.NewAuthenticator(ssogrpc.CallerPolicies(ssogrpc.CallerCredentials{
		Gateway: cfg.GatewayToken, Core: cfg.CoreToken,
	})...)
	if err != nil {
		return nil, err
	}
	gRPCServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		logging.UnaryServerInterceptor(logger),
		authenticator.UnaryServerInterceptor(),
	))
	ssogrpc.Register(gRPCServer, authService)

	ctx, cancel := context.WithCancel(context.Background())
	application := &App{
		gRPCServer: gRPCServer,
		db:         db,
		port:       cfg.Port,
		logger:     logging.WithComponent(logger, "app"),
		cancel:     cancel,
	}
	application.wg.Add(1)
	go func() {
		defer application.wg.Done()
		pgpool.LogStats(ctx, application.logger, map[string]*pgxpool.Pool{"queries": db})
	}()
	application.wg.Add(1)
	go func() {
		defer application.wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cleanupCtx, cleanupCancel := context.WithTimeout(ctx, 10*time.Second)
				if err := authService.CleanupSessions(cleanupCtx); err != nil {
					application.logger.Error("session cleanup failed", "error", err)
				}
				cleanupCancel()
			}
		}
	}()
	initialized = true
	return application, nil
}

func buildOIDCProvider(cfg config.OIDC) (service.OIDCProvider, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	provider, err := oidc.NewProvider(ctx, oidc.Config{
		IssuerURL:    cfg.IssuerURL,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize oidc provider: %w", err)
	}
	return provider, nil
}

func ensureBootstrapAdmin(authService *service.AuthService, cfg config.BootstrapAdmin, logger *slog.Logger) error {
	if strings.TrimSpace(cfg.Username) == "" {
		return fmt.Errorf("bootstrap admin username is required")
	}
	if strings.TrimSpace(cfg.Email) == "" {
		return fmt.Errorf("bootstrap admin email is required")
	}
	if cfg.Password == "" {
		return fmt.Errorf("bootstrap admin password is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	serviceCfg := service.BootstrapAdminConfig{
		Username: strings.TrimSpace(cfg.Username),
		Email:    strings.TrimSpace(cfg.Email),
		Password: cfg.Password,
	}
	if err := authService.EnsureBootstrapAdmin(ctx, serviceCfg); err != nil {
		return fmt.Errorf("failed to ensure bootstrap admin: %w", err)
	}

	logger.Info("bootstrap admin ensured", "username", serviceCfg.Username)
	return nil
}

func (a *App) Run() error {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", a.port))
	if err != nil {
		return err
	}

	a.logger.Info("sso grpc server starting", "port", a.port)
	return a.gRPCServer.Serve(l)
}

func (a *App) Stop() {
	a.logger.Info("sso application stopping")
	a.gRPCServer.GracefulStop()
	a.cancel()
	a.wg.Wait()
	if a.db != nil {
		a.db.Close()
	}
}
