package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	clickhouserepo "github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/clickhouse"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/registry"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/objectstorage"
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
	gRPCServer        *grpc.Server
	httpServer        *http.Server
	db                *sql.DB
	reportsRepo       *clickhouserepo.ReportsRepository
	dockerCli         *docker.Adapter
	orchestrator      *compose.Orchestrator
	buildPublisher    service.BuildQueuePublisher
	composeConsumer   *rabbitmq.ComposeConsumer
	containerConsumer *rabbitmq.ContainerConsumer
	ssoConn           *grpc.ClientConn
	port              int
	ctx               context.Context
	cancel            context.CancelFunc
	wg                *sync.WaitGroup
	logger            *slog.Logger
}

type Config struct {
	Port          int
	HTTPPort      int
	DBURL         string
	RabbitMQURL   string
	SSOTarget     string
	InternalToken string
	HostDiskPath  string
	ConfigManager *config.Manager
	ObjectStorage objectstorage.Config
	ClickHouse    ClickHouseConfig
}

type ClickHouseConfig struct {
	Addr     string
	Database string
	Username string
	Password string
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	appLogger := logging.WithComponent(logger, "app")

	db, err := sql.Open("postgres", cfg.DBURL)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	dockerAdapter, err := docker.NewAdapter(docker.WithHostPathPrefix(cfg.HostDiskPath))
	if err != nil {
		return nil, err
	}

	registryAdapter := registry.NewAdapter(cfg.ConfigManager.Get().RegistryAPIURL)
	ssoConn, err := grpc.NewClient(
		cfg.SSOTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(
			logging.UnaryClientInterceptor(logger),
			internalauth.UnaryClientInterceptor(cfg.InternalToken),
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
	stagedRepo := repository.NewStagedObjectRepository(db)
	reportSnapshotRepo := repository.NewReportSnapshotRepository(db, dockerAdapter, logger)
	reportsRepo := clickhouserepo.NewReportsRepository(clickhouserepo.Config{
		Addr:     cfg.ClickHouse.Addr,
		Database: cfg.ClickHouse.Database,
		Username: cfg.ClickHouse.Username,
		Password: cfg.ClickHouse.Password,
	})

	metricsProvider := metrics.NewSystemMetrics()

	contService := service.NewContainerService(contRepo, volRepo, imgRepo, dockerAdapter, metricsProvider, cfg.ConfigManager, ssoClient, cfg.HostDiskPath, logger)
	volService := service.NewVolumeService(volRepo, dockerAdapter, cfg.ConfigManager, service.VolumeServiceDeps{
		Users:        ssoClient,
		ImageRepo:    imgRepo,
		DiskMetrics:  metricsProvider,
		HostDiskPath: cfg.HostDiskPath,
	})
	imgService := service.NewImageService(imgRepo, dockerAdapter, registryAdapter, contRepo, cfg.ConfigManager)
	objectStore := objectstorage.NewLazyStorage(cfg.ObjectStorage)
	buildService := service.NewBuildService(buildRepo, imgRepo, registryAdapter, ssoClient, logger, service.BuildServiceDeps{
		VolumeRepo:    volRepo,
		Config:        cfg.ConfigManager,
		ObjectStore:   objectStore,
		StagedObjects: stagedRepo,
		Containers:    contRepo,
		DiskMetrics:   metricsProvider,
		HostMetrics:   metricsProvider,
		HostDiskPath:  cfg.HostDiskPath,
	})
	resourceRepo := &projectResourceRepo{contRepo, volRepo}
	projService := service.NewProjectService(projRepo, resourceRepo, dockerAdapter, contService, volService, cfg.ConfigManager)
	contService.SetProjectStatusUpdater(projService)
	systemService := service.NewSystemService(cfg.ConfigManager)
	statsService := service.NewStatsService(contRepo, volRepo, imgRepo, buildRepo, projRepo, metricsProvider, cfg.HostDiskPath, cfg.ConfigManager, ssoClient)
	reportService := service.NewReportService(reportsRepo, reportSnapshotRepo, ssoClient, cfg.ConfigManager, logger)
	contService.SetAuditRecorder(reportService)
	buildPublisher := rabbitmq.NewPublisher(cfg.RabbitMQURL)
	composeConsumer := rabbitmq.NewComposeConsumer(cfg.RabbitMQURL, "core", logger)
	containerConsumer := rabbitmq.NewContainerConsumer(cfg.RabbitMQURL, "core", logger)

	gRPCServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		logging.UnaryServerInterceptor(logger),
		internalauth.UnaryServerInterceptor(cfg.InternalToken),
	))

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	orchestrator := compose.NewOrchestrator(ctx, projRepo, buildRepo, volService, contService, dockerAdapter, cfg.ConfigManager, objectStore, buildService, imgService, logger)
	orchestrator.SetResourceRepository(resourceRepo)
	orchestrator.SetStagedObjectRepository(stagedRepo)
	orchestrator.SetHostDiskGuard(metricsProvider, cfg.HostDiskPath)
	orchestrator.SetUserInfoProvider(ssoClient)
	orchestrator.SetAuditRecorder(reportService)
	recoveryMessage := "deployment interrupted by core service restart"
	if err := orchestrator.CleanupInterruptedDeployments(context.Background(), recoveryMessage); err != nil {
		appLogger.Warn("failed to cleanup interrupted compose deployments", "error", err)
	}
	if err := projRepo.RecoverInterruptedComposeDeployments(context.Background(), cfg.ConfigManager.Get().ComposeDeployMaxAttempts, recoveryMessage); err != nil {
		appLogger.Warn("failed to recover interrupted compose deployments", "error", err)
	}
	operationRecoveryMessage := "operation interrupted by core service restart"
	if recovered, err := contRepo.RecoverInterruptedContainerCreates(context.Background(), errors.New(operationRecoveryMessage)); err != nil {
		appLogger.Warn("failed to recover interrupted container create operations", "error", err)
	} else if recovered > 0 {
		appLogger.Warn("requeued interrupted container create operations", "count", recovered)
	}
	if recovered, err := contRepo.FailActiveNonCreateOperations(context.Background(), errors.New(operationRecoveryMessage)); err != nil {
		appLogger.Warn("failed to recover interrupted non-create resource operations", "error", err)
	} else if recovered > 0 {
		appLogger.Warn("failed interrupted non-create resource operations", "count", recovered)
	}
	buildService.SetDeploymentCanceler(orchestrator)
	projService.SetDeploymentCanceler(orchestrator)
	composeHandler := corehttp.NewComposeHandler(orchestrator, cfg.ConfigManager)
	buildHandler := corehttp.NewBuildHandler(buildService, cfg.ConfigManager)
	router := corehttp.SetupRouter(composeHandler, buildHandler, cfg.InternalToken, logger)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: router,
	}

	coregrpc.RegisterContainerAPI(gRPCServer, contService, ssoClient)
	coregrpc.RegisterVolumeAPI(gRPCServer, volService, ssoClient)
	coregrpc.RegisterImageAPI(gRPCServer, imgService, buildService, ssoClient)
	coregrpc.RegisterProjectAPI(gRPCServer, projService, ssoClient)
	coregrpc.RegisterSystemAPI(gRPCServer, systemService)
	coregrpc.RegisterStatsAPI(gRPCServer, statsService)
	coregrpc.RegisterReportAPI(gRPCServer, reportService)

	ttlWorker := service.NewTTLWorker(contRepo, dockerAdapter, cfg.ConfigManager, logger)
	eventWorker := service.NewEventWorker(contRepo, volRepo, dockerAdapter, contService, projService, cfg.ConfigManager, logger)
	gcWorker := service.NewGCWorker(dockerAdapter, buildService, buildRepo, stagedRepo, objectStore, cfg.ConfigManager, logger)
	volumeUsageWorker := service.NewVolumeUsageWorker(volRepo, contRepo, imgRepo, dockerAdapter, ssoClient, cfg.ConfigManager, logger)
	buildOutboxWorker := service.NewBuildOutboxWorker(buildRepo, buildPublisher, cfg.ConfigManager, logger)
	composeOutboxWorker := service.NewComposeOutboxWorker(projRepo, buildPublisher, cfg.ConfigManager, logger)
	containerOutboxWorker := service.NewContainerOutboxWorker(contRepo, buildPublisher, cfg.ConfigManager, logger)
	containerCreateWorker := service.NewContainerCreateWorker(contService, cfg.ConfigManager, logger)
	composeCoordinator := compose.NewComposeDeploymentCoordinator(orchestrator, logger)
	reportsUsageWorker := service.NewReportsUsageWorker(reportService, logger)

	startWorkers(ctx, wg,
		ttlWorker.Run,
		eventWorker.Run,
		gcWorker.Run,
		volumeUsageWorker.Run,
		contService.RunRebalancer,
		buildOutboxWorker.Run,
		composeOutboxWorker.Run,
		containerOutboxWorker.Run,
		composeCoordinator.Run,
		projService.RunLifecycleCoordinator,
		reportsUsageWorker.Run,
	)
	composeConsumer.Run(ctx, cfg.ConfigManager.Get().ComposeDeployWorkerCount, orchestrator.HandleDeploymentMessage)
	containerConsumer.Run(ctx, cfg.ConfigManager.Get().ContainerCreateWorkerCount, containerCreateWorker.HandleMessage)

	return &App{
		gRPCServer:        gRPCServer,
		httpServer:        httpServer,
		db:                db,
		reportsRepo:       reportsRepo,
		dockerCli:         dockerAdapter,
		orchestrator:      orchestrator,
		buildPublisher:    buildPublisher,
		composeConsumer:   composeConsumer,
		containerConsumer: containerConsumer,
		ssoConn:           ssoConn,
		port:              cfg.Port,
		ctx:               ctx,
		cancel:            cancel,
		wg:                wg,
		logger:            appLogger,
	}, nil
}

