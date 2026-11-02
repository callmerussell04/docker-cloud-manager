package validation_test

import (
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/validation"
	"github.com/stretchr/testify/require"
)

func TestImageTagSourcePolicy(t *testing.T) {
	for _, tag := range []string{
		"nginx", "nginx:alpine", "acme/app:V1", "docker.io/nginx",
		"docker.io/library/nginx:alpine", "docker.io/acme/app:v1",
		"acme/my__app:v1", "acme/my--app:v1",
	} {
		t.Run("allowed/"+tag, func(t *testing.T) {
			require.NoError(t, validation.ImageTag(tag))
		})
	}
	for _, tag := range []string{
		"evil.example/acme/app:latest", "localhost/repo", "LOCALHOST/repo",
		"127.0.0.1/repo", "127.0.0.1:5000/repo", "[::1]:5000/repo",
		"registry:5000/repo", "docker.io:443/library/nginx", "Docker.io/library/nginx",
		"index.docker.io/nginx", "registry-1.docker.io/nginx", "docker.io.evil.example/repo",
		"https://docker.io/nginx", "docker.io./nginx", "docker.io//nginx", "docker.io/../nginx",
		"docker%2eio/nginx", "docker.io%2fevil.example/repo", "docker.io/acme%2fapp",
		"user@docker.io/nginx", "docker.io/Acme/app", "Acme/app", "nginx:", "nginx:-bad",
		" nginx", "nginx\n", "nginx\x00", "", "nginx@sha256:" + strings.Repeat("a", 64),
		"nginx:" + strings.Repeat("a", 129),
	} {
		t.Run("rejected/"+tag, func(t *testing.T) {
			require.Error(t, validation.ImageTag(tag))
		})
	}
}
