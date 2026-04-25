package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/registry"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	coregrpc "github.com/callmerussell04/docker-cloud-manager/internal/core/grpc"
	grpcclient "github.com/callmerussell04/docker-cloud-manager/internal/core/grpc/client"
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
	ssoConn    *grpc.ClientConn
	port       int
	ctx        context.Context
	cancel     context.CancelFunc
	wg         *sync.WaitGroup
	logger     *slog.Logger
}

func New(port int, httpPort int, dbURL string, registryContainerName string, builderHTTPUrl string, ssoTarget string, internalToken string, configManager *config.Manager, logger *slog.Logger) (*App, error) {
	appLogger := logging.WithComponent(logger, "app")

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

	registryAdapter := registry.NewAdapter(configManager.Get().RegistryAPIURL)
	ssoConn, err := grpc.NewClient(
		ssoTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(
			logging.UnaryClientInterceptor(logger),
			internalauth.UnaryClientInterceptor(internalToken),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("sso conn fail: %w", err)
	}
	ssoClient := grpcclient.NewSSOClient(ssoConn)

	contRepo := repository.NewContainerRepository(db)
	volRepo := repository.NewVolumeRepository(db)
	imgRepo := repository.NewImageRepository(db)
	buildRepo := repository.NewBuildRepository(db)
	projRepo := repository.NewProjectRepository(db)

	metricsProvider := metrics.NewSystemMetrics()

	contService := service.NewContainerService(contRepo, volRepo, imgRepo, dockerAdapter, metricsProvider, configManager, ssoClient, logger)
	volService := service.NewVolumeService(volRepo, dockerAdapter, configManager)
	imgService := service.NewImageService(imgRepo, dockerAdapter, registryAdapter, contRepo, configManager)
	buildService := service.NewBuildService(buildRepo, imgRepo, registryAdapter, ssoClient, logger)
	projService := service.NewProjectService(projRepo, &projectResourceRepo{contRepo, volRepo}, dockerAdapter)
	systemService := service.NewSystemService(configManager)
	statsService := service.NewStatsService(contRepo, volRepo, imgRepo, projRepo, configManager, ssoClient)

	gRPCServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		logging.UnaryServerInterceptor(logger),
		internalauth.UnaryServerInterceptor(internalToken),
	))

	orchestrator := compose.NewOrchestrator(projRepo, buildRepo, volService, contService, dockerAdapter, builderHTTPUrl, internalToken, logger)
	composeHandler := corehttp.NewComposeHandler(orchestrator)
	router := corehttp.SetupRouter(composeHandler, internalToken, logger)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", httpPort),
		Handler: router,
	}

	coregrpc.RegisterContainerAPI(gRPCServer, contService, ssoClient)
	coregrpc.RegisterVolumeAPI(gRPCServer, volService, ssoClient)
	coregrpc.RegisterImageAPI(gRPCServer, imgService, buildService, ssoClient)
	coregrpc.RegisterProjectAPI(gRPCServer, projService, ssoClient)
	coregrpc.RegisterSystemAPI(gRPCServer, systemService)
	coregrpc.RegisterStatsAPI(gRPCServer, statsService)

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	ttlWorker := service.NewTTLWorker(contRepo, dockerAdapter, 1*time.Minute, logger)
	eventWorker := service.NewEventWorker(contRepo, dockerAdapter, contService, logger)
	gcWorker := service.NewGCWorker(dockerAdapter, buildService, buildRepo, 1*time.Hour, 30*time.Minute, registryContainerName, logger)

	wg.Add(3)
	go func() {
		defer wg.Done()
		ttlWorker.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		eventWorker.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		gcWorker.Run(ctx)
	}()

	return &App{
		gRPCServer: gRPCServer,
		httpServer: httpServer,
		db:         db,
		dockerCli:  dockerAdapter,
		ssoConn:    ssoConn,
		port:       port,
		ctx:        ctx,
		cancel:     cancel,
		wg:         wg,
		logger:     appLogger,
	}, nil
}

func (a *App) Run() error {
	go func() {
		a.logger.Info("core http server starting", "port", a.httpServer.Addr)
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Error("core http server failed", "error", err)
		}
	}()

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", a.port))
	if err != nil {
		return err
	}

	a.logger.Info("core grpc server starting", "port", a.port)
	return a.gRPCServer.Serve(l)
}

func (a *App) Stop() {
	a.logger.Info("core application stopping")
	a.cancel()
	a.wg.Wait()
	a.gRPCServer.GracefulStop()
	if a.db != nil {
		a.db.Close()
	}
	if a.dockerCli != nil {
		a.dockerCli.Close()
	}
	if a.ssoConn != nil {
		a.ssoConn.Close()
	}
	if a.httpServer != nil {
		_ = a.httpServer.Shutdown(context.Background())
	}
}
