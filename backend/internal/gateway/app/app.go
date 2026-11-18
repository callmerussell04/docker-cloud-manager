package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/grpc/client"
	gatewayhttp "github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
)

type App struct {
	router          *gin.Engine
	server          *http.Server
	port            int
	shutdownTimeout time.Duration
	logger          *slog.Logger
	ssoConn         *grpc.ClientConn
	coreConn        *grpc.ClientConn
}

func New(cfg config.Bootstrap, logger *slog.Logger) (*App, error) {
	clients, err := newGRPCClients(cfg, logger)
	if err != nil {
		return nil, err
	}

	telemetryTickets := service.NewTelemetryTicketStore(cfg.TelemetryTicketTTL)
	proxies, err := newProxyHandlers(cfg, telemetryTickets)
	if err != nil {
		_ = clients.close() // startup already failed; the init error is returned
		return nil, err
	}

	cfg.Router.AuthRedirect.CookieAllowedOrigins = cfg.Router.CORSAllowedOrigins
	healthHandler := handler.NewHealthHandler(newReadiness(cfg, clients))
	router, err := gatewayhttp.NewRouter(cfg.Router, gatewayhttp.RouterDeps{
		AuthHandler: handler.NewAuthHandler(clients.ssoClient, clients.coreClient, cfg.Cookie, cfg.Router.AuthRedirect),
		CoreHandler: handler.NewCoreHandler(clients.coreClient),
		UserHandler: handler.NewUserManagementHandler(handler.UserManagementDeps{
			Users:        clients.ssoClient,
			AdminUsers:   service.NewAdminUsers(clients.ssoClient, clients.coreClient),
			Deactivation: service.NewUserDeactivation(clients.ssoClient, clients.coreClient),
			Audit:        clients.coreClient,
		}),
		HealthHandler:               healthHandler,
		TelemetryTicketHandler:      handler.NewTelemetryTicketHandler(telemetryTickets),
		CoreHTTPProxy:               proxies.coreHTTP,
		TelemetryLogsProxy:          proxies.telemetryLogs,
		TelemetryTerminalProxy:      proxies.telemetryTerminal,
		AdminTelemetryLogsProxy:     proxies.adminTelemetryLogs,
		AdminTelemetryTerminalProxy: proxies.adminTelemetryTerminal,
		TokenVerifier:               clients.ssoClient,
		Logger:                      logger,
	})
	if err != nil {
		_ = clients.close() // startup already failed; the init error is returned
		return nil, fmt.Errorf("initialize gateway router: %w", err)
	}
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		MaxHeaderBytes:    cfg.HTTP.MaxHeaderBytes,
	}

	return &App{
		router:          router,
		server:          server,
		port:            cfg.Port,
		shutdownTimeout: cfg.HTTP.ShutdownTimeout,
		logger:          logging.WithComponent(logger, "app"),
		ssoConn:         clients.ssoConn,
		coreConn:        clients.coreConn,
	}, nil
}

type gatewayClients struct {
	ssoConn    *grpc.ClientConn
	coreConn   *grpc.ClientConn
	ssoClient  *client.SSOClient
	coreClient *client.CoreClient
}

func newGRPCClients(cfg config.Bootstrap, logger *slog.Logger) (gatewayClients, error) {
	grpcOpts := grpcDialOptions(cfg, logger)

	ssoConn, err := grpc.NewClient(cfg.SSOTarget, grpcOpts...)
	if err != nil {
		return gatewayClients{}, fmt.Errorf("failed to create sso grpc client: %w", err)
	}

	coreConn, err := grpc.NewClient(cfg.CoreTarget, grpcOpts...)
	if err != nil {
		_ = ssoConn.Close() // startup already failed; the client error is returned
		return gatewayClients{}, fmt.Errorf("failed to create core grpc client: %w", err)
	}

	return gatewayClients{
		ssoConn:    ssoConn,
		coreConn:   coreConn,
		ssoClient:  client.NewSSOClient(ssoConn),
		coreClient: client.NewCoreClient(coreConn),
	}, nil
}

