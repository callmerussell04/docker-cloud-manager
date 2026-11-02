package auditlog

import (
	"encoding/json"
	"strconv"
)

var allowedDetailKeys = map[string]struct{}{
	DetailSourceType:           {},
	DetailContainerID:          {},
	DetailContainerName:        {},
	DetailProjectID:            {},
	DetailProjectName:          {},
	DetailComposeService:       {},
	DetailDomainPrefix:         {},
	DetailFullDomain:           {},
	DetailInternalPort:         {},
	DetailPreviousDomainPrefix: {},
	DetailPreviousInternalPort: {},
	DetailProvider:             {},
	DetailAdmin:                {},
}

func SafeDetailsJSON(details map[string]string) string {
	if len(details) == 0 {
		return "{}"
	}
	safe := make(map[string]string, len(details))
	for key, value := range details {
		if _, ok := allowedDetailKeys[key]; !ok {
			continue
		}
		if len(value) > 256 {
			value = value[:256]
		}
		safe[key] = value
	}
	if len(safe) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(safe)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func IntDetail(value int) string {
	return strconv.Itoa(value)
}
