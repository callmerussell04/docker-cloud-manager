package app

import (
	"fmt"

	deliveryhttp "github.com/callmerussell04/docker-cloud-manager/internal/builder/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/storage"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/service"
	"github.com/gin-gonic/gin"
)

type App struct {
	router *gin.Engine
	port   int
}

func New(port int, storagePath string) (*App, error) {
	fileManager, err := storage.NewFileManager(storagePath)
	if err != nil {
		return nil, err
	}

	builderService := service.NewBuilderService(fileManager)
	buildHandler := handler.NewBuildHandler(builderService)

	router := deliveryhttp.NewRouter(buildHandler)

	return &App{
		router: router,
		port:   port,
	}, nil
}

func (a *App) Run() error {
	return a.router.Run(fmt.Sprintf(":%d", a.port))
}