func (c gatewayClients) close() error {
	var err error
	if c.ssoConn != nil {
		err = errors.Join(err, c.ssoConn.Close())
	}
	if c.coreConn != nil {
		err = errors.Join(err, c.coreConn.Close())
	}
	return err
}

type proxyHandlers struct {
	coreHTTP               gin.HandlerFunc
	telemetryLogs          gin.HandlerFunc
	telemetryTerminal      gin.HandlerFunc
	adminTelemetryLogs     gin.HandlerFunc
	adminTelemetryTerminal gin.HandlerFunc
}

func newProxyHandlers(cfg config.Bootstrap, tickets *service.TelemetryTicketStore) (proxyHandlers, error) {
	proxyTransport := newProxyTransport(cfg.Proxy)
	coreProxy, err := handler.NewCoreProxyHandler(handler.ProxyOptions{
		TargetURL:    cfg.CoreHTTPTarget,
		ServiceToken: cfg.ServiceToken,
		Transport:    proxyTransport,
	})
	if err != nil {
		return proxyHandlers{}, fmt.Errorf("core proxy setup fail: %w", err)
	}

	// Core serves the log and terminal streams on the same internal listener.
	streamProxyOpts := handler.ProxyOptions{
		TargetURL:      cfg.CoreHTTPTarget,
		ServiceToken:   cfg.ServiceToken,
		Transport:      proxyTransport,
		AllowedOrigins: cfg.Router.CORSAllowedOrigins,
	}
	telemetryLogsProxy, err := newTelemetryLogsProxy(streamProxyOpts, tickets, false)
	if err != nil {
		return proxyHandlers{}, fmt.Errorf("telemetry logs proxy setup fail: %w", err)
	}
	telemetryTerminalProxy, err := newTelemetryTerminalProxy(streamProxyOpts, tickets, false)
	if err != nil {
		return proxyHandlers{}, fmt.Errorf("telemetry terminal proxy setup fail: %w", err)
	}
	adminTelemetryLogsProxy, err := newTelemetryLogsProxy(streamProxyOpts, tickets, true)
	if err != nil {
		return proxyHandlers{}, fmt.Errorf("admin telemetry logs proxy setup fail: %w", err)
	}
	adminTelemetryTerminalProxy, err := newTelemetryTerminalProxy(streamProxyOpts, tickets, true)
	if err != nil {
		return proxyHandlers{}, fmt.Errorf("admin telemetry terminal proxy setup fail: %w", err)
	}

	return proxyHandlers{
		coreHTTP:               coreProxy,
		telemetryLogs:          telemetryLogsProxy,
		telemetryTerminal:      telemetryTerminalProxy,
		adminTelemetryLogs:     adminTelemetryLogsProxy,
		adminTelemetryTerminal: adminTelemetryTerminalProxy,
	}, nil
}

func newTelemetryLogsProxy(opts handler.ProxyOptions, tickets *service.TelemetryTicketStore, adminRoute bool) (gin.HandlerFunc, error) {
	return handler.NewTelemetryLogsProxyHandler(opts, handler.TelemetryProxyAuthOptions{
		Tickets:    tickets,
		StreamType: model.TelemetryStreamLogs,
		AdminRoute: adminRoute,
	})
}

func newTelemetryTerminalProxy(opts handler.ProxyOptions, tickets *service.TelemetryTicketStore, adminRoute bool) (gin.HandlerFunc, error) {
	return handler.NewTelemetryTerminalProxyHandler(opts, handler.TelemetryProxyAuthOptions{
		Tickets:    tickets,
		StreamType: model.TelemetryStreamTerminal,
		AdminRoute: adminRoute,
	})
}

func newReadiness(cfg config.Bootstrap, clients gatewayClients) *readinessChecker {
	return &readinessChecker{
		timeout: cfg.Readiness.Timeout,
		grpcTargets: map[string]*grpc.ClientConn{
			"sso_grpc":  clients.ssoConn,
			"core_grpc": clients.coreConn,
		},
		httpClient: &http.Client{
			Transport: newProxyTransport(cfg.Proxy),
		},
		httpTargets: map[string]string{
			"core_http": cfg.CoreHTTPTarget,
		},
	}
}

