package config

import (
	"context"
	"log/slog"
	"sync"
)

type RuntimeSource interface {
	GetBuilderConfig(ctx context.Context) (BuilderConfig, error)
}

type RuntimeManager struct {
	mu     sync.RWMutex
	cfg    BuilderConfig
	source RuntimeSource
	logger *slog.Logger
}

func NewRuntimeManager(defaultConfig BuilderConfig, source RuntimeSource, logger *slog.Logger) *RuntimeManager {
	return &RuntimeManager{
		cfg:    defaultConfig,
		source: source,
		logger: logger,
	}
}

func (m *RuntimeManager) Get() BuilderConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

func (m *RuntimeManager) Refresh(ctx context.Context) BuilderConfig {
	if m.source == nil {
		return m.Get()
	}

	cfg, err := m.source.GetBuilderConfig(ctx)
	if err != nil {
		if m.logger != nil {
			m.logger.WarnContext(ctx, "failed to refresh builder runtime config", "error", err)
		}
		return m.Get()
	}

	m.mu.Lock()
	cfg.LogsDirPath = m.cfg.LogsDirPath
	cfg.StoragePath = m.cfg.StoragePath
	cfg.RegistryURL = m.cfg.RegistryURL
	m.cfg = cfg
	m.mu.Unlock()
	return cfg
}
