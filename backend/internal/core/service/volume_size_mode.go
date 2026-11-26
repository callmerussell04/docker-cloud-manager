package service

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
)

// volumeSizeCheckTimeout bounds one host check, so a stalled Docker daemon
// cannot hold Core's startup or a settings update.
const volumeSizeCheckTimeout = 30 * time.Second

// VolumeSizeSupportChecker asks Docker whether it accepts a size for a local
// volume on this host. A false answer with no error means Docker refused it.
type VolumeSizeSupportChecker interface {
	CheckVolumeSizeSupport(ctx context.Context) (bool, error)
}

// VolumeSizeEnforcement decides how new volumes get their size: the one place
// that combines the volume_size_enforcement setting with whether this host
// was found to support volume sizes. It is "enforced" only when the setting
// is on and the last host check passed; the check runs at startup when the
// setting is on and again whenever a settings update turns it on.
type VolumeSizeEnforcement struct {
	checker   VolumeSizeSupportChecker
	cfg       ConfigManager
	available atomic.Bool
}

// CheckVolumeSizeEnforcement builds the enforcement state at startup. When
// the setting is on it checks the host once; a failed or refused check logs an
// error and leaves enforcement unavailable, and never stops startup.
func CheckVolumeSizeEnforcement(ctx context.Context, checker VolumeSizeSupportChecker, cfg ConfigManager, logger *slog.Logger) *VolumeSizeEnforcement {
	e := &VolumeSizeEnforcement{checker: checker, cfg: cfg}
	if !cfg.Get().VolumeSizeEnforcement {
		return e
	}
	supported, err := e.CheckVolumeSizeSupport(ctx)
	if err != nil || !supported {
		attrs := []any{"operation", "check_volume_size_support"}
		if err != nil {
			attrs = append(attrs, "error", err)
		}
		logger.Error("volume_size_enforcement is on but this host does not support volume sizes; "+
			"new volumes are created in measured mode until the host check passes", attrs...)
	}
	return e
}

// CheckVolumeSizeSupport checks the host now and records the answer, so a
// passing check makes enforcement available without a restart. A failed check
// records the host as unsupported.
func (e *VolumeSizeEnforcement) CheckVolumeSizeSupport(ctx context.Context) (bool, error) {
	checkCtx, cancel := context.WithTimeout(ctx, volumeSizeCheckTimeout)
	defer cancel()
	supported, err := e.checker.CheckVolumeSizeSupport(checkCtx)
	e.available.Store(err == nil && supported)
	return supported, err
}

// VolumeSizeMode reports the mode new volumes are created in.
func (e *VolumeSizeEnforcement) VolumeSizeMode() model.VolumeSizeMode {
	switch {
	case !e.cfg.Get().VolumeSizeEnforcement:
		return model.VolumeSizeMeasured
	case e.available.Load():
		return model.VolumeSizeEnforced
	default:
		return model.VolumeSizeUnavailable
	}
}
