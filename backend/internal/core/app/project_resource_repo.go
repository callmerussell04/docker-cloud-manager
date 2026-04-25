package app

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/google/uuid"
)

type projectResourceRepo struct {
	contRepo *repository.ContainerRepository
	volRepo  *repository.VolumeRepository
}

func (p *projectResourceRepo) GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Container, error) {
	return p.contRepo.GetByProjectID(ctx, projectID)
}

func (p *projectResourceRepo) GetVolumesByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Volume, error) {
	return p.volRepo.GetByProjectID(ctx, projectID)
}
