package app

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	corehttp "github.com/callmerussell04/docker-cloud-manager/internal/core/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
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

func registryServer(cfg RegistryConfig, client *http.Client, auth *service.RegistryAuthorization, logger *slog.Logger) (*http.Server, error) {
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
