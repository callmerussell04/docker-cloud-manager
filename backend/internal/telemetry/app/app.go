package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	telemetrygrpc "github.com/callmerussell04/docker-cloud-manager/internal/telemetry/grpc/client"
	telemetryhttp "github.com/callmerussell04/docker-cloud-manager/internal/telemetry/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/telemetry/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/telemetry/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type Config struct {
	Port          int
	CoreTarget    string
	InternalToken string
	MaxLogStreams int
	MaxTerminals  int
	HTTP          HTTPServerConfig
	GRPC          GRPCConfig
}

type HTTPServerConfig struct {
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

type GRPCConfig struct {
	RequestTimeout    time.Duration
	MinConnectTimeout time.Duration
}

type App struct {
	router          *gin.Engine
	server          *http.Server
	coreConn        *grpc.ClientConn
	docker          *docker.Adapter
	shutdownTimeout time.Duration
	logger          *slog.Logger
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	coreConn, err := grpc.NewClient(cfg.CoreTarget, grpcDialOptions(cfg, logger)...)
	if err != nil {
		return nil, fmt.Errorf("failed to create core grpc client: %w", err)
	}

	dockerAdapter, err := docker.NewAdapter()
	if err != nil {
		_ = coreConn.Close()
		return nil, err
	}

	coreClient := telemetrygrpc.NewCoreClient(coreConn)
	telemetryService := service.NewTelemetry(coreClient, dockerAdapter, cfg.MaxLogStreams, cfg.MaxTerminals, logger)
	handler := telemetryhttp.NewHandler(telemetryService, logger)
	router := telemetryhttp.NewRouter(handler, cfg.InternalToken, logger)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		MaxHeaderBytes:    cfg.HTTP.MaxHeaderBytes,
	}

	return &App{
		router:          router,
		server:          server,
		coreConn:        coreConn,
		docker:          dockerAdapter,
		shutdownTimeout: cfg.HTTP.ShutdownTimeout,
		logger:          logging.WithComponent(logger, "app"),
	}, nil
}

func (a *App) Run() error {
	a.logger.Info("telemetry server starting", "addr", a.server.Addr)

	runErr := make(chan error, 1)
	go func() {
		err := a.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runErr <- err
			return
		}
		runErr <- nil
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-runErr:
		_ = a.close()
		return err
	case <-ctx.Done():
		a.logger.Info("telemetry shutdown signal received")
	}

	timeout := a.shutdownTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := a.server.Shutdown(shutdownCtx)
	closeErr := a.close()
	if err != nil {
		return fmt.Errorf("telemetry shutdown failed: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}
	a.logger.Info("telemetry server stopped")
	return nil
}

func (a *App) close() error {
	var err error
	if a.coreConn != nil {
		err = errors.Join(err, a.coreConn.Close())
	}
	if a.docker != nil {
		err = errors.Join(err, a.docker.Close())
	}
	return err
}

func grpcDialOptions(cfg Config, logger *slog.Logger) []grpc.DialOption {
	timeoutInterceptor := unaryTimeoutInterceptor(cfg.GRPC.RequestTimeout)
	minConnectTimeout := cfg.GRPC.MinConnectTimeout
	if minConnectTimeout <= 0 {
		minConnectTimeout = 5 * time.Second
	}

	return []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  1 * time.Second,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   30 * time.Second,
			},
			MinConnectTimeout: minConnectTimeout,
		}),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithChainUnaryInterceptor(
			timeoutInterceptor,
			logging.UnaryClientInterceptor(logger),
			internalauth.UnaryClientInterceptor(cfg.InternalToken),
		),
	}
}

func unaryTimeoutInterceptor(timeout time.Duration) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if timeout <= 0 {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
