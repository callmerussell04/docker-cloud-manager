package metrics

import (
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
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

func (m *SystemMetrics) GetCPULoad() (float64, error) {
	percentages, err := cpu.Percent(time.Second, false)
	if err != nil || len(percentages) == 0 {
		return 0.0, err
	}
	return percentages[0], nil
}
