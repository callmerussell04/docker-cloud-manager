package app

import (
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/grpc/client"
	httprouter "github.com/callmerussell04/docker-cloud-manager/internal/gateway/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/service"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
)

type App struct {
	router *gin.Engine
	port   int
	logger *slog.Logger
}

type Config struct {
	Port              int
	SSOTarget         string
	CoreTarget        string
	BuilderHTTPTarget string
	CoreHTTPTarget    string
	InternalToken     string
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	//TODO: fix insecure connection and overall grpc client execution
	ssoConn, err := grpc.NewClient(
		cfg.SSOTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(
			logging.UnaryClientInterceptor(logger),
			internalauth.UnaryClientInterceptor(cfg.InternalToken),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to sso service: %w", err)
	}

	//TODO: fix insecure connection and overall grpc client execution
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

	ssoClient := grpcclient.NewSSOClient(ssoConn)
	authService := service.NewAuth(ssoClient)
	authHandler := handler.NewAuthHandler(authService)

	coreClient := grpcclient.NewCoreClient(coreConn)
	coreService := service.NewCore(coreClient)
	coreHandler := handler.NewCoreHandler(coreService)

	builderProxy, err := handler.NewBuilderProxyHandler(cfg.BuilderHTTPTarget, cfg.InternalToken)
	if err != nil {
		return nil, fmt.Errorf("builder proxy setup fail: %w", err)
	}

	builderLogsProxy, err := handler.NewAuthorizedBuilderLogsProxy(cfg.BuilderHTTPTarget, coreService, cfg.InternalToken)
	if err != nil {
		return nil, fmt.Errorf("builder logs proxy setup fail: %w", err)
	}

	coreProxy, err := handler.NewCoreProxyHandler(cfg.CoreHTTPTarget, cfg.InternalToken)
	if err != nil {
		return nil, fmt.Errorf("core proxy setup fail: %w", err)
	}

	router := httprouter.NewRouter(authHandler, coreHandler, builderProxy, builderLogsProxy, coreProxy, authService, logger)

	return &App{
		router: router,
		port:   cfg.Port,
		logger: logging.WithComponent(logger, "app"),
	}, nil

}

func (a *App) Run() error {
	a.logger.Info("gateway server starting", "port", a.port)
	return a.router.Run(fmt.Sprintf(":%d", a.port))
}
