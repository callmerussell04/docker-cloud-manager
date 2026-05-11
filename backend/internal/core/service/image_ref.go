package service

import (
	"github.com/callmerussell04/docker-cloud-manager/pkg/imageref"
	"github.com/google/uuid"
)

func customImageRepositoryName(ownerID uuid.UUID, baseName string) string {
	return imageref.CustomRepositoryName(ownerID, baseName)
}

func customImageFullTag(registryURL string, ownerID uuid.UUID, baseName, version string) string {
	return imageref.CustomFullTag(registryURL, ownerID, baseName, version)
}
