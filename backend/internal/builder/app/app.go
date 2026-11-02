package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/grpc/client"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/archive"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/storage"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	platformrabbitmq "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type App struct {
	coreConn       *grpc.ClientConn
	dockerAdapter  *docker.Adapter
	builderService *service.BuilderService
	queueConsumer  *platformrabbitmq.Consumer[buildqueue.ImageBuildMessage]
	ctx            context.Context
	cancel         context.CancelFunc
	workerCount    int
	logger         *slog.Logger
}

func New(cfg config.Bootstrap, logger *slog.Logger) (*App, error) {
	if cfg.InstanceID == "" {
		if hostname, err := os.Hostname(); err == nil {
			cfg.InstanceID = hostname
		}
	}

	coreConn, err := grpc.NewClient(
		cfg.CoreTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(
			logging.UnaryClientInterceptor(logger),
			internalauth.UnaryClientInterceptor(cfg.InternalToken),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("core conn fail: %w", err)
	}

	coreClient := client.NewCoreClient(coreConn)
	runtimeConfig := config.NewRuntimeManager(cfg.Builder, coreClient, logging.WithComponent(logger, "builder_config"))

	fileManager, err := storage.NewFileManager(cfg.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create build archive storage: %w", err)
	}

	logManager, err := storage.NewLogManager(cfg.Builder.LogsDirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create build log storage: %w", err)
	}
	workspaceManager, err := storage.NewWorkspaceManager(cfg.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create build workspace storage: %w", err)
	}
	results, err := storage.NewResultStore(cfg.StoragePath)
	if err != nil {
		return nil, err
	}
	objectStore := objectstorage.NewLazyStorage(cfg.ObjectStorage)

	extractor := archive.NewExtractor()

	dockerAdapter, err := docker.NewAdapter(docker.Config{RegistryCAFile: cfg.RegistryCAFile, RequireNetworkPolicy: true})
	if err != nil {
		return nil, fmt.Errorf("failed to create docker adapter: %w", err)
	}
	if err := dockerAdapter.CleanupOrphanBuildContainers(context.Background()); err != nil {
		logging.WithComponent(logger, "app").Warn("failed to cleanup orphan build containers", "error", err)
	}

	builderService, err := service.NewBuilderService(service.BuilderServiceDeps{
		Files:            fileManager,
		Workspaces:       workspaceManager,
		Extractor:        extractor,
		Docker:           dockerAdapter,
		NetworkAdmission: dockerAdapter,
		Logs:             logManager,
		ObjectStore:      objectStore,
		Core:             coreClient,
		Results:          results,
		Config:           runtimeConfig,
		Logger:           logger.With("builder_instance_id", cfg.InstanceID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create builder service: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	queueConsumer := platformrabbitmq.NewConsumer(rabbitmq.BuildConsumerConfig(cfg.RabbitMQURL, cfg.InstanceID, logger))

	return &App{
		coreConn:       coreConn,
		dockerAdapter:  dockerAdapter,
		builderService: builderService,
		queueConsumer:  queueConsumer,
		ctx:            ctx,
		cancel:         cancel,
		workerCount:    cfg.Builder.MaxConcurrentBuilds,
		logger:         logging.WithComponent(logger, "app"),
	}, nil
}

func (a *App) Run() error {
	a.logger.Info("builder worker starting")
	if a.queueConsumer != nil {
		a.queueConsumer.Run(a.ctx, a.workerCount, a.builderService.HandleBuildMessage)
	}
	<-a.ctx.Done()
	return nil
}

func (a *App) Stop(ctx context.Context) {
	a.logger.Info("builder application stopping")
	if a.cancel != nil {
		a.cancel()
	}
	if a.queueConsumer != nil {
		stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := a.queueConsumer.Stop(stopCtx); err != nil {
			a.logger.Error("failed to stop build queue consumer", "error", err)
		}
		cancel()
	}
	if a.builderService != nil {
		stopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := a.builderService.Stop(stopCtx); err != nil {
			a.logger.Error("failed to stop builder service", "error", err)
		}
		cancel()
	}
	if a.coreConn != nil {
		if err := a.coreConn.Close(); err != nil {
			a.logger.Error("failed to close core connection", "error", err)
		}
	}
	if a.dockerAdapter != nil {
		if err := a.dockerAdapter.Close(); err != nil {
			a.logger.Error("failed to close docker adapter", "error", err)
		}
	}
}
