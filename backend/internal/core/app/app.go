package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	clickhouserepo "github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/clickhouse"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/streams"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/userquota"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/pgpool"
	platformrabbitmq "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	coregrpc "github.com/callmerussell04/docker-cloud-manager/backend/internal/core/grpc"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/grpc/client"
	corehttp "github.com/callmerussell04/docker-cloud-manager/backend/internal/core/http"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/metrics"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
)

type App struct {
	gRPCServer        *grpc.Server
	httpServer        *http.Server
	httpShutdown      time.Duration
	registryServer    *http.Server
	db                *pgxpool.Pool
	lockDB            *pgxpool.Pool
	reportsRepo       *clickhouserepo.ReportsRepository
	dockerCli         *docker.Adapter
	orchestrator      *compose.Orchestrator
	outboxPublisher   service.OutboxPublisher
	composeConsumer   *platformrabbitmq.Consumer[queue.DeploymentMessage]
	containerConsumer *platformrabbitmq.Consumer[queue.ContainerLifecycleMessage]
	resourceConsumer  *platformrabbitmq.Consumer[queue.ResourceLifecycleMessage]
	ssoConn           *grpc.ClientConn
	port              int
	ctx               context.Context
	cancel            context.CancelFunc
	wg                *sync.WaitGroup
	logger            *slog.Logger
}

