package coretest

import (
	"context"
	"sync"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/google/uuid"
)

// UserQuotaStoreFake is the in-memory UserQuotaStore. RunUserQuotaStoreContract
// pins it to the Postgres repository.
type UserQuotaStoreFake struct {
	mu     sync.Mutex
	quotas map[uuid.UUID]model.UserQuota
	faults UserQuotaStoreFaults
}

// UserQuotaStoreFaults holds one Fault per UserQuotaStoreFake method. A failing
// call returns the fault's error and changes nothing.
type UserQuotaStoreFaults struct {
	GetCustomUserQuota Fault
	SetCustomUserQuota Fault
}

var _ UserQuotaStore = (*UserQuotaStoreFake)(nil)

// NewUserQuotaStoreFake returns a store in which every owner has the default
// quota.
func NewUserQuotaStoreFake() *UserQuotaStoreFake {
	return &UserQuotaStoreFake{quotas: make(map[uuid.UUID]model.UserQuota)}
}

// SetFaults replaces the store's faults.
func (s *UserQuotaStoreFake) SetFaults(faults UserQuotaStoreFaults) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults = faults
}

func (s *UserQuotaStoreFake) GetCustomUserQuota(_ context.Context, ownerID uuid.UUID) (model.UserQuota, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.faults.GetCustomUserQuota.take(); err != nil {
		return model.UserQuota{}, false, err
	}
	quota, found := s.quotas[ownerID]
	return quota, found, nil
}

func (s *UserQuotaStoreFake) SetCustomUserQuota(_ context.Context, ownerID uuid.UUID, quota model.UserQuota) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.faults.SetCustomUserQuota.take(); err != nil {
		return err
	}
	quota.Custom = true
	s.quotas[ownerID] = quota
	return nil
}
