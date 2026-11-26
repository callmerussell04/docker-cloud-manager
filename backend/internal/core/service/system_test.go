package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
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
			HostCapacity:      coremocks.NewSystemHostCapacity(t),
		}
	}, service.NewSystemService, map[string]string{
		"Config":            "config",
		"VolumeSizeSupport": "volume size support",
		"HostCapacity":      "host capacity",
	})
}

func TestSystemServiceGetAndUpdateConfig(t *testing.T) {
	svc, err := service.NewSystemService(service.SystemServiceDeps{
		Config:            newSystemConfigManager(t, coretest.SystemConfig()),
		VolumeSizeSupport: coremocks.NewSystemVolumeSizeSupport(t),
		HostCapacity:      coremocks.NewSystemHostCapacity(t),
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
		HostCapacity:      coremocks.NewSystemHostCapacity(t),
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
				HostCapacity:      coremocks.NewSystemHostCapacity(t),
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

// The fixture host for the capacity checks: 4 GiB and 2 CPUs. With the
// fixture's overcommit (memory 1.5, CPU 4) and no system reservation, the
// memory pool is 6 GiB and the CPU pool 8000 millicores; the fixture's two
// build slots take 1 GiB and 2000 millicores of them.
const (
	settingsHostMemoryBytes = 4 << 30
	settingsHostCPUs        = 2
)

// A settings update that changes a capacity setting is checked against the
// host with the pool functions admission uses, and rejected, with nothing
// saved, when no pool is left or the build slots do not fit it. The mock is
// strict: a case that expects no host read fails on one.
func TestSystemServiceRejectsCapacitySettingsTheHostCannotHold(t *testing.T) {
	readErr := errors.New("proc unreadable")
	tests := []struct {
		name        string
		apply       func(*config.SystemConfig)
		readsHost   bool
		memoryErr   error
		cpuErr      error
		wantErrIs   error
		wantMessage string
	}{
		{
			name:      "a system memory reservation that leaves a pool",
			apply:     func(c *config.SystemConfig) { c.ReservedSystemMemory = 1 << 30 },
			readsHost: true,
		},
		{
			name:        "a system memory reservation of the whole host",
			apply:       func(c *config.SystemConfig) { c.ReservedSystemMemory = settingsHostMemoryBytes },
			readsHost:   true,
			wantErrIs:   apperrors.ErrBadRequest,
			wantMessage: "the system memory reservation leaves no host memory for containers",
		},
		{
			name:        "a system cpu reservation of every cpu",
			apply:       func(c *config.SystemConfig) { c.ReservedSystemCPU = settingsHostCPUs * 1000 },
			readsHost:   true,
			wantErrIs:   apperrors.ErrBadRequest,
			wantMessage: "the system cpu reservation leaves no host cpu for containers",
		},
		{
			name:      "build slots that fit the memory pool exactly but exceed the cpu pool",
			apply:     func(c *config.SystemConfig) { c.MaxConcurrentBuilds = 12 },
			readsHost: true,
			// 12 x 512 MiB is the 6 GiB pool, 12 x 1000 millicores exceeds 8000.
			wantErrIs:   apperrors.ErrBadRequest,
			wantMessage: "the build slots need more host cpu than the server can reserve",
		},
		{
			name: "build slots above the memory pool",
			apply: func(c *config.SystemConfig) {
				c.MaxConcurrentBuilds, c.BuildCPUMillicores = 13, 100
			},
			readsHost:   true,
			wantErrIs:   apperrors.ErrBadRequest,
			wantMessage: "the build slots need more host memory than the server can reserve",
		},
		{
			name:      "build slots within both pools",
			apply:     func(c *config.SystemConfig) { c.MaxConcurrentBuilds, c.BuildCPUMillicores = 12, 100 },
			readsHost: true,
		},
		{
			name: "a lower overcommit that leaves the build slots no room",
			apply: func(c *config.SystemConfig) {
				c.CPUOvercommitFactor = 0.5 // 1000 millicores for 2000 of build slots
			},
			readsHost:   true,
			wantErrIs:   apperrors.ErrBadRequest,
			wantMessage: "the build slots need more host cpu than the server can reserve",
		},
		{
			name:        "a host memory read that fails",
			apply:       func(c *config.SystemConfig) { c.BuildMemoryBytes = 256 << 20 },
			readsHost:   true,
			memoryErr:   readErr,
			wantErrIs:   apperrors.ErrUnavailable,
			wantMessage: "the host capacity check could not run",
		},
		{
			name:        "a host cpu read that fails",
			apply:       func(c *config.SystemConfig) { c.ReservedSystemCPU = 100 },
			readsHost:   true,
			cpuErr:      readErr,
			wantErrIs:   apperrors.ErrUnavailable,
			wantMessage: "the host capacity check could not run",
		},
		{
			name:  "an unrelated change reads no host",
			apply: func(c *config.SystemConfig) { c.MaxLogSize = "50m" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initial := coretest.SystemConfig()
			host := coremocks.NewSystemHostCapacity(t)
			if tt.readsHost {
				host.EXPECT().GetTotalMemory().Return(settingsHostMemoryBytes, tt.memoryErr).Once()
				// A failed memory read ends the check before the cpu read.
				if tt.memoryErr == nil {
					host.EXPECT().GetLogicalCPUs().Return(settingsHostCPUs, tt.cpuErr).Once()
				}
			}
			svc, err := service.NewSystemService(service.SystemServiceDeps{
				Config:            newSystemConfigManager(t, initial),
				VolumeSizeSupport: coremocks.NewSystemVolumeSizeSupport(t),
				HostCapacity:      host,
			})
			require.NoError(t, err)

			update := svc.GetConfig(context.Background())
			tt.apply(&update)
			err = svc.UpdateConfig(context.Background(), update)

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				require.ErrorContains(t, err, tt.wantMessage)
				require.Equal(t, initial, svc.GetConfig(context.Background()), "a rejected update changes nothing")
				return
			}
			require.NoError(t, err)
			require.Equal(t, update, svc.GetConfig(context.Background()))
		})
	}
}

// A host too small for the capacity settings still lets an administrator save
// a change the capacity checks do not read.
func TestSystemServiceSavesUnrelatedChangesOnAHostTooSmallForTheSettings(t *testing.T) {
	initial := coretest.SystemConfig()
	initial.ReservedSystemMemory = 64 << 30
	svc, err := service.NewSystemService(service.SystemServiceDeps{
		Config:            newSystemConfigManager(t, initial),
		VolumeSizeSupport: coremocks.NewSystemVolumeSizeSupport(t),
		HostCapacity:      coremocks.NewSystemHostCapacity(t),
	})
	require.NoError(t, err)

	update := svc.GetConfig(context.Background())
	update.MaxLogSize = "50m"
	require.NoError(t, svc.UpdateConfig(context.Background(), update))
	require.Equal(t, "50m", svc.GetConfig(context.Background()).MaxLogSize)
}

// At startup every host conflict logs one error, and nothing fails.
func TestReportHostSettingConflictsLogsOneErrorEach(t *testing.T) {
	tests := []struct {
		name      string
		apply     func(*config.SystemConfig)
		memoryErr error
		want      []string
	}{
		{name: "settings the host holds", apply: func(*config.SystemConfig) {}},
		{
			name: "no memory and no cpu pool",
			apply: func(c *config.SystemConfig) {
				c.ReservedSystemMemory, c.ReservedSystemCPU = settingsHostMemoryBytes, settingsHostCPUs*1000
			},
			want: []string{
				"the system memory reservation leaves no host memory for containers",
				"the system cpu reservation leaves no host cpu for containers",
			},
		},
		{
			name:  "build slots above both pools",
			apply: func(c *config.SystemConfig) { c.MaxConcurrentBuilds = 13 },
			want: []string{
				"the build slots need more host memory than the server can reserve",
				"the build slots need more host cpu than the server can reserve",
			},
		},
		{
			name:      "a host read that fails",
			apply:     func(*config.SystemConfig) {},
			memoryErr: errors.New("proc unreadable"),
			want:      []string{"the host capacity check could not run"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := coretest.SystemConfig()
			tt.apply(&cfg)
			host := coremocks.NewSystemHostCapacity(t)
			host.EXPECT().GetTotalMemory().Return(settingsHostMemoryBytes, tt.memoryErr).Once()
			if tt.memoryErr == nil {
				host.EXPECT().GetLogicalCPUs().Return(settingsHostCPUs, nil).Once()
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))

			service.ReportHostSettingConflicts(context.Background(), host, cfg, logger)

			var got []string
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				if line == "" {
					continue
				}
				var entry struct {
					Level    string `json:"level"`
					Conflict string `json:"conflict"`
				}
				require.NoError(t, json.Unmarshal([]byte(line), &entry))
				require.Equal(t, "ERROR", entry.Level)
				got = append(got, entry.Conflict)
			}
			require.Equal(t, tt.want, got)
		})
	}
}

var _ service.SystemHostCapacity = (service.HostMetricsProvider)(nil)

var _ service.SystemVolumeSizeSupport = (*service.VolumeSizeEnforcement)(nil)
