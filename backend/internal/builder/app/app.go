package app

import (
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/grpc/client"
	deliveryhttp "github.com/callmerussell04/docker-cloud-manager/internal/builder/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/archive"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/storage"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/service"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type App struct {
	router *gin.Engine
	port   int
}

func New(port int, coreTarget string, internalToken string, cfg config.BuilderConfig, maxUnpackedSize int64, maxLogSize int64, storagePath string) (*App, error) {
	coreConn, err := grpc.NewClient(
		coreTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(internalauth.UnaryClientInterceptor(internalToken)),
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

	builderService := service.NewBuilderService(fileManager, extractor, dockerAdapter, logManager, coreClient, cfg)
	buildHandler := handler.NewBuildHandler(builderService, cfg.LogsDirPath)

	router := deliveryhttp.NewRouter(buildHandler, internalToken)

	return &App{
		router: router,
		port:   port,
	}, nil
}

func (a *App) Run() error {
	return a.router.Run(fmt.Sprintf(":%d", a.port))
}
