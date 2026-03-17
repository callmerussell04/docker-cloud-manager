package app

import (
	"database/sql"
	"fmt"
	"net"
	"time"

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
}

func New(port int, dbURL string, jwtSecret string, accessTTL, refreshTTL time.Duration) (*App, error) {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	repo := repository.NewUserRepository(db)
	tokenProvider := jwt.NewProvider(jwtSecret, accessTTL, refreshTTL)
	authService := service.NewAuth(repo, tokenProvider)

	gRPCServer := grpc.NewServer()
	authgrpc.Register(gRPCServer, authService)

	return &App{
		gRPCServer: gRPCServer,
		db:         db,
		port:       port,
	}, nil
}

func (a *App) Run() error {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", a.port))
	if err != nil {
		return err
	}

	return a.gRPCServer.Serve(l)
}

func (a *App) Stop() {
	a.gRPCServer.GracefulStop()
	if a.db != nil {
		a.db.Close()
	}
}
