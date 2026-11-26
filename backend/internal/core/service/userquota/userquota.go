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
	"math"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/nilcheck"
	"github.com/google/uuid"
)

// MaxOwnersPerRead bounds one UserQuotas read; it matches the largest admin
// user page.
const MaxOwnersPerRead = 100

// Store holds custom quotas.
type Store interface {
	// GetCustomUserQuota returns the owner's custom quota, marked Custom, or
	// false when the owner has the default quota.
	GetCustomUserQuota(ctx context.Context, ownerID uuid.UUID) (model.UserQuota, bool, error)
	// GetCustomUserQuotas returns the custom quotas, marked Custom, of those
	// owners that have one.
	GetCustomUserQuotas(ctx context.Context, ownerIDs []uuid.UUID) (map[uuid.UUID]model.UserQuota, error)
	// SetCustomUserQuota gives the owner a custom quota, replacing any other.
	SetCustomUserQuota(ctx context.Context, ownerID uuid.UUID, quota model.UserQuota) error
	// DeleteCustomUserQuota returns the owner to the default quota.
	DeleteCustomUserQuota(ctx context.Context, ownerID uuid.UUID) error
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
	if nilcheck.IsNil(store) {
		return nil, errors.New("user quotas: store is required")
	}
	if nilcheck.IsNil(cfg) {
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

// UserQuotas returns the effective quota of each owner, marked Custom or
// not, for an administrator. Owners on the default quota get the defaults as
// they are now.
func (s *Service) UserQuotas(ctx context.Context, ownerIDs []uuid.UUID) (map[uuid.UUID]model.UserQuota, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if len(ownerIDs) > MaxOwnersPerRead {
		return nil, apperrors.New(apperrors.ErrBadRequest, fmt.Sprintf("at most %d users may be read at once", MaxOwnersPerRead))
	}
	custom, err := s.store.GetCustomUserQuotas(ctx, ownerIDs)
	if err != nil {
		return nil, fmt.Errorf("read user quotas: %w", err)
	}
	byDefault := defaultQuota(s.config.Get())
	quotas := make(map[uuid.UUID]model.UserQuota, len(ownerIDs))
	for _, ownerID := range ownerIDs {
		if quota, found := custom[ownerID]; found {
			quotas[ownerID] = quota
			continue
		}
		quotas[ownerID] = byDefault
	}
	return quotas, nil
}

// SetUserQuota gives the owner a custom quota for an administrator and
// returns it. Lowering a quota below what the owner already reserves stops
// nothing; it applies from the owner's next admission. Whether the owner
// exists is the caller's to confirm: Core does not ask the identity service.
func (s *Service) SetUserQuota(ctx context.Context, ownerID uuid.UUID, limits model.UserQuotaLimits) (model.UserQuota, error) {
	if err := requireAdmin(ctx); err != nil {
		return model.UserQuota{}, err
	}
	quota, err := validateLimits(limits)
	if err != nil {
		return model.UserQuota{}, err
	}
	if err := s.store.SetCustomUserQuota(ctx, ownerID, quota); err != nil {
		return model.UserQuota{}, fmt.Errorf("set user quota: %w", err)
	}
	quota.Custom = true
	return quota, nil
}

// ResetUserQuota returns the owner to the default quota for an administrator
// and returns that quota. Resetting an owner already on it changes nothing.
func (s *Service) ResetUserQuota(ctx context.Context, ownerID uuid.UUID) (model.UserQuota, error) {
	if err := requireAdmin(ctx); err != nil {
		return model.UserQuota{}, err
	}
	if err := s.store.DeleteCustomUserQuota(ctx, ownerID); err != nil {
		return model.UserQuota{}, fmt.Errorf("reset user quota: %w", err)
	}
	return defaultQuota(s.config.Get()), nil
}

// requireAdmin admits only an administrator with an actor identity.
func requireAdmin(ctx context.Context) error {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return err
	}
	if scope.Kind != accessscope.KindAdmin || scope.UserID == uuid.Nil {
		return apperrors.ErrForbidden
	}
	return nil
}

// validateLimits applies the bounds of the default quota settings. Its
// messages never repeat the rejected value.
func validateLimits(limits model.UserQuotaLimits) (model.UserQuota, error) {
	cpu := limits.CPUCores
	if math.IsNaN(cpu) || !(cpu > 0 && cpu <= config.MaxUserQuotaCPU) || config.CPUMillicores(cpu) < 1 {
		return model.UserQuota{}, apperrors.New(apperrors.ErrBadRequest, fmt.Sprintf("cpu quota must be between 0.001 and %.0f cores", config.MaxUserQuotaCPU))
	}
	if limits.RAMMB <= 0 || limits.DiskMB <= 0 {
		return model.UserQuota{}, apperrors.New(apperrors.ErrBadRequest, "memory and disk quotas must be positive")
	}
	return model.UserQuota{CPUMillicores: config.CPUMillicores(cpu), RAMMB: limits.RAMMB, DiskMB: limits.DiskMB}, nil
}

// defaultQuota is the quota of an owner without a custom quota.
func defaultQuota(cfg config.SystemConfig) model.UserQuota {
	return model.UserQuota{
		CPUMillicores: config.CPUMillicores(cfg.DefaultUserQuotaCPU),
		RAMMB:         cfg.DefaultUserQuotaRAMMB,
		DiskMB:        cfg.DefaultUserQuotaDiskMB,
	}
}
