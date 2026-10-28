package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/pgpool"
	"google.golang.org/grpc"

	authgrpc "github.com/callmerussell04/docker-cloud-manager/internal/sso/grpc"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/lib/jwt"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/lib/oidc"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/service"
)

type App struct {
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	gRPCServer *grpc.Server
	db         *sql.DB
	port       int
	logger     *slog.Logger
}

type Config struct {
	Port           int
	DBURL          string
	DBPool         pgpool.Config
	JWTSecret      string
	InternalToken  string
	AccessTTL      time.Duration
	SessionTTL     time.Duration
	SessionIdleTTL time.Duration
	BootstrapAdmin BootstrapAdminConfig
	Auth           AuthConfig
	OIDC           OIDCConfig
}

type BootstrapAdminConfig struct {
	Username string
	Email    string
	Password string
}

type AuthConfig struct {
	LocalLoginEnabled    bool
	LocalRegisterEnabled bool
}

type OIDCConfig struct {
	Enabled      bool
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AdminGroups  []string
	DefaultRole  string
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	poolCfg := cfg.DBPool
	if err := poolCfg.Validate(1); err != nil {
		return nil, err
	}
	queryCfg := poolCfg
	db, err := sql.Open("postgres", cfg.DBURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	initialized := false
	defer func() {
		if !initialized {
			_ = db.Close() // startup failed and its error is returned; the process exits without reusing the pool
		}
	}()
	if err := queryCfg.Apply(db); err != nil {
		return nil, err
	}
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer pingCancel()
	if err := db.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("ping postgres pool: %w", err)
	}

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

	gRPCServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		logging.UnaryServerInterceptor(logger),
		internalauth.UnaryServerInterceptor(cfg.InternalToken),
	))
	authgrpc.Register(gRPCServer, authService)

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
		pgpool.LogStats(ctx, application.logger, map[string]*sql.DB{"queries": db})
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

func buildOIDCProvider(cfg OIDCConfig) (service.OIDCProvider, error) {
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

func ensureBootstrapAdmin(authService *service.AuthService, cfg BootstrapAdminConfig, logger *slog.Logger) error {
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
		if err := a.db.Close(); err != nil {
			a.logger.Warn("postgres connection close failed", "error", err)
		}
	}
}
