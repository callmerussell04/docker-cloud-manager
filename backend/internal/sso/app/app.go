package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"google.golang.org/grpc"

	authgrpc "github.com/callmerussell04/docker-cloud-manager/internal/sso/grpc"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/lib/jwt"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/service"
)

type App struct {
	gRPCServer *grpc.Server
	db         *sql.DB
	port       int
	logger     *slog.Logger
}

type Config struct {
	Port           int
	DBURL          string
	JWTSecret      string
	InternalToken  string
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	BootstrapAdmin BootstrapAdminConfig
}

type BootstrapAdminConfig struct {
	Username string
	Email    string
	Password string
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	db, err := sql.Open("postgres", cfg.DBURL)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	repo := repository.NewUserRepository(db)
	tokenProvider := jwt.NewProvider(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)
	authService := service.NewAuthService(repo, tokenProvider)
	if err := ensureBootstrapAdmin(authService, cfg.BootstrapAdmin, logger); err != nil {
		return nil, err
	}

	gRPCServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		logging.UnaryServerInterceptor(logger),
		internalauth.UnaryServerInterceptor(cfg.InternalToken),
	))
	authgrpc.Register(gRPCServer, authService)

	return &App{
		gRPCServer: gRPCServer,
		db:         db,
		port:       cfg.Port,
		logger:     logging.WithComponent(logger, "app"),
	}, nil
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
	if a.db != nil {
		a.db.Close()
	}
}
