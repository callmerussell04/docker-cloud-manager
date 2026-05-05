package service

import (
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
)

type CoreProvider interface {
	ContainerProvider
	VolumeProvider
	ImageProvider
	BuildProvider
	ProjectProvider
	SystemProvider
	StatsProvider
}

type Core struct {
	provider    CoreProvider
	objectStore BuildObjectStore
	logger      *slog.Logger
}

func NewCore(provider CoreProvider, objectStore BuildObjectStore, logger *slog.Logger) *Core {
	if logger == nil {
		logger = slog.Default()
	}
	return &Core{
		provider:    provider,
		objectStore: objectStore,
		logger:      logging.WithComponent(logger, "gateway_core_service"),
	}
}
