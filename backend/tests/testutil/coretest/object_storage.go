package coretest

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	composemocks "github.com/callmerussell04/docker-cloud-manager/backend/tests/mocks/core/service/compose"
)

// StatObjectStorage lets one Compose object-store double also serve the build
// service, sizing staged objects from the same in-memory object map.
type StatObjectStorage struct {
	*composemocks.ObjectStorage
	Objects map[string][]byte
}

func (s StatObjectStorage) StatObject(_ context.Context, objectKey string) (objectstorage.ObjectInfo, error) {
	data, ok := s.Objects[objectKey]
	if !ok {
		return objectstorage.ObjectInfo{}, apperrors.ErrNotFound
	}
	return objectstorage.ObjectInfo{Size: int64(len(data))}, nil
}
