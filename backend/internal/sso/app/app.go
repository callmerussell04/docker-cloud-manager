package app

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net"
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

func New(port int, dbURL string, jwtSecret string, internalToken string, accessTTL, refreshTTL time.Duration, logger *slog.Logger) (*App, error) {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	repo := repository.NewUserRepository(db)
	tokenProvider := jwt.NewProvider(jwtSecret, accessTTL, refreshTTL)
	authService := service.NewAuthService(repo, tokenProvider)

	gRPCServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		logging.UnaryServerInterceptor(logger),
		internalauth.UnaryServerInterceptor(internalToken),
	))
	authgrpc.Register(gRPCServer, authService)

	return &App{
		gRPCServer: gRPCServer,
		db:         db,
		port:       port,
		logger:     logging.WithComponent(logger, "app"),
	}, nil
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
