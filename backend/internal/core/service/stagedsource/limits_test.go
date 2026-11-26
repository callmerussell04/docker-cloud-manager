package stagedsource_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/stagedsource"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	stagedsourcemocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service/stagedsource"
	"github.com/stretchr/testify/require"
)

func TestCheckHostDiskFloorCountsTheProjectedWrite(t *testing.T) {
	metricsFailure := errors.New("disk metrics unavailable")
	scenarios := []struct {
		name      string
		path      string
		wantPath  string
		freeBytes int64
		projected int64
		statErr   error
		wantErr   error
	}{
		{name: "room after the write", path: "/host/var/lib/docker", wantPath: "/host/var/lib/docker", freeBytes: 300 * mb, projected: 100 * mb},
		{name: "write would cross the floor", path: "/host/var/lib/docker", wantPath: "/host/var/lib/docker", freeBytes: 250 * mb, projected: 100 * mb, wantErr: apperrors.ErrHostExhausted},
		{name: "negative write counts as none", wantPath: "/", freeBytes: 200 * mb, projected: -mb},
		{name: "metrics fail", wantPath: "/", statErr: metricsFailure, wantErr: metricsFailure},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := coretest.IntegrationConfig()
			cfg.HostMinFreeDiskBytes = 200 * mb
			metrics := stagedsourcemocks.NewHostDiskMetrics(t)
			metrics.EXPECT().GetDiskUsage(scenario.wantPath).Return(model.HostDiskStats{FreeBytes: scenario.freeBytes}, scenario.statErr).Once()
			staging := newStaging(t, stagedsource.Deps{DiskMetrics: metrics, DockerDataPath: scenario.path, Config: coretest.StaticConfig{Config: cfg}})

			err := staging.CheckHostDiskFloor(scenario.projected)

			if scenario.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, scenario.wantErr)
		})
	}
}

func TestCheckHostDiskFloorIsOffWithoutMetricsOrFloor(t *testing.T) {
	cfg := coretest.IntegrationConfig()
	cfg.HostMinFreeDiskBytes = 200 * mb
	require.NoError(t, newStaging(t, stagedsource.Deps{Config: coretest.StaticConfig{Config: cfg}}).CheckHostDiskFloor(mb))

	cfg.HostMinFreeDiskBytes = 0
	staging := newStaging(t, stagedsource.Deps{DiskMetrics: stagedsourcemocks.NewHostDiskMetrics(t), Config: coretest.StaticConfig{Config: cfg}})
	require.NoError(t, staging.CheckHostDiskFloor(mb))
}

func TestCappedReaderRejectsBytesBeyondTheCap(t *testing.T) {
	for name, scenario := range map[string]struct {
		input   string
		wantErr error
	}{
		"under the cap":   {input: "abc"},
		"exactly the cap": {input: "abcd"},
		"over the cap":    {input: "abcde", wantErr: apperrors.ErrBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer

			_, err := io.Copy(&out, stagedsource.CappedReader(strings.NewReader(scenario.input), 4))

			if scenario.wantErr == nil {
				require.NoError(t, err)
				require.Equal(t, scenario.input, out.String())
				return
			}
			require.ErrorIs(t, err, scenario.wantErr)
			require.LessOrEqual(t, out.Len(), 4)
		})
	}
}
