package compose_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/gitsource"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	composemocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service/compose"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const regularComposeFileMessage = "compose file must be a regular file"

// cloneCheckout makes the Git clone write the checkout that prepare builds.
func cloneCheckout(t *testing.T, prepare func(t *testing.T, repoDir string)) {
	t.Helper()
	compose.SetCloneGitRepositoryForTest(t, func(_ context.Context, req gitsource.CloneRequest) (gitsource.RepoInfo, error) {
		require.NoError(t, os.MkdirAll(req.DestDir, 0o750))
		prepare(t, req.DestDir)
		return gitsource.RepoInfo{Host: "github.com", Path: "owner/repo", Ref: req.Ref}, nil
	})
}

func writeCheckoutFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// rejectingGitOrchestrator has strict mocks with no expectations: any project,
// job, staged reservation or staged object fails the test.
func rejectingGitOrchestrator(t *testing.T) *compose.Orchestrator {
	t.Helper()
	repo := composemocks.NewProjectRepository(t)
	deps, _ := composeOrchestratorDeps(t, repo, composeTestConfig())
	deps.ObjectStore = composemocks.NewObjectStorage(t)
	deps.StagedObjects = composemocks.NewStagedObjectRepository(t)
	deps.CapacityLock = composemocks.NewCapacityLock(t)
	deps.ReservedDisk = composemocks.NewReservedDiskReader(t)
	o, err := compose.NewOrchestrator(deps)
	require.NoError(t, err)
	return o
}

// composeFileLocations covers a default candidate and an explicitly requested
// path, which follow the same rules.
var composeFileLocations = []struct {
	name      string
	requested string
	path      string
}{
	{name: "default candidate", requested: "", path: "docker-compose.yml"},
	{name: "requested path", requested: "deploy/compose.yml", path: "deploy/compose.yml"},
}

func gitSource(composeFile string) model.GitSource {
	return model.GitSource{RepoURL: "https://github.com/owner/repo.git", Ref: "main", ComposeFile: composeFile}
}

func TestStartGitDeploymentRejectsComposeFilesThatAreNotRegular(t *testing.T) {
	coreLocalFile := filepath.Join(t.TempDir(), "secret.yml")
	writeCheckoutFile(t, coreLocalFile, gitComposeWithoutBuild)

	tests := []struct {
		name        string
		composeFile string
		prepare     func(t *testing.T, repoDir string)
		message     string
	}{
		{
			name: "default candidate symlinked to a device",
			prepare: func(t *testing.T, repoDir string) {
				require.NoError(t, os.Symlink("/dev/zero", filepath.Join(repoDir, "docker-compose.yml")))
			},
			message: regularComposeFileMessage,
		},
		{
			name: "second default candidate symlinked to a Core-local file",
			prepare: func(t *testing.T, repoDir string) {
				require.NoError(t, os.Symlink(coreLocalFile, filepath.Join(repoDir, "docker-compose.yaml")))
			},
			message: regularComposeFileMessage,
		},
		{
			name:        "requested file symlinked to a Core-local file",
			composeFile: "deploy/compose.yml",
			prepare: func(t *testing.T, repoDir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "deploy"), 0o750))
				require.NoError(t, os.Symlink(coreLocalFile, filepath.Join(repoDir, "deploy", "compose.yml")))
			},
			message: regularComposeFileMessage,
		},
		{
			name:        "requested file symlinked inside the checkout",
			composeFile: "compose.yml",
			prepare: func(t *testing.T, repoDir string) {
				writeCheckoutFile(t, filepath.Join(repoDir, "real.yml"), gitComposeWithoutBuild)
				require.NoError(t, os.Symlink("real.yml", filepath.Join(repoDir, "compose.yml")))
			},
			message: regularComposeFileMessage,
		},
		{
			name:        "parent directory symlinked outside the checkout",
			composeFile: "deploy/secret.yml",
			prepare: func(t *testing.T, repoDir string) {
				require.NoError(t, os.Symlink(filepath.Dir(coreLocalFile), filepath.Join(repoDir, "deploy")))
			},
			message: regularComposeFileMessage,
		},
		{
			name:        "parent directory symlinked inside the checkout",
			composeFile: "deploy/docker-compose.yml",
			prepare: func(t *testing.T, repoDir string) {
				writeCheckoutFile(t, filepath.Join(repoDir, "real", "docker-compose.yml"), gitComposeWithoutBuild)
				require.NoError(t, os.Symlink("real", filepath.Join(repoDir, "deploy")))
			},
			message: regularComposeFileMessage,
		},
		{
			name:        "requested file is a named pipe",
			composeFile: "compose.yml",
			prepare: func(t *testing.T, repoDir string) {
				mkfifo(t, filepath.Join(repoDir, "compose.yml"))
			},
			message: regularComposeFileMessage,
		},
		{
			name:        "requested path is a directory",
			composeFile: "deploy",
			prepare: func(t *testing.T, repoDir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "deploy"), 0o750))
			},
			message: "compose file path points to a directory",
		},
		{
			name:        "requested parent is a regular file",
			composeFile: "docker-compose.yml/inner.yml",
			prepare: func(t *testing.T, repoDir string) {
				writeCheckoutFile(t, filepath.Join(repoDir, "docker-compose.yml"), gitComposeWithoutBuild)
			},
			message: "compose file not found in git repository",
		},
		{
			name:        "requested file is missing",
			composeFile: "deploy/compose.yml",
			prepare:     func(t *testing.T, repoDir string) {},
			message:     "compose file not found in git repository",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cloneCheckout(t, tt.prepare)
			o := rejectingGitOrchestrator(t)

			ctx := accessscope.WithUserScope(context.Background(), uuid.New(), "", "")
			_, err := o.StartGitDeployment(ctx, "proj", gitSource(tt.composeFile))

			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			require.Equal(t, tt.message, apperrors.SafeMessage(err))
			if tt.composeFile != "" {
				require.NotContains(t, err.Error(), tt.composeFile)
			}
		})
	}
}

