package service_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
)

// Core reads the root filesystem cap status once at startup and keeps it. An
// administrator sees a warning in the log unless the cap is enforced, and Core
// starts whatever Docker answers.
func TestCheckRootfsQuota(t *testing.T) {
	tests := []struct {
		name      string
		read      model.RootfsQuotaStatus
		readErr   error
		want      model.RootfsQuotaStatus
		wantWarns int
	}{
		{name: "enforced", read: model.RootfsQuotaEnforced, want: model.RootfsQuotaEnforced, wantWarns: 0},
		{name: "not enforced", read: model.RootfsQuotaNotEnforced, want: model.RootfsQuotaNotEnforced, wantWarns: 1},
		{name: "unknown", read: model.RootfsQuotaUnknown, want: model.RootfsQuotaUnknown, wantWarns: 1},
		{
			name: "failed read", read: model.RootfsQuotaUnknown, readErr: errors.New("docker unavailable"),
			want: model.RootfsQuotaUnknown, wantWarns: 1,
		},
		{
			name: "failed read reporting another status still counts as unknown", read: model.RootfsQuotaEnforced,
			readErr: errors.New("docker unavailable"), want: model.RootfsQuotaUnknown, wantWarns: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := coremocks.NewRootfsQuotaReader(t)
			reader.EXPECT().ReadRootfsQuotaStatus(mock.Anything).Return(tt.read, tt.readErr).Once()
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))

			check := service.CheckRootfsQuota(context.Background(), reader, logger)

			require.Equal(t, tt.want, check.RootfsQuotaStatus())
			var warnings []map[string]any
			for _, entry := range decodeLogs(t, &logs) {
				require.NotEqual(t, "ERROR", entry["level"], "startup continues")
				if entry["level"] == "WARN" {
					warnings = append(warnings, entry)
				}
			}
			require.Len(t, warnings, tt.wantWarns)
			if tt.wantWarns > 0 {
				require.Equal(t, string(tt.want), warnings[0]["rootfs_quota_status"])
			}
		})
	}
}

var _ service.StatsRootfsQuota = (*service.RootfsQuotaCheck)(nil)
