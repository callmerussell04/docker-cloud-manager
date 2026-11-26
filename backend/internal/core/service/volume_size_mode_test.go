package service_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
)

// Core checks the host once at startup when enforcement is on. A failed check
// never stops startup: it logs an error and new volumes are created measured,
// shown to the administrator as "unavailable".
func TestCheckVolumeSizeEnforcementAtStartup(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		check      bool
		supported  bool
		checkErr   error
		wantMode   model.VolumeSizeMode
		wantErrors int
	}{
		{name: "setting off checks nothing", enabled: false, wantMode: model.VolumeSizeMeasured},
		{name: "setting on and supported", enabled: true, check: true, supported: true, wantMode: model.VolumeSizeEnforced},
		{name: "setting on and not supported", enabled: true, check: true, wantMode: model.VolumeSizeUnavailable, wantErrors: 1},
		{
			name: "setting on and the check fails", enabled: true, check: true, checkErr: errors.New("docker unavailable"),
			wantMode: model.VolumeSizeUnavailable, wantErrors: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := coretest.SystemConfig()
			cfg.VolumeSizeEnforcement = tt.enabled
			checker := coremocks.NewVolumeSizeSupportChecker(t)
			if tt.check {
				checker.EXPECT().CheckVolumeSizeSupport(mock.Anything).Return(tt.supported, tt.checkErr).Once()
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))

			enforcement := service.CheckVolumeSizeEnforcement(context.Background(), checker, coretest.StaticConfig{Config: cfg}, logger)

			require.Equal(t, tt.wantMode, enforcement.VolumeSizeMode())
			var logged []map[string]any
			for _, entry := range decodeLogs(t, &logs) {
				if entry["level"] == "ERROR" {
					logged = append(logged, entry)
				}
			}
			require.Len(t, logged, tt.wantErrors)
		})
	}
}

// After a failed startup check, an administrator can turn enforcement off and
// on again: the settings update re-checks the host, and a passing check makes
// enforcement available without a restart.
func TestVolumeSizeEnforcementBecomesAvailableAfterASuccessfulSettingsUpdate(t *testing.T) {
	initial := coretest.SystemConfig()
	initial.VolumeSizeEnforcement = true
	manager := newSystemConfigManager(t, initial)
	checker := coremocks.NewVolumeSizeSupportChecker(t)
	checker.EXPECT().CheckVolumeSizeSupport(mock.Anything).Return(false, nil).Once()
	enforcement := service.CheckVolumeSizeEnforcement(context.Background(), checker, manager, slog.New(slog.DiscardHandler))
	require.Equal(t, model.VolumeSizeUnavailable, enforcement.VolumeSizeMode())

	svc, err := service.NewSystemService(service.SystemServiceDeps{
		Config: manager, VolumeSizeSupport: enforcement, HostCapacity: coremocks.NewSystemHostCapacity(t),
	})
	require.NoError(t, err)
	cfg := svc.GetConfig(context.Background())
	cfg.VolumeSizeEnforcement = false
	require.NoError(t, svc.UpdateConfig(context.Background(), cfg))
	require.Equal(t, model.VolumeSizeMeasured, enforcement.VolumeSizeMode())

	checker.EXPECT().CheckVolumeSizeSupport(mock.Anything).Return(true, nil).Once()
	cfg.VolumeSizeEnforcement = true
	require.NoError(t, svc.UpdateConfig(context.Background(), cfg))
	require.Equal(t, model.VolumeSizeEnforced, enforcement.VolumeSizeMode())
}

// With the setting on and the startup check failing, Core keeps creating
// volumes, in measured mode: no size reaches Docker and the volume is not
// recorded as enforced.
func TestVolumesAreCreatedMeasuredWhenTheStartupCheckFails(t *testing.T) {
	cfg := volumeTestConfig()
	cfg.Config.VolumeSizeEnforcement = true
	checker := coremocks.NewVolumeSizeSupportChecker(t)
	checker.EXPECT().CheckVolumeSizeSupport(mock.Anything).Return(false, nil).Once()
	enforcement := service.CheckVolumeSizeEnforcement(context.Background(), checker, cfg, slog.New(slog.DiscardHandler))

	ownerID, volumeID, operationID := uuid.New(), uuid.New(), uuid.New()
	repo := newVolumeRepoMock(t)
	dockerAPI := coremocks.NewVolumeDockerAPI(t)
	lifecycle := newVolumeLifecycleFake(operationID, volumeID, ownerID, model.OperationCreate)
	svc := newTestVolumeService(t, service.VolumeServiceDeps{
		Repository: repo, Docker: dockerAPI, Config: cfg, Lifecycle: lifecycle, SizeMode: enforcement,
	})
	repo.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{ID: volumeID, OwnerID: ownerID, Name: "data", DockerName: "vol_data", SizeBytes: 1 << 30}, nil)
	dockerAPI.EXPECT().CreateVolume(mock.Anything, mock.MatchedBy(func(spec model.VolumeRuntimeSpec) bool {
		return spec.VolumeName == "vol_data" && spec.Name == "data" && spec.OwnerID == ownerID.String() && spec.SizeBytes == 0
	})).Return("vol_data", nil).Once()
	repo.EXPECT().MarkAvailable(mock.Anything, volumeID, false).Return(nil).Once()

	require.NoError(t, svc.ExecuteQueuedVolumeOperation(context.Background(), operationID, volumeID))
	require.Equal(t, model.OperationStatusDone, lifecycle.op.Status)
	require.Equal(t, model.VolumeSizeUnavailable, enforcement.VolumeSizeMode(), "monitoring reports the mode as unavailable")
}
