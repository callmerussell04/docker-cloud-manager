package auditlog_test

import (
	"encoding/json"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/stretchr/testify/require"
)

func TestSafeDetailsJSONAllowsExposeDetailsAndDropsUnsafeKeys(t *testing.T) {
	raw := auditlog.SafeDetailsJSON(map[string]string{
		auditlog.DetailContainerID:          "container-id",
		auditlog.DetailContainerName:        "api",
		auditlog.DetailProjectID:            "project-id",
		auditlog.DetailProjectName:          "proj",
		auditlog.DetailComposeService:       "web",
		auditlog.DetailDomainPrefix:         "app",
		auditlog.DetailFullDomain:           "app.example.test",
		auditlog.DetailInternalPort:         "8080",
		auditlog.DetailPreviousDomainPrefix: "old",
		auditlog.DetailPreviousInternalPort: "80",
		auditlog.DetailSourceType:           "compose",
		"password":                          "secret",
		"env":                               "TOKEN=secret",
	})

	var details map[string]string
	require.NoError(t, json.Unmarshal([]byte(raw), &details))
	require.Equal(t, "container-id", details[auditlog.DetailContainerID])
	require.Equal(t, "app.example.test", details[auditlog.DetailFullDomain])
	require.Equal(t, "old", details[auditlog.DetailPreviousDomainPrefix])
	require.NotContains(t, details, "password")
	require.NotContains(t, details, "env")
}
