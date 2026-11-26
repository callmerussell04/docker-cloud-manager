package compose

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/gitsource"
)

// SetCloneGitRepositoryForTest replaces the Git clone with clone for the
// duration of t, so tests can deploy from a prepared checkout.
func SetCloneGitRepositoryForTest(t *testing.T, clone func(ctx context.Context, req gitsource.CloneRequest) (gitsource.RepoInfo, error)) {
	t.Helper()
	previous := cloneGitRepository
	cloneGitRepository = clone
	t.Cleanup(func() { cloneGitRepository = previous })
}
