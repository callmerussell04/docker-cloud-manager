package registry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

func TLSClient(caFile string) (*http.Client, error) {
	ca, err := os.ReadFile(caFile)
	if err != nil {
		return nil, errors.New("read registry CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("invalid registry CA")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	transport.ResponseHeaderTimeout = 15 * time.Second
	return &http.Client{Transport: transport, Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

type TokenProvider func(context.Context, string, bool) (string, error)

func NewSecureAdapter(registryURL string, client *http.Client, tokens TokenProvider) (*Adapter, error) {
	u, err := url.Parse(registryURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || tokens == nil {
		return nil, errors.New("invalid authenticated registry endpoint")
	}
	return &Adapter{baseURL: registryURL, httpClient: client, tokens: tokens}, nil
}

func (a *Adapter) authorize(ctx context.Context, req *http.Request, repository string, cleanup bool) error {
	if a.tokens == nil {
		return nil
	}
	token, err := a.tokens(ctx, repository, cleanup)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

func responseError(code int) error {
	switch code {
	case http.StatusNotFound:
		return apperrors.ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return apperrors.ErrForbidden
	default:
		return apperrors.ErrUnavailable
	}
}

// DockerAuth encodes only an exact-repository exchange identity for the daemon.
func DockerAuth(username, password, authority string) string {
	raw, _ := json.Marshal(map[string]string{"username": username, "password": password, "serveraddress": authority})
	return base64.URLEncoding.EncodeToString(raw)
}

func RepositoryFromImage(imageName, authority string) (string, bool) {
	prefix := authority + "/"
	if !strings.HasPrefix(imageName, prefix) {
		return "", false
	}
	name := strings.TrimPrefix(imageName, prefix)
	name = strings.SplitN(name, "@", 2)[0]
	// The registry authority has already been removed; any colon is a tag separator.
	name = strings.SplitN(name, ":", 2)[0]
	return name, name != ""
}