func TestStartGitDeploymentRejectsAnOversizedComposeFileWithoutReadingIt(t *testing.T) {
	for _, tc := range composeFileLocations {
		t.Run(tc.name, func(t *testing.T) {
			composeFile, name := tc.requested, tc.path
			cloneCheckout(t, func(t *testing.T, repoDir string) {
				path := filepath.Join(repoDir, filepath.FromSlash(name))
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
				f, err := os.Create(path)
				require.NoError(t, err)
				// A sparse file far above the cap: reading it whole would be obvious.
				require.NoError(t, f.Truncate(64<<30))
				require.NoError(t, f.Close())
			})
			o := rejectingGitOrchestrator(t)

			ctx := accessscope.WithUserScope(context.Background(), uuid.New(), "", "")
			_, err := o.StartGitDeployment(ctx, "proj", gitSource(composeFile))

			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			require.Equal(t, "compose file exceeds configured size limit", apperrors.SafeMessage(err))
		})
	}
}

func TestStartGitDeploymentDeploysARegularComposeFile(t *testing.T) {
	for _, tc := range composeFileLocations {
		t.Run(tc.name, func(t *testing.T) {
			composeFile, name := tc.requested, tc.path
			cloneCheckout(t, func(t *testing.T, repoDir string) {
				writeCheckoutFile(t, filepath.Join(repoDir, filepath.FromSlash(name)), gitComposeWithoutBuild)
			})
			repo := composemocks.NewProjectRepository(t)
			deps, _ := composeOrchestratorDeps(t, repo, composeTestConfig())
			objects := coretest.NewMemoryObjects()
			deps.ObjectStore = objects
			var savedJob model.ComposeDeploymentJob
			repo.EXPECT().
				CreateWithComposeDeploymentJob(mock.Anything, mock.AnythingOfType("model.Project"), mock.AnythingOfType("model.ComposeDeploymentJob"), mock.AnythingOfType("model.ComposeDeploymentOutbox")).
				Run(func(_ context.Context, _ model.Project, job model.ComposeDeploymentJob, _ model.ComposeDeploymentOutbox) {
					savedJob = job
				}).
				Return(nil)
			o, err := compose.NewOrchestrator(deps)
			require.NoError(t, err)

			ctx := accessscope.WithUserScope(context.Background(), uuid.New(), "", "")
			projectID, err := o.StartGitDeployment(ctx, "proj", gitSource(composeFile))

			require.NoError(t, err)
			require.NotEqual(t, uuid.Nil, projectID)
			require.Equal(t, model.ComposeDeploymentStatusQueued, savedJob.Status)
			require.True(t, objects.Has(savedJob.SourceObjectKey))
		})
	}
}
