package dependencywait_test

import (
	"fmt"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/dependencywait"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

type outcome string

const (
	wait       outcome = "wait"        // not met yet; poll again
	done       outcome = "done"        // condition met
	conflict   outcome = "conflict"    // the dependency failed: ErrConflict
	badRequest outcome = "bad request" // the condition cannot apply: ErrBadRequest
)

// containerStates are the Docker states a dependency can be inspected in, in
// the column order of the table below.
var containerStates = []struct {
	name  string
	state model.ContainerState
}{
	{"created", model.ContainerState{Status: "created"}},
	{"running", model.ContainerState{Status: "running", Running: true}},
	{"exited 0", model.ContainerState{Status: "exited", ExitCode: 0}},
	{"exited 1", model.ContainerState{Status: "exited", ExitCode: 1}},
	{"dead 137", model.ContainerState{Status: "dead", ExitCode: 137}},
	{"paused", model.ContainerState{Status: "paused", Running: true}},
}

// TestEvaluateInspection specifies dependency-condition evaluation for every
// combination of condition, healthcheck presence, Docker health status and
// container state.
func TestEvaluateInspection(t *testing.T) {
	const (
		started   = model.ComposeDependencyConditionStarted
		completed = model.ComposeDependencyConditionCompletedSuccessfully
		healthy   = model.ComposeDependencyConditionHealthy
		other     = "service_ready"
	)
	tests := []struct {
		condition   string
		healthcheck bool
		health      string // "" when Docker reports no health status
		// want lists the outcome per container state:
		// created, running, exited 0, exited 1, dead 137, paused.
		want [6]outcome
	}{
		// service_started ignores health.
		{started, false, "", [6]outcome{wait, done, wait, conflict, conflict, done}},
		{started, false, "starting", [6]outcome{wait, done, wait, conflict, conflict, done}},
		{started, false, "healthy", [6]outcome{wait, done, wait, conflict, conflict, done}},
		{started, false, "unhealthy", [6]outcome{wait, done, wait, conflict, conflict, done}},
		{started, true, "", [6]outcome{wait, done, wait, conflict, conflict, done}},
		{started, true, "starting", [6]outcome{wait, done, wait, conflict, conflict, done}},
		{started, true, "healthy", [6]outcome{wait, done, wait, conflict, conflict, done}},
		{started, true, "unhealthy", [6]outcome{wait, done, wait, conflict, conflict, done}},

		// service_completed_successfully ignores health.
		{completed, false, "", [6]outcome{wait, wait, done, conflict, wait, wait}},
		{completed, false, "starting", [6]outcome{wait, wait, done, conflict, wait, wait}},
		{completed, false, "healthy", [6]outcome{wait, wait, done, conflict, wait, wait}},
		{completed, false, "unhealthy", [6]outcome{wait, wait, done, conflict, wait, wait}},
		{completed, true, "", [6]outcome{wait, wait, done, conflict, wait, wait}},
		{completed, true, "starting", [6]outcome{wait, wait, done, conflict, wait, wait}},
		{completed, true, "healthy", [6]outcome{wait, wait, done, conflict, wait, wait}},
		{completed, true, "unhealthy", [6]outcome{wait, wait, done, conflict, wait, wait}},

		// service_healthy: without a healthcheck or health status it cannot
		// apply; with a healthcheck it waits for Docker's first health status;
		// a reported health status decides, except that a starting dependency
		// that exits non-zero has failed.
		{healthy, false, "", [6]outcome{badRequest, badRequest, badRequest, badRequest, badRequest, badRequest}},
		{healthy, false, "starting", [6]outcome{wait, wait, wait, conflict, conflict, wait}},
		{healthy, false, "healthy", [6]outcome{done, done, done, done, done, done}},
		{healthy, false, "unhealthy", [6]outcome{conflict, conflict, conflict, conflict, conflict, conflict}},
		{healthy, true, "", [6]outcome{wait, wait, wait, wait, wait, wait}},
		{healthy, true, "starting", [6]outcome{wait, wait, wait, conflict, conflict, wait}},
		{healthy, true, "healthy", [6]outcome{done, done, done, done, done, done}},
		{healthy, true, "unhealthy", [6]outcome{conflict, conflict, conflict, conflict, conflict, conflict}},

		// An unsupported condition never applies.
		{other, false, "", [6]outcome{badRequest, badRequest, badRequest, badRequest, badRequest, badRequest}},
		{other, false, "starting", [6]outcome{badRequest, badRequest, badRequest, badRequest, badRequest, badRequest}},
		{other, false, "healthy", [6]outcome{badRequest, badRequest, badRequest, badRequest, badRequest, badRequest}},
		{other, false, "unhealthy", [6]outcome{badRequest, badRequest, badRequest, badRequest, badRequest, badRequest}},
		{other, true, "", [6]outcome{badRequest, badRequest, badRequest, badRequest, badRequest, badRequest}},
		{other, true, "starting", [6]outcome{badRequest, badRequest, badRequest, badRequest, badRequest, badRequest}},
		{other, true, "healthy", [6]outcome{badRequest, badRequest, badRequest, badRequest, badRequest, badRequest}},
		{other, true, "unhealthy", [6]outcome{badRequest, badRequest, badRequest, badRequest, badRequest, badRequest}},
	}

	// Every condition × healthcheck × health combination has exactly one row.
	rows := make(map[string]bool, len(tests))
	for _, tt := range tests {
		rows[fmt.Sprintf("%s/%t/%s", tt.condition, tt.healthcheck, tt.health)] = true
	}
	require.Len(t, rows, 4*2*4)
	require.Len(t, tests, 4*2*4)

	for _, tt := range tests {
		for i, column := range containerStates {
			name := fmt.Sprintf("%s/healthcheck=%t/health=%q/%s", tt.condition, tt.healthcheck, tt.health, column.name)
			t.Run(name, func(t *testing.T) {
				inspection := model.ContainerInspection{State: column.state}
				if tt.healthcheck {
					inspection.Healthcheck = &model.Healthcheck{Test: []string{"CMD-SHELL", "true"}}
				}
				if tt.health != "" {
					health := tt.health
					inspection.State.HealthStatus = &health
				}

				gotDone, err := dependencywait.EvaluateInspection(inspection, tt.condition)

				switch tt.want[i] {
				case wait:
					require.NoError(t, err)
					require.False(t, gotDone)
				case done:
					require.NoError(t, err)
					require.True(t, gotDone)
				case conflict:
					require.ErrorIs(t, err, apperrors.ErrConflict)
					require.False(t, gotDone)
				case badRequest:
					require.ErrorIs(t, err, apperrors.ErrBadRequest)
					require.False(t, gotDone)
				}
			})
		}
	}
}
