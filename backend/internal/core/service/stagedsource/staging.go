// Package stagedsource owns Core's rules for staged sources: uploaded or
// Git-cloned archives placed in object storage with a staged-byte reservation.
// Build ingestion and Compose ingestion both stage sources through it.
package stagedsource

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
)

// Repository stores staged-object reservations. Reserve only inserts: the
// limits are checked by Staging under the owner capacity lock.
type Repository interface {
	Reserve(ctx context.Context, reservation model.StagedObjectReservation) error
	UpdateBytes(ctx context.Context, objectKey string, bytesReserved int64) error
	Release(ctx context.Context, objectKey string) error
	ActiveBytesByOwner(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

// QuotaProvider reads the owner's effective quota from Core.
type QuotaProvider interface {
	EffectiveUserQuota(ctx context.Context, ownerID uuid.UUID) (model.UserQuota, error)
}

// ReservedDiskReader reads the disk an owner already reserves, in bytes.
type ReservedDiskReader interface {
	ReservedDiskBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

// CapacityLock is the owner capacity lock. The staged disk quota check and the
// reservation insert run under it, so they are serialised with every other
// admission of the same owner. It waits in Core's lock pool, never in the
// query pool.
type CapacityLock interface {
	AcquireOwnerCapacityLock(ctx context.Context, ownerID uuid.UUID) (func(), error)
}

// HostDiskMetrics reads host disk usage for the disk floor.
type HostDiskMetrics interface {
	GetDiskUsage(path string) (model.HostDiskStats, error)
}

// ConfigProvider reads runtime configuration.
type ConfigProvider interface {
	Get() config.SystemConfig
}

// unlimitedDiskQuotaBytes is the disk quota when no quota provider is wired.
const unlimitedDiskQuotaBytes = math.MaxInt64

// Deps are the capabilities staging needs. A nil Repository turns staging off:
// nothing is reserved, resized or released and no limit is checked. With a
// Repository, CapacityLock, ReservedDisk and Config are required. A nil Quotas
// counts no disk quota. A nil DiskMetrics turns the host disk floor off.
type Deps struct {
	Repository   Repository
	CapacityLock CapacityLock
	Quotas       QuotaProvider
	ReservedDisk ReservedDiskReader
	DiskMetrics  HostDiskMetrics
	HostDiskPath string
	Config       ConfigProvider
}

// Staging reserves, resizes and releases staged objects and checks the staged
// disk quota and the projected host disk floor.
type Staging struct {
	repo         Repository
	capacityLock CapacityLock
	quotas       QuotaProvider
	reservedDisk ReservedDiskReader
	diskMetrics  HostDiskMetrics
	hostDiskPath string
	cfg          ConfigProvider
}

// New builds staging from deps. It rejects a Repository without its required
// capacity lock, reserved disk reader or config, including typed nils.
func New(deps Deps) (*Staging, error) {
	if deps.Repository != nil && !isNil(deps.Repository) {
		required := []struct {
			name  string
			value any
		}{
			{"capacity lock", deps.CapacityLock},
			{"reserved disk reader", deps.ReservedDisk},
			{"config", deps.Config},
		}
		for _, dependency := range required {
			if dependency.value == nil || isNil(dependency.value) {
				return nil, fmt.Errorf("staged source: %s is required", dependency.name)
			}
		}
	} else {
		deps.Repository = nil
	}
	return &Staging{
		repo:         deps.Repository,
		capacityLock: deps.CapacityLock,
		quotas:       deps.Quotas,
		reservedDisk: deps.ReservedDisk,
		diskMetrics:  deps.DiskMetrics,
		hostDiskPath: deps.HostDiskPath,
		cfg:          deps.Config,
	}, nil
}

func isNil(value any) bool {
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// Reservation asks to reserve Bytes for a staged object under ObjectKey.
type Reservation struct {
	OwnerID   uuid.UUID
	ObjectKey string
	Kind      string
	Bytes     int64
}

// Reserve admits and reserves a staged object by the one staged disk rule,
// under the owner capacity lock: the owner's reserved disk plus their staged
// bytes plus Bytes must fit the disk quota (user_disk), then their staged bytes
// plus Bytes must fit MaxStagedSourceBytesPerUser (staged_source_bytes). The
// insert runs before the lock is released. Negative sizes reserve zero. An
// empty object key reserves nothing. The caller must not hold a capacity lock:
// the lock pool has one connection, so a nested acquisition never returns.
func (s *Staging) Reserve(ctx context.Context, r Reservation) error {
	if s.repo == nil || r.ObjectKey == "" {
		return nil
	}
	if r.Bytes < 0 {
		r.Bytes = 0
	}
	unlock, err := s.capacityLock.AcquireOwnerCapacityLock(ctx, r.OwnerID)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.checkLimits(ctx, r.OwnerID, r.Bytes); err != nil {
		return err
	}
	return s.repo.Reserve(ctx, model.StagedObjectReservation{
		ID:            uuid.New(),
		OwnerID:       r.OwnerID,
		ObjectKey:     r.ObjectKey,
		Kind:          r.Kind,
		BytesReserved: r.Bytes,
	})
}

// checkLimits is the one staged disk rule. The caller holds the owner
// capacity lock.
func (s *Staging) checkLimits(ctx context.Context, ownerID uuid.UUID, incomingBytes int64) error {
	quotaBytes, err := s.diskQuotaBytes(ctx, ownerID)
	if err != nil {
		return err
	}
	reservedBytes, err := s.reservedDisk.ReservedDiskBytes(ctx, ownerID)
	if err != nil {
		return err
	}
	stagedBytes, err := s.repo.ActiveBytesByOwner(ctx, ownerID)
	if err != nil {
		return err
	}
	if reservedBytes+stagedBytes+incomingBytes > quotaBytes {
		return apperrors.Exceeded(apperrors.LimitUserDisk)
	}
	if maxStaged := s.cfg.Get().MaxStagedSourceBytesPerUser; maxStaged > 0 && stagedBytes+incomingBytes > maxStaged {
		return apperrors.Exceeded(apperrors.LimitStagedSourceBytes)
	}
	return nil
}

// diskQuotaBytes is the one place staging reads the owner's disk quota.
func (s *Staging) diskQuotaBytes(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	if s.quotas == nil {
		return unlimitedDiskQuotaBytes, nil
	}
	quota, err := s.quotas.EffectiveUserQuota(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	return quota.DiskBytes(), nil
}

// Resize records the stored size of a staged object, once the upload knows it.
// A missing reservation is not an error.
func (s *Staging) Resize(ctx context.Context, objectKey string, bytesReserved int64) error {
	if s.repo == nil || objectKey == "" {
		return nil
	}
	if err := s.repo.UpdateBytes(ctx, objectKey, bytesReserved); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	return nil
}

// Release ends a staged object's reservation on failure, cancellation or a
// terminal result. A missing reservation is not an error. Deleting the object
// itself stays with the caller.
func (s *Staging) Release(ctx context.Context, objectKey string) error {
	if s.repo == nil || objectKey == "" {
		return nil
	}
	if err := s.repo.Release(ctx, objectKey); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	return nil
}
