package metrics

type SystemMetrics struct{}

func NewSystemMetrics() *SystemMetrics {
	return &SystemMetrics{}
}

// TODO: replace with proper logic
func (m *SystemMetrics) GetFreeMemory() int64 {
	return 16 * 1024 * 1024 * 1024
}