// Config adds what only the composition root builds to the loaded bootstrap settings.
type Config struct {
	config.Bootstrap
	DBPool        pgpool.Config
	ConfigManager *config.Manager
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	appLogger := logging.WithComponent(logger, "app")

	db, lockDB, err := openPostgresPools(context.Background(), cfg.DBURL, cfg.DBPool)
	if err != nil {
		return nil, err
	}
	initialized := false
	defer func() {
		if !initialized {
			db.Close()
			lockDB.Close()
		}
	}()

	ssoConn, err := grpc.NewClient(
		cfg.SSOTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(
			logging.UnaryClientInterceptor(logger),
			// Core may state only system scope to SSO, even while serving a user.
			internalauth.SystemScopeUnaryClientInterceptor(cfg.ServiceToken),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("sso conn fail: %w", err)
	}
	ssoClient := client.NewSSOClient(ssoConn)
	imgRepo := repository.NewImageRepository(db)
	registry, err := newRegistryComponents(cfg.Registry, repository.NewRegistryGrantRepository(db, lockDB), imgRepo, ssoClient, cfg.ConfigManager, logger)
	if err != nil {
		return nil, err
	}
	warnWithoutTenantCgroupParent(appLogger, cfg.TenantCgroupParent)
	// Host disk figures describe the filesystem Docker writes to.
	dockerDataPath := cfg.DockerDataPath()
	dockerAdapter, err := docker.NewAdapter(docker.WithHostPathPrefix(cfg.HostPathPrefix), docker.WithCgroupParent(cfg.TenantCgroupParent), docker.WithPullAuthorization(pullAuthorization(registry.auth, cfg.ConfigManager)))
	if err != nil {
		return nil, err
	}
	rootfsQuota, err := service.CheckDockerHost(context.Background(), dockerAdapter, service.DockerDataDir{Root: cfg.DockerDataRoot, Path: dockerDataPath}, appLogger)
	if err != nil {
		return nil, err
	}

	contRepo := repository.NewContainerRepository(db, lockDB)
	volRepo := repository.NewVolumeRepository(db)
	// Quota reads made under the owner capacity lock use the query pool.
	userQuotas, err := userquota.New(repository.NewUserQuotaRepository(db), cfg.ConfigManager)
	if err != nil {
		return nil, err
	}
	buildRepo := repository.NewBuildRepository(db, lockDB)
	projRepo := repository.NewProjectRepository(db)
	stagedRepo := repository.NewStagedObjectRepository(db)
	resourceLifecycleRepo := repository.NewResourceLifecycleRepository(db)
	reportSnapshotRepo := repository.NewReportSnapshotRepository(db, dockerAdapter, logger)
	reportsRepo := clickhouserepo.NewReportsRepository(clickhouserepo.Config{
		Addr:     cfg.ClickHouse.Addr,
		Database: cfg.ClickHouse.Database,
		Username: cfg.ClickHouse.Username,
		Password: cfg.ClickHouse.Password,
	})

	metricsProvider := metrics.NewSystemMetrics()
	volumeSizeEnforcement := service.CheckVolumeSizeEnforcement(context.Background(), dockerAdapter, cfg.ConfigManager, appLogger)
	service.ReportHostSettingConflicts(context.Background(), metricsProvider, cfg.ConfigManager.Get(), appLogger)

	contService, err := service.NewContainerService(service.ContainerServiceDeps{
		Repository: contRepo, Volumes: volRepo,
		Images: imgRepo, Docker: dockerAdapter,
		Metrics: metricsProvider, DiskMetrics: metricsProvider, Config: cfg.ConfigManager,
		Quotas: userQuotas, Builds: buildRepo, DockerDataPath: dockerDataPath,
		Logger: logger,
	})
	if err != nil {
		return nil, err
	}
	volService, err := service.NewVolumeService(service.VolumeServiceDeps{
		Repository:     volRepo,
		Docker:         dockerAdapter,
		Config:         cfg.ConfigManager,
		Lifecycle:      resourceLifecycleRepo,
		Quotas:         userQuotas,
		ImageRepo:      imgRepo,
		DiskMetrics:    metricsProvider,
		CapacityLock:   contRepo,
		SizeMode:       volumeSizeEnforcement,
		DockerDataPath: dockerDataPath,
		Logger:         logger,
	})
	if err != nil {
		return nil, err
	}
	imgService, err := service.NewImageService(service.ImageServiceDeps{
		Repository:   imgRepo,
		Docker:       dockerAdapter,
		Registry:     registry.adapter,
		Containers:   contRepo,
		Config:       cfg.ConfigManager,
		Lifecycle:    resourceLifecycleRepo,
		RegistryAuth: registry.auth,
	})
	if err != nil {
		return nil, err
	}
	objectStore := objectstorage.NewLazyStorage(cfg.ObjectStorage)
	buildService, err := service.NewBuildService(service.BuildServiceDeps{
		Repository:     buildRepo,
		Images:         imgRepo,
		Registry:       registry.adapter,
		Docker:         dockerAdapter,
		Quotas:         userQuotas,
		Logger:         logger,
		RegistryAuth:   registry.auth,
		VolumeRepo:     volRepo,
		Config:         cfg.ConfigManager,
		ObjectStore:    objectStore,
		StagedObjects:  stagedRepo,
		Containers:     contRepo,
		CapacityLock:   contRepo,
		DiskMetrics:    metricsProvider,
		HostMetrics:    metricsProvider,
		DockerDataPath: dockerDataPath,
	})
	if err != nil {
		return nil, err
	}

	resourceRepo := &projectResourceRepo{contRepo, volRepo}
	projService, err := service.NewProjectService(service.ProjectServiceDeps{
		Repository: projRepo,
		Resources:  resourceRepo,
		Docker:     dockerAdapter,
		Containers: contService,
		Volumes:    volService,
		Capacity:   contService,
		Config:     cfg.ConfigManager,
		Logger:     logger,
	})
	if err != nil {
		return nil, err
	}
	contService.SetProjectStatusUpdater(projService)
	systemService, err := service.NewSystemService(service.SystemServiceDeps{
		Config: cfg.ConfigManager, VolumeSizeSupport: volumeSizeEnforcement, HostCapacity: metricsProvider,
	})
	if err != nil {
		return nil, err
	}
	reportService := service.NewReportService(reportsRepo, reportSnapshotRepo, ssoClient, cfg.ConfigManager, logger)
	contService.SetAuditRecorder(reportService)
	outboxPublisher := rabbitmq.NewPublisher(cfg.RabbitMQURL, rabbitmq.Topology{})
	composeConsumer, err := platformrabbitmq.NewConsumer(rabbitmq.ComposeConsumerConfig(cfg.RabbitMQURL, "core", rabbitmq.Topology{}, logger))
	if err != nil {
		return nil, err
	}
	containerConsumer, err := platformrabbitmq.NewConsumer(rabbitmq.ContainerConsumerConfig(cfg.RabbitMQURL, "core", rabbitmq.Topology{}, logger))
	if err != nil {
		return nil, err
	}
	resourceConsumer, err := platformrabbitmq.NewConsumer(rabbitmq.ResourceConsumerConfig(cfg.RabbitMQURL, "core", rabbitmq.Topology{}, logger))
	if err != nil {
		return nil, err
	}

	authenticator, err := internalauth.NewAuthenticator(coregrpc.CallerPolicies(coregrpc.CallerCredentials{
		Gateway: cfg.GatewayToken, Builder: cfg.BuilderToken,
	})...)
	if err != nil {
		return nil, err
	}
	gRPCServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		logging.UnaryServerInterceptor(logger),
		authenticator.UnaryServerInterceptor(),
	))

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	composePlanAdmission, err := service.NewComposePlanAdmission(service.ComposePlanAdmissionDeps{
		Containers:     contRepo,
		Volumes:        volRepo,
		Builds:         buildRepo,
		Images:         imgRepo,
		Deployments:    projRepo,
		Quotas:         userQuotas,
		HostMetrics:    metricsProvider,
		DiskMetrics:    metricsProvider,
		DockerDataPath: dockerDataPath,
		Config:         cfg.ConfigManager,
		Logger:         logger,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	orchestrator, err := compose.NewOrchestrator(compose.OrchestratorDeps{
		Projects:       projRepo,
		PlanAdmission:  composePlanAdmission,
		Config:         cfg.ConfigManager,
		Builds:         buildRepo,
		Resources:      resourceRepo,
		Volumes:        volService,
		Containers:     contService,
		ObjectStore:    objectStore,
		BuildJobs:      buildService,
		Images:         imgService,
		StagedObjects:  stagedRepo,
		CapacityLock:   contRepo,
		ReservedDisk:   service.NewReservedDiskReader(imgRepo, volRepo),
		DiskMetrics:    metricsProvider,
		DockerDataPath: dockerDataPath,
		Quotas:         userQuotas,
		Logger:         logger,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	if err := recoverInterruptedWork(ctx, orchestrator, projRepo, resourceLifecycleRepo, contRepo, cfg.ConfigManager, appLogger); err != nil {
		cancel()
		return nil, err
	}
	buildService.SetDeploymentCanceler(orchestrator)
	projService.SetDeploymentCanceler(orchestrator)
	composeHandler := corehttp.NewComposeHandler(orchestrator, cfg.ConfigManager)
	buildHandler := corehttp.NewBuildHandler(buildService, cfg.ConfigManager)
	streamService, err := streams.NewService(streams.Deps{
		Targets:             contService,
		Docker:              dockerAdapter,
		Config:              cfg.ConfigManager,
		MaxLogStreams:       cfg.Streams.MaxLogStreams,
		MaxTerminalSessions: cfg.Streams.MaxTerminalSessions,
		Logger:              logger,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	streamHandler := corehttp.NewStreamHandler(streamService, logger)
	router := corehttp.SetupRouter(composeHandler, buildHandler, streamHandler, authenticator, logger)

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           router,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		MaxHeaderBytes:    cfg.HTTP.MaxHeaderBytes,
	}
	// Shutdown neither cancels open log streams nor sees hijacked terminal
	// connections; the stream handler ends both when shutdown begins.
	httpServer.RegisterOnShutdown(streamHandler.Shutdown)

	coregrpc.RegisterContainerAPI(gRPCServer, contService, ssoClient)
	coregrpc.RegisterVolumeAPI(gRPCServer, volService, ssoClient)
	coregrpc.RegisterImageAPI(gRPCServer, imgService, buildService, ssoClient)
	coregrpc.RegisterProjectAPI(gRPCServer, projService, ssoClient)
	coregrpc.RegisterSystemAPI(gRPCServer, systemService)
	coregrpc.RegisterReportAPI(gRPCServer, reportService)
	coregrpc.RegisterQuotaAPI(gRPCServer, userQuotas)
	workloadSuspension, err := service.NewWorkloadSuspension(service.WorkloadSuspensionDeps{
		Containers:         contRepo,
		Builds:             buildRepo,
		BuildCanceler:      buildService,
		Deployments:        projRepo,
		DeploymentCanceler: orchestrator,
		Logger:             logger,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	coregrpc.RegisterWorkloadAPI(gRPCServer, workloadSuspension)

	ttlWorker := service.NewTTLWorker(contRepo, cfg.ConfigManager, logger)
	eventWorker := service.NewEventWorker(contRepo, volRepo, dockerAdapter, projService, cfg.ConfigManager, logger)
	gcWorker, err := service.NewGCWorker(service.GCWorkerDeps{
		Docker: dockerAdapter, Images: buildService, Builds: buildRepo, RegistryGC: repository.NewRegistryGCRepository(db),
		StagedObjects: stagedRepo, Objects: objectStore, Config: cfg.ConfigManager, Logger: logger,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	volumeUsageWorker := service.NewVolumeUsageWorker(volRepo, contRepo, dockerAdapter, cfg.ConfigManager, logger)
	memoryPressureWorker, err := service.NewMemoryPressureWorker(service.MemoryPressureWorkerDeps{
		Repository:   contRepo,
		Docker:       dockerAdapter,
		Metrics:      metricsProvider,
		Builds:       buildRepo,
		BuildStopper: buildService,
		Quotas:       userQuotas,
		Config:       cfg.ConfigManager,
		Logger:       logger,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	statsService, err := service.NewStatsService(service.StatsServiceDeps{
		Containers: contRepo, Volumes: volRepo, Images: imgRepo, Builds: buildRepo, Projects: projRepo,
		Metrics: metricsProvider, DockerDataPath: dockerDataPath, Config: cfg.ConfigManager, Quotas: userQuotas,
		Borrowed: memoryPressureWorker, RootfsQuota: rootfsQuota, VolumeSizeMode: volumeSizeEnforcement,
		Logger: logger,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	coregrpc.RegisterStatsAPI(gRPCServer, statsService)
	containerCreateWorker := service.NewContainerCreateWorker(contService, cfg.ConfigManager, logger)
	resourceLifecycleWorker := service.NewResourceLifecycleWorker(volService, imgService, cfg.ConfigManager, logger)
	composeCoordinator, err := compose.NewDeploymentCoordinator(compose.DeploymentCoordinatorDeps{
		Deployments:       projRepo,
		Builds:            buildRepo,
		Builder:           compose.NewLocalBuilderClient(objectStore, buildService),
		Containers:        contService,
		ProjectContainers: resourceRepo,
		Volumes:           volService,
		Docker:            dockerAdapter,
		Images:            imgService,
		Sources:           orchestrator,
		Config:            cfg.ConfigManager,
		Auditor:           reportService,
		Users:             ssoClient,
		Logger:            logger,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	reportsUsageWorker := service.NewReportsUsageWorker(reportService, logger)
	retentionWorker := service.NewRetentionWorker(repository.NewRetentionRepository(db), logger)
	operationRecoveryWorker, err := service.NewOperationRecoveryWorker(service.OperationRecoveryWorkerDeps{
		Containers:  contRepo,
		Resources:   resourceLifecycleRepo,
		Deployments: projRepo,
		Config:      cfg.ConfigManager,
		Logger:      logger,
	})
	if err != nil {
		cancel()
		return nil, err
	}

	startWorkers(ctx, wg,
		func(ctx context.Context) {
			pgpool.LogStats(ctx, appLogger, map[string]*pgxpool.Pool{"queries": db, "locks": lockDB})
		},
		registry.cleanup.Run,
		ttlWorker.Run,
		eventWorker.Run,
		gcWorker.Run,
		volumeUsageWorker.Run,
		memoryPressureWorker.Run,
		composeCoordinator.Run,
		projService.RunLifecycleCoordinator,
		reportsUsageWorker.Run,
		retentionWorker.Run,
		operationRecoveryWorker.Run,
	)
	startWorkers(ctx, wg, outboxWorkers(outboxPublisher, buildRepo, projRepo, contRepo, resourceLifecycleRepo, cfg.ConfigManager, logger)...)
	composeConsumer.Run(ctx, cfg.ConfigManager.Get().ComposeDeployWorkerCount, orchestrator.HandleDeploymentMessage)
	containerConsumer.Run(ctx, cfg.ConfigManager.Get().ContainerCreateWorkerCount, containerCreateWorker.HandleMessage)
	resourceConsumer.Run(ctx, cfg.ConfigManager.Get().ContainerCreateWorkerCount, resourceLifecycleWorker.HandleMessage)

	initialized = true
	return &App{
		gRPCServer:        gRPCServer,
		httpServer:        httpServer,
		httpShutdown:      cfg.HTTP.ShutdownTimeout,
		registryServer:    registry.edge,
		db:                db,
		lockDB:            lockDB,
		reportsRepo:       reportsRepo,
		dockerCli:         dockerAdapter,
		orchestrator:      orchestrator,
		outboxPublisher:   outboxPublisher,
		composeConsumer:   composeConsumer,
		containerConsumer: containerConsumer,
		resourceConsumer:  resourceConsumer,
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
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := a.registryServer.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.logger.Error("core registry edge failed", "error", err)
			a.cancel()
		}
	}()
	go func() {
		a.logger.Info("core http server starting", "port", a.httpServer.Addr)
		if err := a.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	if a.registryServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := a.registryServer.Shutdown(shutdownCtx); err != nil {
			a.logger.Warn("core registry edge shutdown failed", "error", err)
			if err := a.registryServer.Close(); err != nil {
				a.logger.Warn("core registry edge close failed", "error", err)
			}
		}
		cancel()
	}
	if a.httpServer != nil {
		timeout := a.httpShutdown
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
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
	if a.resourceConsumer != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := a.resourceConsumer.Stop(stopCtx); err != nil {
			a.logger.Warn("resource queue consumer shutdown timed out", "error", err)
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
	if a.lockDB != nil {
		a.lockDB.Close()
	}
	if a.db != nil {
		a.db.Close()
	}
	if a.reportsRepo != nil {
		if err := a.reportsRepo.Close(); err != nil {
			a.logger.Warn("clickhouse reports repository close failed", "error", err)
		}
	}
	if a.dockerCli != nil {
		if err := a.dockerCli.Close(); err != nil {
			a.logger.Warn("docker client close failed", "error", err)
		}
	}
	if a.outboxPublisher != nil {
		if err := a.outboxPublisher.Close(); err != nil {
			a.logger.Warn("rabbitmq publisher close failed", "error", err)
		}
	}
	if a.ssoConn != nil {
		if err := a.ssoConn.Close(); err != nil {
			a.logger.Warn("sso connection close failed", "error", err)
		}
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
