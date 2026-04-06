package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type Adapter struct {
	baseURL    string
	httpClient *http.Client
}

func NewAdapter(registryURL string) *Adapter {
	baseURL := registryURL
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}
	return &Adapter{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

type manifestResponse struct {
	Config struct {
		Size int64 `json:"size"`
	} `json:"config"`
	Layers []struct {
		Size int64 `json:"size"`
	} `json:"layers"`
}

func (a *Adapter) GetImageSizeAndDigest(ctx context.Context, repo, tag string) (int64, string, error) {
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", a.baseURL, repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}

	// Обязательный заголовок для получения Manifest V2, где есть размеры слоев и Digest
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("registry returned status: %d", resp.StatusCode)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return 0, "", errors.New("missing Docker-Content-Digest header")
	}

	var manifest manifestResponse
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return 0, "", err
	}

	totalSize := manifest.Config.Size
	for _, layer := range manifest.Layers {
		totalSize += layer.Size
	}

	return totalSize, digest, nil
}

func (a *Adapter) DeleteManifest(ctx context.Context, repo, digest string) error {
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", a.baseURL, repo, digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("failed to delete manifest, status: %d", resp.StatusCode)
	}
	return nil
}
