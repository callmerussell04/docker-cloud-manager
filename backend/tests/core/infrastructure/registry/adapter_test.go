package registry_test

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/registry"
	"github.com/stretchr/testify/require"
)

func TestRegistryAdapterManifestSuccess(t *testing.T) {
	var acceptHeader string
	adapter := newAdapterWithRoundTripper(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, "http://registry.test/v2/owners/demo/manifests/latest", req.URL.String())
		acceptHeader = req.Header.Get("Accept")
		return httpResponse(http.StatusOK, "sha256:digest", `{"config":{"size":100},"layers":[{"size":200},{"size":300}]}`), nil
	}))

	size, digest, err := adapter.GetImageSizeAndDigest(context.Background(), "owners/demo", "latest")
	require.NoError(t, err)
	require.EqualValues(t, 600, size)
	require.Equal(t, "sha256:digest", digest)
	require.Contains(t, acceptHeader, "application/vnd.docker.distribution.manifest.v2+json")
	require.Contains(t, acceptHeader, "application/vnd.oci.image.manifest.v1+json")
}

func TestRegistryAdapterManifestErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		digest     string
		body       string
	}{
		{name: "non ok", statusCode: http.StatusNotFound, digest: "sha256:digest", body: `{}`},
		{name: "missing digest", statusCode: http.StatusOK, body: `{}`},
		{name: "invalid json", statusCode: http.StatusOK, digest: "sha256:digest", body: `{`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := newAdapterWithRoundTripper(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return httpResponse(tt.statusCode, tt.digest, tt.body), nil
			}))

			_, _, err := adapter.GetImageSizeAndDigest(context.Background(), "repo", "tag")
			require.Error(t, err)
		})
	}
}

func TestRegistryAdapterDeleteManifestStatuses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{name: "accepted", statusCode: http.StatusAccepted},
		{name: "not found is idempotent", statusCode: http.StatusNotFound},
		{name: "server error", statusCode: http.StatusInternalServerError, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := newAdapterWithRoundTripper(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodDelete, req.Method)
				require.Equal(t, "http://registry.test/v2/repo/manifests/sha256:digest", req.URL.String())
				return httpResponse(tt.statusCode, "", ""), nil
			}))

			err := adapter.DeleteManifest(context.Background(), "repo", "sha256:digest")
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func newAdapterWithRoundTripper(t *testing.T, rt http.RoundTripper) *registry.Adapter {
	t.Helper()
	adapter := registry.NewAdapter("http://registry.test")
	field := reflect.ValueOf(adapter).Elem().FieldByName("httpClient")
	require.True(t, field.IsValid())
	ptr := unsafe.Pointer(field.UnsafeAddr())
	reflect.NewAt(field.Type(), ptr).Elem().Set(reflect.ValueOf(&http.Client{Transport: rt}))
	return adapter
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func httpResponse(status int, digest, body string) *http.Response {
	header := make(http.Header)
	if digest != "" {
		header.Set("Docker-Content-Digest", digest)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
