// Package stagedsource owns Core's rules for staged sources: uploaded or
// Git-cloned archives placed in object storage with a staged-byte reservation.
// Build ingestion and Compose ingestion both stage sources through it.
package stagedsource

import (
	"context"
	"errors"
	"math"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
)

// QuotaRule chooses what the staged disk quota check counts against the
// owner's disk quota besides the staged bytes.
type QuotaRule int

const (
	// CountReservedAndStaged counts the owner's reserved disk (images and
	// volumes) plus staged bytes, including the incoming bytes. Builds use it.
	CountReservedAndStaged QuotaRule = iota + 1
	// CountStagedOnly counts staged bytes only, including the incoming bytes.
	// Compose ingestion uses it: the containers, volumes and builds a
	// deployment creates are admitted by their own services later, so only
	// staged objects could bypass the regular disk checks.
	CountStagedOnly
)

// Repository stores staged-object reservations.
type Repository interface {
	Reserve(ctx context.Context, reservation model.StagedObjectReservation, maxBytesPerUser int64) error
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
// nothing is reserved, resized or released and no quota is checked. A nil
// Quotas counts no disk quota. ReservedDisk is required only by
// CountReservedAndStaged. A nil DiskMetrics turns the host disk floor off.
type Deps struct {
	Repository   Repository
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
	quotas       QuotaProvider
	reservedDisk ReservedDiskReader
	diskMetrics  HostDiskMetrics
	hostDiskPath string
	cfg          ConfigProvider
}

func New(deps Deps) *Staging {
	return &Staging{
		repo:         deps.Repository,
		quotas:       deps.Quotas,
		reservedDisk: deps.ReservedDisk,
		diskMetrics:  deps.DiskMetrics,
		hostDiskPath: deps.HostDiskPath,
		cfg:          deps.Config,
	}
}

// Reservation asks to reserve Bytes for a staged object under ObjectKey.
type Reservation struct {
	OwnerID   uuid.UUID
	ObjectKey string
	Kind      string
	Bytes     int64
	Rule      QuotaRule
}

// Reserve checks the staged disk quota under r.Rule and then reserves the
// staged bytes within MaxStagedSourceBytesPerUser. Negative sizes reserve
// zero. An empty object key reserves nothing.
func (s *Staging) Reserve(ctx context.Context, r Reservation) error {
	if s.repo == nil || r.ObjectKey == "" {
		return nil
	}
	if r.Bytes < 0 {
		r.Bytes = 0
	}
	if err := s.checkDiskQuota(ctx, r.OwnerID, r.Bytes, r.Rule); err != nil {
		return err
	}
	return s.repo.Reserve(ctx, model.StagedObjectReservation{
		ID:            uuid.New(),
		OwnerID:       r.OwnerID,
		ObjectKey:     r.ObjectKey,
		Kind:          r.Kind,
		BytesReserved: r.Bytes,
	}, s.cfg.Get().MaxStagedSourceBytesPerUser)
}

// checkDiskQuota is the one staged disk quota check. Each rule keeps the read
// order its caller has always used.
func (s *Staging) checkDiskQuota(ctx context.Context, ownerID uuid.UUID, incomingBytes int64, rule QuotaRule) error {
	var quotaBytes, usedBytes, stagedBytes int64
	var err error
	switch rule {
	case CountReservedAndStaged:
		if s.reservedDisk == nil {
			return errors.New("staged source: reserved disk reader is required to count reserved disk")
		}
		if quotaBytes, err = s.diskQuotaBytes(ctx, ownerID); err != nil {
			return err
		}
		if usedBytes, err = s.reservedDisk.ReservedDiskBytes(ctx, ownerID); err != nil {
			return err
		}
		if stagedBytes, err = s.repo.ActiveBytesByOwner(ctx, ownerID); err != nil {
			return err
		}
	case CountStagedOnly:
		if stagedBytes, err = s.repo.ActiveBytesByOwner(ctx, ownerID); err != nil {
			return err
		}
		if quotaBytes, err = s.diskQuotaBytes(ctx, ownerID); err != nil {
			return err
		}
	default:
		return errors.New("staged source: unknown disk quota rule")
	}
	if usedBytes+stagedBytes+incomingBytes > quotaBytes {
		return apperrors.New(apperrors.ErrQuotaExceeded, "user disk quota exceeded")
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
