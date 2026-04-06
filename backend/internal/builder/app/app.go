package app

import (
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/grpc/client"
	deliveryhttp "github.com/callmerussell04/docker-cloud-manager/internal/builder/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/archive"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/storage"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/service"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type App struct {
	router *gin.Engine
	port   int
}

func New(port int, coreTarget string, config service.BuilderConfig, maxUnpackedSize int64, maxLogSize int64, storagePath string) (*App, error) {
	coreConn, err := grpc.NewClient(coreTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("core conn fail: %w", err)
	}

	coreClient := grpcclient.NewCoreClient(coreConn)

	fileManager, err := storage.NewFileManager(storagePath)
	if err != nil {
		return nil, err
	}

	logManager, err := storage.NewLogManager(config.LogsDirPath, maxLogSize)
	if err != nil {
		return nil, err
	}

	extractor := archive.NewExtractor(maxUnpackedSize)

	dockerAdapter, err := docker.NewAdapter()
	if err != nil {
		return nil, err
	}

	builderService := service.NewBuilderService(fileManager, extractor, dockerAdapter, logManager, coreClient, config)
	buildHandler := handler.NewBuildHandler(builderService, config.LogsDirPath)

	router := deliveryhttp.NewRouter(buildHandler)

	return &App{
		router: router,
		port:   port,
	}, nil
}

func (a *App) Run() error {
	return a.router.Run(fmt.Sprintf(":%d", a.port))
}
