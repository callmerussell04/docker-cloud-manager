package dependencywait_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/dependencywait"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/stretchr/testify/require"
)

func TestEvaluateDependencyConditions(t *testing.T) {
	healthy := "healthy"
	unhealthy := "unhealthy"
	tests := []struct {
		name      string
		state     model.ContainerState
		condition string
		wantDone  bool
		wantErr   error
	}{
		{name: "started running", state: model.ContainerState{Running: true}, condition: model.ComposeDependencyConditionStarted, wantDone: true},
		{name: "started failed", state: model.ContainerState{Running: false, ExitCode: 1}, condition: model.ComposeDependencyConditionStarted, wantErr: apperrors.ErrConflict},
		{name: "healthy", state: model.ContainerState{Running: true, HealthStatus: &healthy}, condition: model.ComposeDependencyConditionHealthy, wantDone: true},
		{name: "unhealthy", state: model.ContainerState{Running: true, HealthStatus: &unhealthy}, condition: model.ComposeDependencyConditionHealthy, wantErr: apperrors.ErrConflict},
		{name: "healthy without healthcheck", state: model.ContainerState{Running: true}, condition: model.ComposeDependencyConditionHealthy, wantErr: apperrors.ErrBadRequest},
		{name: "completed success", state: model.ContainerState{Status: "exited", Running: false, ExitCode: 0}, condition: model.ComposeDependencyConditionCompletedSuccessfully, wantDone: true},
		{name: "completed still created", state: model.ContainerState{Status: "created", Running: false, ExitCode: 0}, condition: model.ComposeDependencyConditionCompletedSuccessfully, wantDone: false},
		{name: "completed failed", state: model.ContainerState{Status: "exited", Running: false, ExitCode: 2}, condition: model.ComposeDependencyConditionCompletedSuccessfully, wantErr: apperrors.ErrConflict},
		{name: "unsupported", state: model.ContainerState{}, condition: "unsupported", wantErr: apperrors.ErrBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, err := dependencywait.Evaluate(tt.state, tt.condition)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantDone, done)
		})
	}
}

func TestEvaluateInspectionHealthyWaitsForDockerHealthState(t *testing.T) {
	done, err := dependencywait.EvaluateInspection(model.ContainerInspection{
		Healthcheck: &model.Healthcheck{Test: []string{"CMD-SHELL", "pg_isready -U postgres"}},
		State:       model.ContainerState{Status: "created", Running: false},
	}, model.ComposeDependencyConditionHealthy)

	require.NoError(t, err)
	require.False(t, done)
}

func TestEvaluateInspectionHealthyStillRejectsMissingHealthcheck(t *testing.T) {
	_, err := dependencywait.EvaluateInspection(model.ContainerInspection{
		State: model.ContainerState{Status: "running", Running: true},
	}, model.ComposeDependencyConditionHealthy)

	require.ErrorIs(t, err, apperrors.ErrBadRequest)
}
