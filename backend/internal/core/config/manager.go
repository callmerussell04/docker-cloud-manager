package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type SystemConfig struct {
	BaseDomain               string        `json:"base_domain"`
	DefaultMemoryReservation int64         `json:"default_memory_reservation_bytes"`
	ReservedSystemMemory     int64         `json:"reserved_system_memory_bytes"`
	OvercommitFactor         float64       `json:"overcommit_factor"`
	MaxBurstMultiplier       int64         `json:"max_burst_multiplier"`
	DefaultCPUShares         int64         `json:"default_cpu_shares"`
	HighLoadCPUShares        int64         `json:"high_load_cpu_shares"`
	HighLoadContainerCount   int           `json:"high_load_container_count"`
	ContainerStopTimeout     int           `json:"container_stop_timeout"`
	MaxLogSize               string        `json:"max_log_size"`
	MaxLogFiles              string        `json:"max_log_files"`
	ContainerDiskQuota       string        `json:"container_disk_quota"`
	MaxVolumesPerUser        int           `json:"max_volumes_per_user"`
	MaxContainersPerUser     int           `json:"max_containers_per_user"`
	RegistryURL              string        `json:"registry_url"`
	ContainerTTL             time.Duration `json:"container_ttl"`
}

type Manager struct {
	mu         sync.RWMutex
	config     SystemConfig
	configPath string
}

func NewManager(configPath string, defaultConfig SystemConfig) (*Manager, error) {
	m := &Manager{
		configPath: configPath,
		config:     defaultConfig,
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return nil, err
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := m.saveToFile(defaultConfig); err != nil {
			return nil, err
		}
	} else {
		if err := m.loadFromFile(); err != nil {
			return nil, err
		}
	}

	return m, nil
}

func (m *Manager) Get() SystemConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

func (m *Manager) Update(newConfig SystemConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.saveToFile(newConfig); err != nil {
		return err
	}
	m.config = newConfig
	return nil
}

func (m *Manager) saveToFile(cfg SystemConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.configPath, data, 0644)
}

func (m *Manager) loadFromFile() error {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return err
	}
	var cfg SystemConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	m.config = cfg
	return nil
}
