package metrics

import (
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
)

type SystemMetrics struct{}

func NewSystemMetrics() *SystemMetrics {
	return &SystemMetrics{}
}

func (m *SystemMetrics) GetTotalMemory() (int64, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return int64(v.Total), nil
}

func (m *SystemMetrics) GetFreeMemory() (int64, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return int64(v.Available), nil
}

func (m *SystemMetrics) GetLogicalCPUs() (int64, error) {
	return int64(runtime.NumCPU()), nil
}

func (m *SystemMetrics) GetCPULoad() (float64, error) {
	percentages, err := cpu.Percent(time.Second, false)
	if err != nil || len(percentages) == 0 {
		return 0.0, err
	}
	return percentages[0], nil
}

func (m *SystemMetrics) GetMemoryStats() (model.HostMemoryStats, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return model.HostMemoryStats{}, err
	}
	return model.HostMemoryStats{
		TotalBytes:     int64(v.Total),
		UsedBytes:      int64(v.Total - v.Available),
		AvailableBytes: int64(v.Available),
	}, nil
}

func (m *SystemMetrics) GetDiskUsage(path string) (model.HostDiskStats, error) {
	v, err := disk.Usage(path)
	if err != nil {
		return model.HostDiskStats{}, err
	}
	return model.HostDiskStats{
		TotalBytes: int64(v.Total),
		UsedBytes:  int64(v.Used),
		FreeBytes:  int64(v.Free),
	}, nil
}
