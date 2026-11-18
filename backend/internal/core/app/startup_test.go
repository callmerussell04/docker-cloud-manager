package app

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Without a tenant parent cgroup nothing caps what tenant and build
// containers take together, so Core says so once at startup. It is a
// warning, never a reason not to start.
func TestWarnWithoutTenantCgroupParent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		parent    string
		wantWarns int
	}{
		{name: "unset", parent: "", wantWarns: 1},
		{name: "blank", parent: "  ", wantWarns: 1},
		{name: "set", parent: "dcm-tenants.slice", wantWarns: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer

			warnWithoutTenantCgroupParent(slog.New(slog.NewJSONHandler(&logs, nil)), tc.parent)

			var warnings []map[string]any
			for line := range strings.Lines(logs.String()) {
				var entry map[string]any
				require.NoError(t, json.Unmarshal([]byte(line), &entry))
				require.Equal(t, "WARN", entry["level"], "startup continues")
				warnings = append(warnings, entry)
			}
			require.Len(t, warnings, tc.wantWarns)
			if tc.wantWarns > 0 {
				require.Contains(t, warnings[0]["msg"], "TENANT_CGROUP_PARENT", "the warning names the setting")
			}
		})
	}
}
