package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

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
