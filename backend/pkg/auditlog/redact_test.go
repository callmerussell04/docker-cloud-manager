package auditlog

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeDetailsJSONAllowsExposeDetailsAndDropsUnsafeKeys(t *testing.T) {
	raw := SafeDetailsJSON(map[string]string{
		DetailContainerID:          "container-id",
		DetailContainerName:        "api",
		DetailProjectID:            "project-id",
		DetailProjectName:          "proj",
		DetailComposeService:       "web",
		DetailDomainPrefix:         "app",
		DetailFullDomain:           "app.example.test",
		DetailInternalPort:         "8080",
		DetailPreviousDomainPrefix: "old",
		DetailPreviousInternalPort: "80",
		DetailSourceType:           "compose",
		"password":                 "secret",
		"env":                      "TOKEN=secret",
	})

	var details map[string]string
	require.NoError(t, json.Unmarshal([]byte(raw), &details))
	require.Equal(t, "container-id", details[DetailContainerID])
	require.Equal(t, "app.example.test", details[DetailFullDomain])
	require.Equal(t, "old", details[DetailPreviousDomainPrefix])
	require.NotContains(t, details, "password")
	require.NotContains(t, details, "env")
}
