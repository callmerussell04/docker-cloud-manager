package app

import (
	"fmt"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/grpc/client"
	deliveryhttp "github.com/callmerussell04/docker-cloud-manager/internal/builder/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/archive"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/storage"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/service"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type App struct {
	router *gin.Engine
	port   int
	logger *slog.Logger
}

type Config struct {
	Port            int
	CoreTarget      string
	InternalToken   string
	Builder         config.BuilderConfig
	MaxUnpackedSize int64
	MaxLogSize      int64
	StoragePath     string
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

	//TODO: make it an env
	maxArchiveSize := int64(50 << 20) // 50 MB
	fileManager, err := storage.NewFileManager(cfg.StoragePath, maxArchiveSize)
	if err != nil {
		return nil, err
	}

	logManager, err := storage.NewLogManager(cfg.Builder.LogsDirPath, cfg.MaxLogSize)
	if err != nil {
		return nil, err
	}

	extractor := archive.NewExtractor(cfg.MaxUnpackedSize)

	dockerAdapter, err := docker.NewAdapter()
	if err != nil {
		return nil, err
	}

	builderService := service.NewBuilderService(fileManager, extractor, dockerAdapter, logManager, coreClient, cfg.Builder, logger)
	buildHandler := handler.NewBuildHandler(builderService, cfg.Builder.LogsDirPath)

	router := deliveryhttp.NewRouter(buildHandler, cfg.InternalToken, logger)

	return &App{
		router: router,
		port:   cfg.Port,
		logger: logging.WithComponent(logger, "app"),
	}, nil
}

func (a *App) Run() error {
	a.logger.Info("builder server starting", "port", a.port)
	return a.router.Run(fmt.Sprintf(":%d", a.port))
}
