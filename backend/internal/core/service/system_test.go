package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
)

func newSystemConfigManager(t *testing.T, cfg config.SystemConfig) *config.Manager {
	t.Helper()
	manager, err := config.NewManager(filepath.Join(t.TempDir(), "config.json"), cfg)
	require.NoError(t, err)
	return manager
}

func TestSystemServiceRejectsMissingDependencies(t *testing.T) {
	depstest.RequireRejectsMissing(t, func(t *testing.T) service.SystemServiceDeps {
		return service.SystemServiceDeps{
			Config:            newSystemConfigManager(t, coretest.SystemConfig()),
			VolumeSizeSupport: coremocks.NewSystemVolumeSizeSupport(t),
		}
	}, service.NewSystemService, map[string]string{
		"Config":            "config",
		"VolumeSizeSupport": "volume size support",
	})
}

func TestSystemServiceGetAndUpdateConfig(t *testing.T) {
	svc, err := service.NewSystemService(service.SystemServiceDeps{
		Config:            newSystemConfigManager(t, coretest.SystemConfig()),
		VolumeSizeSupport: coremocks.NewSystemVolumeSizeSupport(t),
	})
	require.NoError(t, err)

	cfg := svc.GetConfig(context.Background())
	require.Equal(t, coretest.SystemConfig().MaxLogSize, cfg.MaxLogSize)

	cfg.MaxLogSize = "25m"
	require.NoError(t, svc.UpdateConfig(context.Background(), cfg))
	require.Equal(t, "25m", svc.GetConfig(context.Background()).MaxLogSize)
}

func TestSystemServiceRejectsInvalidConfig(t *testing.T) {
	svc, err := service.NewSystemService(service.SystemServiceDeps{
		Config:            newSystemConfigManager(t, coretest.SystemConfig()),
		VolumeSizeSupport: coremocks.NewSystemVolumeSizeSupport(t),
	})
	require.NoError(t, err)

	cfg := svc.GetConfig(context.Background())
	cfg.BuildPidsLimit = 0
	err = svc.UpdateConfig(context.Background(), cfg)
	require.ErrorIs(t, err, apperrors.ErrBadRequest)
	require.Equal(t, coretest.SystemConfig().BuildPidsLimit, svc.GetConfig(context.Background()).BuildPidsLimit)
}

// Turning volume size enforcement on asks the host first, so an administrator
// never runs with enforcement they believe in but do not have. Turning it off,
// or leaving it as it is, asks nothing (the strict mock fails on any call).
func TestSystemServiceChecksTheHostWhenVolumeSizeEnforcementTurnsOn(t *testing.T) {
	checkErr := errors.New("docker unavailable")
	tests := []struct {
		name        string
		current     bool
		requested   bool
		check       bool // whether the host check runs
		supported   bool
		checkErr    error
		wantErrIs   error
		wantMessage string
		wantSaved   bool
	}{
		{name: "turned on and the host supports it", requested: true, check: true, supported: true, wantSaved: true},
		{
			name: "turned on and the host does not support it", requested: true, check: true,
			wantErrIs: apperrors.ErrBadRequest, wantMessage: "this host does not support volume sizes", wantSaved: false,
		},
		{
			name: "turned on and the check fails", requested: true, check: true, checkErr: checkErr,
			wantErrIs: apperrors.ErrUnavailable, wantMessage: "the host check could not run", wantSaved: false,
		},
		{name: "turned off", current: true, requested: false, wantSaved: false},
		{name: "left on", current: true, requested: true, wantSaved: true},
		{name: "left off", current: false, requested: false, wantSaved: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initial := coretest.SystemConfig()
			initial.VolumeSizeEnforcement = tt.current
			support := coremocks.NewSystemVolumeSizeSupport(t)
			if tt.check {
				support.EXPECT().CheckVolumeSizeSupport(mock.Anything).Return(tt.supported, tt.checkErr).Once()
			}
			svc, err := service.NewSystemService(service.SystemServiceDeps{
				Config:            newSystemConfigManager(t, initial),
				VolumeSizeSupport: support,
			})
			require.NoError(t, err)

			update := svc.GetConfig(context.Background())
			update.VolumeSizeEnforcement = tt.requested
			update.MaxLogSize = "50m"
			err = svc.UpdateConfig(context.Background(), update)

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				if tt.wantMessage != "" {
					require.ErrorContains(t, err, tt.wantMessage)
				}
				require.Equal(t, initial, svc.GetConfig(context.Background()), "a rejected update changes nothing")
				return
			}
			require.NoError(t, err)
			got := svc.GetConfig(context.Background())
			require.Equal(t, tt.wantSaved, got.VolumeSizeEnforcement)
			require.Equal(t, "50m", got.MaxLogSize)
		})
	}
}

var _ service.SystemVolumeSizeSupport = (*service.VolumeSizeEnforcement)(nil)
