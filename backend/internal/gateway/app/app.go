package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/grpc/client"
	httprouter "github.com/callmerussell04/docker-cloud-manager/internal/gateway/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/service"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
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

type Config struct {
	Port                int
	SSOTarget           string
	CoreTarget          string
	BuilderHTTPTarget   string
	CoreHTTPTarget      string
	TelemetryHTTPTarget string
	InternalToken       string

	HTTP      HTTPServerConfig
	Router    httprouter.Config
	Cookie    handler.CookieConfig
	Proxy     ProxyConfig
	GRPC      GRPCConfig
	Readiness ReadinessConfig
}

type HTTPServerConfig struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

type ProxyConfig struct {
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
	ExpectContinueTimeout time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
}

type GRPCConfig struct {
	RequestTimeout    time.Duration
	MinConnectTimeout time.Duration
	TLS               GRPCTLSConfig
}

type GRPCTLSConfig struct {
	Enabled    bool
	CAFile     string
	CertFile   string
	KeyFile    string
	ServerName string
}

type ReadinessConfig struct {
	Timeout time.Duration
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	grpcOpts, err := grpcDialOptions(cfg, logger)
	if err != nil {
		return nil, err
	}

	ssoConn, err := grpc.NewClient(cfg.SSOTarget, grpcOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create sso grpc client: %w", err)
	}

	coreConn, err := grpc.NewClient(cfg.CoreTarget, grpcOpts...)
	if err != nil {
		_ = ssoConn.Close()
		return nil, fmt.Errorf("failed to create core grpc client: %w", err)
	}

	ssoClient := grpcclient.NewSSOClient(ssoConn)
	authService := service.NewAuth(ssoClient)
	authHandler := handler.NewAuthHandler(authService, cfg.Cookie)
	userService := service.NewUserManagement(ssoClient)
	userHandler := handler.NewUserManagementHandler(userService)

	coreClient := grpcclient.NewCoreClient(coreConn)
	coreService := service.NewCore(coreClient)
	coreHandler := handler.NewCoreHandler(coreService)

	proxyTransport := newProxyTransport(cfg.Proxy)
	builderProxy, err := handler.NewBuilderProxyHandler(handler.ProxyOptions{
		TargetURL:     cfg.BuilderHTTPTarget,
		InternalToken: cfg.InternalToken,
		Transport:     proxyTransport,
	})
	if err != nil {
		_ = ssoConn.Close()
		_ = coreConn.Close()
		return nil, fmt.Errorf("builder proxy setup fail: %w", err)
	}

	buildProxy, err := handler.NewAuthorizedBuildProxyHandler(handler.ProxyOptions{
		TargetURL:     cfg.BuilderHTTPTarget,
		InternalToken: cfg.InternalToken,
		Transport:     proxyTransport,
	}, coreService)
	if err != nil {
		_ = ssoConn.Close()
		_ = coreConn.Close()
		return nil, fmt.Errorf("builder build proxy setup fail: %w", err)
	}

	coreProxy, err := handler.NewCoreProxyHandler(handler.ProxyOptions{
		TargetURL:     cfg.CoreHTTPTarget,
		InternalToken: cfg.InternalToken,
		Transport:     proxyTransport,
	})
	if err != nil {
		_ = ssoConn.Close()
		_ = coreConn.Close()
		return nil, fmt.Errorf("core proxy setup fail: %w", err)
	}

	telemetryProxyOpts := handler.ProxyOptions{
		TargetURL:      cfg.TelemetryHTTPTarget,
		InternalToken:  cfg.InternalToken,
		Transport:      proxyTransport,
		AllowedOrigins: cfg.Router.CORSAllowedOrigins,
	}
	telemetryLogsProxy, err := handler.NewTelemetryLogsProxyHandler(telemetryProxyOpts)
	if err != nil {
		_ = ssoConn.Close()
		_ = coreConn.Close()
		return nil, fmt.Errorf("telemetry logs proxy setup fail: %w", err)
	}
	telemetryTerminalProxy, err := handler.NewTelemetryTerminalProxyHandler(telemetryProxyOpts)
	if err != nil {
		_ = ssoConn.Close()
		_ = coreConn.Close()
		return nil, fmt.Errorf("telemetry terminal proxy setup fail: %w", err)
	}

	readiness := &readinessChecker{
		timeout: cfg.Readiness.Timeout,
		grpcTargets: map[string]*grpc.ClientConn{
			"sso_grpc":  ssoConn,
			"core_grpc": coreConn,
		},
		httpClient: &http.Client{
			Transport: newProxyTransport(cfg.Proxy),
		},
		httpTargets: map[string]string{
			"builder_http":   cfg.BuilderHTTPTarget,
			"core_http":      cfg.CoreHTTPTarget,
			"telemetry_http": cfg.TelemetryHTTPTarget,
		},
	}
	healthHandler := handler.NewHealthHandler(readiness)

	router := httprouter.NewRouter(cfg.Router, authHandler, coreHandler, userHandler, healthHandler, builderProxy, buildProxy, coreProxy, telemetryLogsProxy, telemetryTerminalProxy, authService, logger)
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
		ssoConn:         ssoConn,
		coreConn:        coreConn,
	}, nil
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
		_ = a.closeGRPC()
		return err
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

func grpcDialOptions(cfg Config, logger *slog.Logger) ([]grpc.DialOption, error) {
	creds, err := grpcCredentials(cfg.GRPC.TLS)
	if err != nil {
		return nil, err
	}

	timeoutInterceptor := unaryTimeoutInterceptor(cfg.GRPC.RequestTimeout)
	interceptors := []grpc.UnaryClientInterceptor{
		timeoutInterceptor,
		logging.UnaryClientInterceptor(logger),
		internalauth.UnaryClientInterceptor(cfg.InternalToken),
	}

	minConnectTimeout := cfg.GRPC.MinConnectTimeout
	if minConnectTimeout <= 0 {
		minConnectTimeout = 5 * time.Second
	}

	return []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
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
	}, nil
}

func grpcCredentials(cfg GRPCTLSConfig) (credentials.TransportCredentials, error) {
	if !cfg.Enabled {
		return insecure.NewCredentials(), nil
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: cfg.ServerName,
	}
	if cfg.CAFile != "" {
		caPEM, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read grpc tls ca file: %w", err)
		}
		rootCAs := x509.NewCertPool()
		if ok := rootCAs.AppendCertsFromPEM(caPEM); !ok {
			return nil, fmt.Errorf("failed to parse grpc tls ca file")
		}
		tlsConfig.RootCAs = rootCAs
	}
	if cfg.CertFile != "" || cfg.KeyFile != "" {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			return nil, fmt.Errorf("both grpc tls cert and key files are required for mtls")
		}
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load grpc mtls client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return credentials.NewTLS(tlsConfig), nil
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

func newProxyTransport(cfg ProxyConfig) *http.Transport {
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
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
	return nil
}
