package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/grpc/client"
	deliveryhttp "github.com/callmerussell04/docker-cloud-manager/internal/builder/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/archive"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/objectstorage"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/storage"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/service"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type App struct {
	router         *gin.Engine
	httpServer     *http.Server
	coreConn       *grpc.ClientConn
	dockerAdapter  *docker.Adapter
	builderService *service.BuilderService
	queueConsumer  *rabbitmq.Consumer
	port           int
	ctx            context.Context
	cancel         context.CancelFunc
	workerCount    int
	logger         *slog.Logger
}

type Config struct {
	Port          int
	CoreTarget    string
	InternalToken string
	Builder       config.BuilderConfig
	StoragePath   string
	RabbitMQURL   string
	ObjectStorage objectstorage.Config
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
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

	coreClient := grpcclient.NewCoreClient(coreConn)
	runtimeConfig := config.NewRuntimeManager(cfg.Builder, coreClient, logging.WithComponent(logger, "builder_config"))

	fileManager, err := storage.NewFileManager(cfg.StoragePath)
	if err != nil {
		return nil, err
	}

	logManager, err := storage.NewLogManager(cfg.Builder.LogsDirPath)
	if err != nil {
		return nil, err
	}
	objectStore, err := objectstorage.NewMinIOStorage(context.Background(), cfg.ObjectStorage)
	if err != nil {
		return nil, err
	}

	extractor := archive.NewExtractor()

	dockerAdapter, err := docker.NewAdapter()
	if err != nil {
		return nil, err
	}
	if err := dockerAdapter.CleanupOrphanBuildContainers(context.Background()); err != nil {
		logging.WithComponent(logger, "app").Warn("failed to cleanup orphan build containers", "error", err)
	}

	builderService := service.NewBuilderService(fileManager, extractor, dockerAdapter, logManager, objectStore, coreClient, runtimeConfig, logger)
	buildHandler := handler.NewBuildHandler(builderService, cfg.Builder.LogsDirPath, runtimeConfig)

	router := deliveryhttp.NewRouter(buildHandler, cfg.InternalToken, logger)
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: router,
	}
	ctx, cancel := context.WithCancel(context.Background())
	queueConsumer := rabbitmq.NewConsumer(cfg.RabbitMQURL, logger)

	return &App{
		router:         router,
		httpServer:     httpServer,
		coreConn:       coreConn,
		dockerAdapter:  dockerAdapter,
		builderService: builderService,
		queueConsumer:  queueConsumer,
		port:           cfg.Port,
		ctx:            ctx,
		cancel:         cancel,
		workerCount:    cfg.Builder.MaxConcurrentBuilds,
		logger:         logging.WithComponent(logger, "app"),
	}, nil
}

func (a *App) Run() error {
	a.logger.Info("builder server starting", "port", a.port)
	if a.queueConsumer != nil {
		a.queueConsumer.Run(a.ctx, a.workerCount, a.builderService.HandleBuildMessage)
	}
	if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (a *App) Stop(ctx context.Context) {
	a.logger.Info("builder application stopping")
	if a.cancel != nil {
		a.cancel()
	}
	if a.builderService != nil {
		stopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_ = a.builderService.Stop(stopCtx)
		cancel()
	}
	if a.httpServer != nil {
		_ = a.httpServer.Shutdown(ctx)
	}
	if a.coreConn != nil {
		_ = a.coreConn.Close()
	}
	if a.dockerAdapter != nil {
		_ = a.dockerAdapter.Close()
	}
}
