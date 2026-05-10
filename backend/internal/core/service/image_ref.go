package service

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func customImageRepositoryName(ownerID uuid.UUID, baseName string) string {
	return strings.ToLower(fmt.Sprintf("%s_%s", ownerID.String(), baseName))
}

func customImageFullTag(registryURL string, ownerID uuid.UUID, baseName, version string) string {
	return fmt.Sprintf("%s/%s:%s", registryURL, customImageRepositoryName(ownerID, baseName), version)
}
