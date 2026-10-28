package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

const manifestAttemptTimeout = time.Minute

type Adapter struct {
	baseURL    string
	httpClient *http.Client
	tokens     TokenProvider
}

func (a *Adapter) GetImageSizeAndDigest(ctx context.Context, repo, tag string) (int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, manifestAttemptTimeout)
	defer cancel()

	url := fmt.Sprintf("%s/v2/%s/manifests/%s", a.baseURL, repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}

	// НОВОЕ: Указываем, что мы готовы принять как старый формат Docker, так и новый OCI
	acceptHeaders := []string{
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.oci.image.index.v1+json",
	}
	req.Header.Set("Accept", strings.Join(acceptHeaders, ", "))

	if err := a.authorize(ctx, req, repo, model.RegistryTrustedPull); err != nil {
		return 0, "", requestError("registry manifest authorization failed", err)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return 0, "", requestError("registry manifest lookup failed", err)
	}
	defer func() { _ = resp.Body.Close() }() // read-only stream; close failure is not actionable

	if resp.StatusCode != http.StatusOK {
		return 0, "", apperrors.Wrap(
			responseError(resp.StatusCode), "registry manifest lookup failed",
			fmt.Errorf("registry returned HTTP %d", resp.StatusCode),
		)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return 0, "", apperrors.New(apperrors.ErrUnavailable, "registry manifest digest is missing")
	}

	var manifest manifestResponse
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return 0, "", requestError("registry manifest response is invalid", err)
	}

	totalSize := manifest.Config.Size
	for _, layer := range manifest.Layers {
		totalSize += layer.Size
	}

	return totalSize, digest, nil
}

func (a *Adapter) DeleteManifest(ctx context.Context, repo, digest string) error {
	ctx, cancel := context.WithTimeout(ctx, manifestAttemptTimeout)
	defer cancel()

	url := fmt.Sprintf("%s/v2/%s/manifests/%s", a.baseURL, repo, digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	if err := a.authorize(ctx, req, repo, model.RegistryTrustedCleanup); err != nil {
		return requestError("registry manifest authorization failed", err)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return requestError("registry manifest deletion failed", err)
	}
	defer func() { _ = resp.Body.Close() }() // read-only stream; close failure is not actionable

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNotFound {
		return apperrors.Wrap(
			responseError(resp.StatusCode), "registry manifest deletion failed",
			fmt.Errorf("registry returned HTTP %d", resp.StatusCode),
		)
	}
	return nil
}

func requestError(message string, cause error) error {
	kind := apperrors.ErrUnavailable
	var netErr net.Error
	if errors.Is(cause, context.DeadlineExceeded) || (errors.As(cause, &netErr) && netErr.Timeout()) {
		kind = apperrors.ErrTimeout
	} else if errors.Is(cause, apperrors.ErrForbidden) {
		kind = apperrors.ErrForbidden
	}
	return apperrors.Wrap(kind, message, cause)
}
