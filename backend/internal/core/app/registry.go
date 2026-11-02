package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	corehttp "github.com/callmerussell04/docker-cloud-manager/internal/core/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/registry"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/logging"
)

// RegistryConfig is bootstrap-only. The TLS edge has no Core control API routes.
type RegistryConfig struct {
	Authority              string
	BackendURL             string
	CAFile                 string
	CertificateFile        string
	KeyFile                string
	SigningCertificateFile string
	SigningKeyFile         string
	Issuer                 string
}

// registryComponents is Core's Registry trust boundary. The adapter signs its
// requests with the authorization's trusted tokens, and the cleanup worker uses
// the adapter, so they are built in that order.
type registryComponents struct {
	auth    *service.RegistryAuthorization
	adapter *registry.Adapter
	cleanup *service.RegistryCleanupWorker
	edge    *http.Server
}

func newRegistryComponents(cfg RegistryConfig, grants *repository.RegistryGrantRepository, users service.UserInfoProvider, runtime *config.Manager, logger *slog.Logger) (registryComponents, error) {
	client, err := registry.TLSClient(cfg.CAFile)
	if err != nil {
		return registryComponents{}, err
	}
	signer, err := registry.NewSigner(cfg.SigningCertificateFile, cfg.SigningKeyFile, cfg.Issuer)
	if err != nil {
		return registryComponents{}, err
	}
	auth, err := service.NewRegistryAuthorization(service.RegistryAuthorizationDeps{
		Repository: grants, Users: users, Signer: signer, Authority: cfg.Authority,
	})
	if err != nil {
		return registryComponents{}, err
	}
	adapter, err := registry.NewSecureAdapter(cfg.BackendURL, client, auth.TrustedToken)
	if err != nil {
		return registryComponents{}, err
	}
	cleanup, err := service.NewRegistryCleanupWorker(service.RegistryCleanupWorkerDeps{
		Repository: grants, Registry: adapter, Config: runtime, Logger: logger,
	})
	if err != nil {
		return registryComponents{}, err
	}
	edge, err := registryServer(cfg, client, auth, logger)
	if err != nil {
		return registryComponents{}, err
	}
	return registryComponents{auth: auth, adapter: adapter, cleanup: cleanup, edge: edge}, nil
}

// pullAuthorization gives the Docker daemon an exact-repository pull identity
// for DCM-built images and no credentials for public images.
func pullAuthorization(auth *service.RegistryAuthorization, runtime *config.Manager) func(context.Context, string) (string, error) {
	return func(ctx context.Context, imageName string) (string, error) {
		repo, custom := registry.RepositoryFromImage(imageName, runtime.Get().RegistryPublicURL)
		if !custom {
			return "", nil
		}
		credential, err := auth.PullCredential(ctx, repo)
		if err != nil {
			return "", err
		}
		return registry.DockerAuth(credential.Username, credential.Password, runtime.Get().RegistryPublicURL), nil
	}
}

func registryServer(cfg RegistryConfig, client *http.Client, auth *service.RegistryAuthorization, logger *slog.Logger) (*http.Server, error) {
	// Parse errors are replaced, not wrapped: *url.Error and its causes quote the
	// raw setting, which may carry userinfo, and bootstrap errors omit values.
	endpoint, err := url.Parse("https://" + cfg.Authority)
	if err != nil || endpoint.Host != cfg.Authority || endpoint.Port() != "5000" || endpoint.Path != "" || endpoint.User != nil {
		return nil, errors.New("invalid canonical registry authority")
	}
	target, err := url.Parse(cfg.BackendURL)
	if err != nil || target.Scheme != "https" || target.Host == "" {
		return nil, errors.New("invalid registry backend")
	}
	pair, err := tls.LoadX509KeyPair(cfg.CertificateFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load registry edge TLS identity: %w", err)
	}
	handler := corehttp.RegistryEdgeHandler(target, endpoint.Host, client.Transport, auth, logging.WithComponent(logger, "registry_edge"))
	return &http.Server{Addr: ":5000", Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: time.Minute, WriteTimeout: time.Minute, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, NextProtos: []string{"http/1.1"}}}, nil
}
