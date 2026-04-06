package app

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/registry"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/google/uuid"
	"google.golang.org/grpc"

	coregrpc "github.com/callmerussell04/docker-cloud-manager/internal/core/grpc"
	corehttp "github.com/callmerussell04/docker-cloud-manager/internal/core/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/metrics"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
)

type App struct {
	gRPCServer *grpc.Server
	httpServer *http.Server
	db         *sql.DB
	dockerCli  *docker.Adapter
	port       int
	ctx        context.Context
	cancel     context.CancelFunc
}

type projectResourceRepo struct {
	contRepo *repository.ContainerRepository
	volRepo  *repository.VolumeRepository
}

func (p *projectResourceRepo) GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Container, error) {
	return p.contRepo.GetByOwnerID(ctx, ownerID)
}
func (p *projectResourceRepo) GetVolumesByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Volume, error) {
	return p.volRepo.GetByOwnerID(ctx, ownerID)
}

func New(port int, httpPort int, dbURL string, registryURL string, registryContainerName string, builderHTTPUrl string, cfg service.ContainerConfig) (*App, error) {
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

	registryAdapter := registry.NewAdapter(registryURL)

	contRepo := repository.NewContainerRepository(db)
	volRepo := repository.NewVolumeRepository(db)
	imgRepo := repository.NewImageRepository(db)
	buildRepo := repository.NewBuildRepository(db)
	projRepo := repository.NewProjectRepository(db)

	metricsProvider := metrics.NewSystemMetrics()

	contService := service.NewContainerService(contRepo, volRepo, imgRepo, dockerAdapter, metricsProvider, cfg)
	volService := service.NewVolumeService(volRepo, dockerAdapter, cfg.MaxVolumesPerUser)
	imgService := service.NewImageService(imgRepo, buildRepo, dockerAdapter, registryAdapter, registryURL)
	projService := service.NewProjectService(projRepo, &projectResourceRepo{contRepo, volRepo}, dockerAdapter)
	gRPCServer := grpc.NewServer()

	orchestrator := compose.NewOrchestrator(projRepo, buildRepo, volService, contService, builderHTTPUrl)
	composeHandler := corehttp.NewComposeHandler(orchestrator)
	router := corehttp.SetupRouter(composeHandler)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", httpPort),
		Handler: router,
	}

	coregrpc.RegisterContainerAPI(gRPCServer, contService)
	coregrpc.RegisterVolumeAPI(gRPCServer, volService)
	coregrpc.RegisterImageAPI(gRPCServer, imgService)
	coregrpc.RegisterProjectAPI(gRPCServer, projService)

	ctx, cancel := context.WithCancel(context.Background())

	ttlWorker := service.NewTTLWorker(contRepo, dockerAdapter, 1*time.Minute)
	go ttlWorker.Run(ctx)

	eventWorker := service.NewEventWorker(contRepo, dockerAdapter, contService)
	go eventWorker.Run(ctx)

	gcWorker := service.NewGCWorker(dockerAdapter, imgService, buildRepo, 1*time.Hour, 30*time.Minute, registryContainerName)
	go gcWorker.Run(ctx)

	return &App{
		gRPCServer: gRPCServer,
		httpServer: httpServer,
		db:         db,
		dockerCli:  dockerAdapter,
		port:       port,
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

func (a *App) Run() error {
	go func() {
		_ = a.httpServer.ListenAndServe()
	}()

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
	if a.httpServer != nil {
		_ = a.httpServer.Shutdown(context.Background())
	}
}