func startWorkers(ctx context.Context, wg *sync.WaitGroup, workers ...func(context.Context)) {
	wg.Add(len(workers))
	for _, worker := range workers {
		worker := worker
		go func() {
			defer wg.Done()
			worker(ctx)
		}()
	}
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
	if a.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
			a.logger.Warn("core http server shutdown failed", "error", err)
		}
		cancel()
	}
	if a.gRPCServer != nil {
		stopGRPCServer(a.gRPCServer, a.logger, 10*time.Second)
	}
	if a.composeConsumer != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := a.composeConsumer.Stop(stopCtx); err != nil {
			a.logger.Warn("compose queue consumer shutdown timed out", "error", err)
		}
		cancel()
	}
	if a.containerConsumer != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := a.containerConsumer.Stop(stopCtx); err != nil {
			a.logger.Warn("container queue consumer shutdown timed out", "error", err)
		}
		cancel()
	}
	if a.orchestrator != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := a.orchestrator.Stop(stopCtx); err != nil {
			a.logger.Warn("compose orchestrator shutdown timed out", "error", err)
		}
		cancel()
	}
	a.wg.Wait()
	if a.db != nil {
		a.db.Close()
	}
	if a.reportsRepo != nil {
		if err := a.reportsRepo.Close(); err != nil {
			a.logger.Warn("clickhouse reports repository close failed", "error", err)
		}
	}
	if a.dockerCli != nil {
		a.dockerCli.Close()
	}
	if a.buildPublisher != nil {
		_ = a.buildPublisher.Close()
	}
	if a.ssoConn != nil {
		a.ssoConn.Close()
	}
}

func stopGRPCServer(server *grpc.Server, logger *slog.Logger, timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		logger.Warn("core grpc graceful stop timed out; forcing stop")
		server.Stop()
	}
}
