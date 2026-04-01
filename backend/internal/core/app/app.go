package app

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"

	coredelivery "github.com/callmerussell04/docker-cloud-manager/internal/core/grpc"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/metrics"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
)

type App struct {
	gRPCServer *grpc.Server
	db         *sql.DB
	dockerCli  *docker.Adapter
	port       int
	ctx        context.Context
	cancel     context.CancelFunc
}

func New(port int, dbURL string, cfg service.ContainerConfig) (*App, error) {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	dockerAdapter, err := docker.NewAdapter()
	if err != nil {
		return nil, err
	}

	contRepo := repository.NewContainerRepository(db)
	volRepo := repository.NewVolumeRepository(db)
	imgRepo := repository.NewImageRepository(db)

	metricsProvider := metrics.NewSystemMetrics()

	contService := service.NewContainerService(contRepo, volRepo, dockerAdapter, metricsProvider, cfg)
	volService := service.NewVolumeService(volRepo, dockerAdapter, cfg.MaxVolumesPerUser)
	imgService := service.NewImageService(imgRepo, dockerAdapter)

	gRPCServer := grpc.NewServer()

	coredelivery.RegisterContainerAPI(gRPCServer, contService)
	coredelivery.RegisterVolumeAPI(gRPCServer, volService)
	coredelivery.RegisterImageAPI(gRPCServer, imgService)

	ctx, cancel := context.WithCancel(context.Background())

	ttlWorker := service.NewTTLWorker(contRepo, dockerAdapter, 1*time.Minute)
	go ttlWorker.Run(ctx)

	eventWorker := service.NewEventWorker(contRepo, dockerAdapter, contService)
	go eventWorker.Run(ctx)

	gcWorker := service.NewGCWorker(dockerAdapter, 1*time.Hour)
	go gcWorker.Run(ctx)

	return &App{
		gRPCServer: gRPCServer,
		db:         db,
		dockerCli:  dockerAdapter,
		port:       port,
		ctx:        ctx,
		cancel:     cancel,
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
	a.cancel()
	a.gRPCServer.GracefulStop()
	if a.db != nil {
		a.db.Close()
	}
	if a.dockerCli != nil {
		a.dockerCli.Close()
	}
}
