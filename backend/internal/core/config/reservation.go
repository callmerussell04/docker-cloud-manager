package config

import (
	"fmt"
	"strconv"
)

// MemoryReservationOrDefault is a container's memory reservation in bytes:
// bytes, or DefaultMemoryReservation for a figure of zero or less.
func (c SystemConfig) MemoryReservationOrDefault(bytes int64) int64 {
	if bytes > 0 {
		return bytes
	}
	return c.DefaultMemoryReservation
}

// CPUReservationOrDefault is a container's CPU reservation in millicores:
// millicores, or DefaultCPUReservation for a figure of zero or less.
func (c SystemConfig) CPUReservationOrDefault(millicores int64) int64 {
	if millicores > 0 {
		return millicores
	}
	return c.DefaultCPUReservation
}

// MemoryReservationRange says the memory reservation bounds for a rejection
// message, as "between 16 MiB and 1 TiB". It is derived from the bounds, so a
// changed bound changes every message, and it never holds a rejected figure.
func MemoryReservationRange() string {
	return "between " + MinMemoryReservation() + " and " + binarySize(MaxMemoryReservationBytes)
}

// MinMemoryReservation says the smallest memory reservation for a rejection
// message, as "16 MiB".
func MinMemoryReservation() string {
	return binarySize(MinMemoryReservationBytes)
}

// CPUReservationRange says the CPU reservation bounds in millicores for a
// rejection message, as "between 10 millicores and the largest cpu quota".
func CPUReservationRange() string {
	return fmt.Sprintf("between %d millicores and the largest cpu quota", MinCPUReservationMillicores)
}

// CPUReservationRangeInCores says the CPU reservation bounds in cores, the
// unit of a Compose file, for a rejection message, as "between 0.01 cores and
// the largest cpu quota".
func CPUReservationRangeInCores() string {
	cores := strconv.FormatFloat(float64(MinCPUReservationMillicores)/1000, 'f', -1, 64)
	return "between " + cores + " cores and the largest cpu quota"
}

// binarySize says bytes in the largest binary unit that divides it exactly,
// as "16 MiB" or "1 TiB".
func binarySize(bytes int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	unit := 0
	for unit < len(units)-1 && bytes != 0 && bytes%1024 == 0 {
		bytes /= 1024
		unit++
	}
	return fmt.Sprintf("%d %s", bytes, units[unit])
}
