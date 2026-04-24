package app

import (
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/grpc/client"
	httprouter "github.com/callmerussell04/docker-cloud-manager/internal/gateway/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/service"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
)

type App struct {
	router *gin.Engine
	port   int
}

func New(port int, ssoTarget, coreTarget, builderHttpTarget, coreHttpTarget string, internalToken string) (*App, error) {
	//TODO: fix insecure connection and overall grpc client execution
	ssoConn, err := grpc.NewClient(
		ssoTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(internalauth.UnaryClientInterceptor(internalToken)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to sso service: %w", err)
	}

	//TODO: fix insecure connection and overall grpc client execution
	coreConn, err := grpc.NewClient(
		coreTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(internalauth.UnaryClientInterceptor(internalToken)),
	)
	if err != nil {
		return nil, fmt.Errorf("core conn fail: %w", err)
	}

	ssoClient := grpcclient.NewSSOClient(ssoConn)
	authService := service.NewAuth(ssoClient)
	authHandler := handler.NewAuthHandler(authService)

	coreClient := grpcclient.NewCoreClient(coreConn)
	coreService := service.NewCore(coreClient)
	coreHandler := handler.NewCoreHandler(coreService)

	builderProxy, err := handler.NewBuilderProxyHandler(builderHttpTarget, internalToken)
	if err != nil {
		return nil, fmt.Errorf("builder proxy setup fail: %w", err)
	}

	builderLogsProxy, err := handler.NewAuthorizedBuilderLogsProxy(builderHttpTarget, coreService, internalToken)
	if err != nil {
		return nil, fmt.Errorf("builder logs proxy setup fail: %w", err)
	}

	coreProxy, err := handler.NewCoreProxyHandler(coreHttpTarget, internalToken)
	if err != nil {
		return nil, fmt.Errorf("core proxy setup fail: %w", err)
	}

	router := httprouter.NewRouter(authHandler, coreHandler, builderProxy, builderLogsProxy, coreProxy, authService)

	return &App{
		router: router,
		port:   port,
	}, nil

}

func (a *App) Run() error {
	log.Printf("Gateway server is running on port %d", a.port)
	return a.router.Run(fmt.Sprintf(":%d", a.port))
}
