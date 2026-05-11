package imageref

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func ParseTag(rawTag string) (baseName, version string) {
	parts := strings.SplitN(rawTag, ":", 2)
	if len(parts) == 1 || parts[1] == "" {
		return parts[0], "latest"
	}
	return parts[0], parts[1]
}

func NormalizeTag(rawTag string) string {
	baseName, version := ParseTag(rawTag)
	return fmt.Sprintf("%s:%s", baseName, version)
}

func CustomRepositoryName(ownerID uuid.UUID, baseName string) string {
	return CustomRepositoryNameString(ownerID.String(), baseName)
}

func CustomRepositoryNameString(ownerID, baseName string) string {
	return strings.ToLower(fmt.Sprintf("%s_%s", ownerID, baseName))
}

func CustomFullTag(registryURL string, ownerID uuid.UUID, baseName, version string) string {
	return fmt.Sprintf("%s/%s:%s", registryURL, CustomRepositoryName(ownerID, baseName), version)
}
