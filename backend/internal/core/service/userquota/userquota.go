// Package userquota owns what a user may reserve. Core stores a custom quota
// for an owner an administrator chose one for; every other owner has the
// default quota from the runtime config. Admission and every other quota
// reader ask this package and never the identity service, so an SSO outage
// cannot block admission.
package userquota

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/google/uuid"
)

// Store holds custom quotas.
type Store interface {
	// GetCustomUserQuota returns the owner's custom quota, marked Custom, or
	// false when the owner has the default quota.
	GetCustomUserQuota(ctx context.Context, ownerID uuid.UUID) (model.UserQuota, bool, error)
}

// ConfigProvider reads the current runtime config.
type ConfigProvider interface {
	Get() config.SystemConfig
}

// Service resolves an owner's effective quota.
type Service struct {
	store  Store
	config ConfigProvider
}

// New rejects an absent store or config, including a typed nil.
func New(store Store, cfg ConfigProvider) (*Service, error) {
	if isNil(store) {
		return nil, errors.New("user quotas: store is required")
	}
	if isNil(cfg) {
		return nil, errors.New("user quotas: config is required")
	}
	return &Service{store: store, config: cfg}, nil
}

// EffectiveUserQuota returns the owner's custom quota, or else the default
// quota in the runtime config as it is now. A caller holding the owner
// capacity lock reads the same custom quota every other reader does.
func (s *Service) EffectiveUserQuota(ctx context.Context, ownerID uuid.UUID) (model.UserQuota, error) {
	quota, custom, err := s.store.GetCustomUserQuota(ctx, ownerID)
	if err != nil {
		return model.UserQuota{}, fmt.Errorf("read user quota: %w", err)
	}
	if custom {
		return quota, nil
	}
	return defaultQuota(s.config.Get()), nil
}

func isNil(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// defaultQuota is the quota of an owner without a custom quota.
func defaultQuota(cfg config.SystemConfig) model.UserQuota {
	return model.UserQuota{
		CPUMillicores: config.CPUMillicores(cfg.DefaultUserQuotaCPU),
		RAMMB:         cfg.DefaultUserQuotaRAMMB,
		DiskMB:        cfg.DefaultUserQuotaDiskMB,
	}
}
