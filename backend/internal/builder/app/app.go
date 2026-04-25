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
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/logging"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type App struct {
	router *gin.Engine
	port   int
	logger *slog.Logger
}

func New(port int, coreTarget string, internalToken string, cfg config.BuilderConfig, maxUnpackedSize int64, maxLogSize int64, storagePath string, logger *slog.Logger) (*App, error) {
	coreConn, err := grpc.NewClient(
		coreTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(
			logging.UnaryClientInterceptor(logger),
			internalauth.UnaryClientInterceptor(internalToken),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("core conn fail: %w", err)
	}

	coreClient := grpcclient.NewCoreClient(coreConn)

	//TODO: make it an env
	maxArchiveSize := int64(50 << 20) // 50 MB
	fileManager, err := storage.NewFileManager(storagePath, maxArchiveSize)
	if err != nil {
		return nil, err
	}

	logManager, err := storage.NewLogManager(cfg.LogsDirPath, maxLogSize)
	if err != nil {
		return nil, err
	}

	extractor := archive.NewExtractor(maxUnpackedSize)

	dockerAdapter, err := docker.NewAdapter()
	if err != nil {
		return nil, err
	}

	builderService := service.NewBuilderService(fileManager, extractor, dockerAdapter, logManager, coreClient, cfg, logger)
	buildHandler := handler.NewBuildHandler(builderService, cfg.LogsDirPath)

	router := deliveryhttp.NewRouter(buildHandler, internalToken, logger)

	return &App{
		router: router,
		port:   port,
		logger: logging.WithComponent(logger, "app"),
	}, nil
}

func (a *App) Run() error {
	a.logger.Info("builder server starting", "port", a.port)
	return a.router.Run(fmt.Sprintf(":%d", a.port))
}
