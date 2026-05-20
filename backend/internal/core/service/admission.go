package service

import (
	"context"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

func hostMemoryPool(totalMemoryBytes, reservedSystemMemoryBytes int64, overcommitFactor float64) int64 {
	if overcommitFactor <= 0 {
		return 0
	}
	available := totalMemoryBytes - reservedSystemMemoryBytes
	if available <= 0 {
		return 0
	}
	return int64(float64(available) * overcommitFactor)
}

func ensureHostMemoryAdmission(totalMemoryBytes int64, cfg config.SystemConfig, reservedMemoryBytes, requestedMemoryBytes int64) error {
	if requestedMemoryBytes < 0 {
		requestedMemoryBytes = 0
	}
	pool := hostMemoryPool(totalMemoryBytes, cfg.ReservedSystemMemory, cfg.OvercommitFactor)
	if pool <= 0 {
		return apperrors.ErrHostExhausted
	}
	if reservedMemoryBytes+requestedMemoryBytes > pool {
		return apperrors.ErrHostExhausted
	}
	return nil
}

func ensureHostMemoryCapacity(ctx context.Context, metrics HostMetricsProvider, cfg config.SystemConfig, reservedMemoryBytes, requestedMemoryBytes int64, logger *slog.Logger, operation string) error {
	if metrics == nil {
		return nil
	}
	totalMem, err := metrics.GetTotalMemory()
	if err != nil {
		if logger != nil {
			logger.ErrorContext(ctx, "failed to get system memory", "operation", operation, "error", err)
		}
		return apperrors.ErrInternal
	}
	if err := ensureHostMemoryAdmission(totalMem, cfg, reservedMemoryBytes, requestedMemoryBytes); err != nil {
		if logger != nil {
			pool := hostMemoryPool(totalMem, cfg.ReservedSystemMemory, cfg.OvercommitFactor)
			logger.WarnContext(ctx,
				"request rejected by host memory capacity",
				"operation", operation,
				"projected_memory_mb", (reservedMemoryBytes+requestedMemoryBytes)/bytesPerMB,
				"max_pool_mb", pool/bytesPerMB,
			)
		}
		return err
	}
	return nil
}
