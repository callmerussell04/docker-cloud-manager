package coretest_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/stretchr/testify/require"
)

func TestMemoryObjectsContract(t *testing.T) {
	coretest.RunObjectStoreContract(t, func(*testing.T) coretest.ObjectStore {
		return coretest.NewMemoryObjects()
	})
}

func TestMinIOStorageObjectStoreContract(t *testing.T) {
	coretest.RunObjectStoreContract(t, func(t *testing.T) coretest.ObjectStore {
		store, err := objectstorage.NewMinIOStorage(context.Background(), coretest.ObjectStorageConfigFromEnv(t))
		require.NoError(t, err)
		return store
	})
}
