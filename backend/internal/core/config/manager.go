package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
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
		if err := m.loadFromFile(defaultConfig); err != nil {
			return nil, err
		}
		if err := m.saveToFile(m.config); err != nil {
			return nil, err
		}
	}

	if err := ValidateSystemConfig(m.config); err != nil {
		return nil, fmt.Errorf("invalid system config: %w", err)
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

	if err := ValidateSystemConfig(newConfig); err != nil {
		return err
	}
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
	dir := filepath.Dir(m.configPath)
	tmp, err := os.CreateTemp(dir, filepath.Base(m.configPath)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, m.configPath)
}

func (m *Manager) loadFromFile(defaultConfig SystemConfig) error {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return err
	}
	data, err = backfillConfigJSON(data, defaultConfig)
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

func backfillConfigJSON(data []byte, defaultConfig SystemConfig) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	if _, ok := raw["container_ttl_hours"]; !ok {
		if legacy, ok := raw["container_ttl"]; ok {
			var ttlNanos int64
			if err := json.Unmarshal(legacy, &ttlNanos); err == nil && ttlNanos >= 0 {
				hours := int64((time.Duration(ttlNanos)).Hours())
				encoded, err := json.Marshal(hours)
				if err != nil {
					return nil, err
				}
				raw["container_ttl_hours"] = encoded
			}
		}
	}
	delete(raw, "container_ttl")

	defaultData, err := json.Marshal(defaultConfig)
	if err != nil {
		return nil, err
	}
	var defaults map[string]json.RawMessage
	if err := json.Unmarshal(defaultData, &defaults); err != nil {
		return nil, err
	}
	for key, value := range defaults {
		if _, ok := raw[key]; !ok {
			raw[key] = value
		}
	}

	return json.Marshal(raw)
}