func (a *App) Run() error {
	a.logger.Info("gateway server starting", "port", a.port)

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
		return errors.Join(err, a.closeGRPC())
	case <-ctx.Done():
		a.logger.Info("gateway shutdown signal received")
	}

	shutdownTimeout := a.shutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = 15 * time.Second
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	shutdownErr := a.server.Shutdown(shutdownCtx)
	grpcErr := a.closeGRPC()
	if shutdownErr != nil {
		return fmt.Errorf("gateway shutdown failed: %w", shutdownErr)
	}
	if grpcErr != nil {
		return grpcErr
	}
	a.logger.Info("gateway server stopped")
	return nil
}

func (a *App) closeGRPC() error {
	var err error
	if a.ssoConn != nil {
		err = errors.Join(err, a.ssoConn.Close())
	}
	if a.coreConn != nil {
		err = errors.Join(err, a.coreConn.Close())
	}
	return err
}

func grpcDialOptions(cfg config.Bootstrap, logger *slog.Logger) []grpc.DialOption {
	timeoutInterceptor := unaryTimeoutInterceptor(cfg.GRPC.RequestTimeout)
	interceptors := []grpc.UnaryClientInterceptor{
		timeoutInterceptor,
		logging.UnaryClientInterceptor(logger),
		internalauth.UnaryClientInterceptor(cfg.ServiceToken),
	}

	minConnectTimeout := cfg.GRPC.MinConnectTimeout
	if minConnectTimeout <= 0 {
		minConnectTimeout = 5 * time.Second
	}

	return []grpc.DialOption{
		// Internal gRPC runs on the host's private Docker networks without TLS
		// (ADR 0006); each call carries Gateway's own credential.
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
		grpc.WithChainUnaryInterceptor(interceptors...),
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

func newProxyTransport(cfg config.Proxy) *http.Transport {
	dialTimeout := cfg.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}
	idleTimeout := cfg.IdleConnTimeout
	if idleTimeout <= 0 {
		idleTimeout = 90 * time.Second
	}
	tlsTimeout := cfg.TLSHandshakeTimeout
	if tlsTimeout <= 0 {
		tlsTimeout = 10 * time.Second
	}
	expectTimeout := cfg.ExpectContinueTimeout
	if expectTimeout <= 0 {
		expectTimeout = time.Second
	}
	maxIdleConns := cfg.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = 100
	}
	maxIdleConnsPerHost := cfg.MaxIdleConnsPerHost
	if maxIdleConnsPerHost <= 0 {
		maxIdleConnsPerHost = 20
	}

	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		IdleConnTimeout:       idleTimeout,
		TLSHandshakeTimeout:   tlsTimeout,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		ExpectContinueTimeout: expectTimeout,
	}
}

type readinessChecker struct {
	timeout     time.Duration
	grpcTargets map[string]*grpc.ClientConn
	httpClient  *http.Client
	httpTargets map[string]string
}

func (r *readinessChecker) Ready(ctx context.Context) map[string]error {
	timeout := r.timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	checks := make(map[string]error, len(r.grpcTargets)+len(r.httpTargets))
	for name, conn := range r.grpcTargets {
		checks[name] = waitForGRPCReady(ctx, conn)
	}
	for name, target := range r.httpTargets {
		checks[name] = r.checkHTTP(ctx, target)
	}
	return checks
}

func waitForGRPCReady(ctx context.Context, conn *grpc.ClientConn) error {
	if conn == nil {
		return fmt.Errorf("grpc connection is not configured")
	}
	state := conn.GetState()
	if state == connectivity.Ready {
		return nil
	}
	conn.Connect()
	for state != connectivity.Ready {
		if !conn.WaitForStateChange(ctx, state) {
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("grpc state did not change from %s", state.String())
		}
		state = conn.GetState()
		if state == connectivity.TransientFailure || state == connectivity.Shutdown {
			return fmt.Errorf("grpc connection state is %s", state.String())
		}
	}
	return nil
}

func (r *readinessChecker) checkHTTP(ctx context.Context, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return err
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close() // status-only probe; close failure is not actionable
	if resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
	return nil
}
