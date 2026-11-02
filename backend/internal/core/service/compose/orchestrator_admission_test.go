package compose_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	composemocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service/compose"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const twoServiceCompose = `
services:
  web:
    image: nginx:latest
  cache:
    image: redis:latest
`

func TestStartDeploymentAdmission(t *testing.T) {
	counterFailure := errors.New("deployment counter unavailable")
	capacityExceeded := apperrors.New(apperrors.ErrHostExhausted, "host capacity exhausted")
	scenarios := []struct {
		name          string
		queueLimit    int
		queued        int
		countErr      error
		capacityErr   error
		wantErr       error
		checkCapacity bool
	}{
		{name: "admitted", queueLimit: 2, queued: 1, checkCapacity: true},
		{name: "queue full", queueLimit: 2, queued: 2, wantErr: apperrors.ErrLimitExceeded},
		{name: "queue counter fails", queueLimit: 2, countErr: counterFailure, wantErr: counterFailure},
		{name: "queue limit disabled", queueLimit: 0, checkCapacity: true},
		{name: "capacity exceeded", queueLimit: 2, capacityErr: capacityExceeded, wantErr: capacityExceeded, checkCapacity: true},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			owner := uuid.New()
			cfg := composeTestConfig()
			cfg.MaxQueuedComposeDeploysPerUser = scenario.queueLimit
			cfg.DefaultMemoryReservation = 256 << 20
			cfg.DefaultCPUReservation = 500
			repo := composemocks.NewProjectRepository(t)
			deps, objects := composeOrchestratorDeps(t, repo, cfg)
			counter := composemocks.NewActiveComposeDeploymentCounter(t)
			if scenario.queueLimit > 0 {
				counter.EXPECT().CountActiveComposeDeploymentsByOwner(mock.Anything, owner).
					Return(scenario.queued, scenario.countErr).Once()
			}
			deps.DeploymentCounter = counter
			capacity := composemocks.NewCapacityChecker(t)
			if scenario.checkCapacity {
				capacity.EXPECT().CheckCapacity(mock.Anything, owner, int64(2*256<<20), int64(2*500), int64(0)).
					Return(scenario.capacityErr).Once()
			}
			deps.Capacity = capacity
			if scenario.wantErr == nil {
				repo.EXPECT().CreateWithComposeDeploymentJob(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			}
			o, err := compose.NewOrchestrator(deps)
			require.NoError(t, err)
			ctx := accessscope.WithUserScope(context.Background(), owner, "", "")

			projectID, err := o.StartDeployment(ctx, "proj", "source.zip", "", bytes.NewReader(zipComposeArchive(t, twoServiceCompose)))
			if scenario.wantErr == nil {
				require.NoError(t, err)
				require.NotEqual(t, uuid.Nil, projectID)
				return
			}
			require.ErrorIs(t, err, scenario.wantErr)
			require.Equal(t, apperrors.SafeMessage(scenario.wantErr), apperrors.SafeMessage(err))
			require.Equal(t, uuid.Nil, projectID)
			require.Empty(t, objects, "rejected deployments must delete their staged source")
		})
	}
}
