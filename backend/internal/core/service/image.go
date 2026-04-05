package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type ImageRepository interface {
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Image, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.Image, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Save(ctx context.Context, img domain.Image) error
	UpdateSize(ctx context.Context, id uuid.UUID, sizeMB int) error
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetUserDiskQuota(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type BuildRepository interface {
	Save(ctx context.Context, b domain.Build) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
}

type ImageDockerAPI interface {
	RemoveImage(ctx context.Context, imageID string, force bool) error
}

type ImageService struct {
	repo      ImageRepository
	buildRepo BuildRepository
	dockerAPI ImageDockerAPI
}

func NewImageService(repo ImageRepository, buildRepo BuildRepository, dockerAPI ImageDockerAPI) *ImageService {
	return &ImageService{
		repo:      repo,
		buildRepo: buildRepo,
		dockerAPI: dockerAPI,
	}
}

func (s *ImageService) GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Image, error) {
	return s.repo.GetByOwnerID(ctx, ownerID)
}

func (s *ImageService) Delete(ctx context.Context, ownerID, imageID uuid.UUID) error {
	img, err := s.repo.GetByID(ctx, imageID)
	if err != nil {
		return err
	}

	if img.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	if !img.IsCustom {
		return errors.New("cannot delete system image")
	}

	err = s.dockerAPI.RemoveImage(ctx, img.Tag, false)
	if err != nil {
		return err
	}

	return s.repo.Delete(ctx, imageID)
}

func (s *ImageService) InitBuildRecord(ctx context.Context, ownerID uuid.UUID, tag string, logFilePath string) (uuid.UUID, uuid.UUID, error) {
	// 1. Предварительная проверка дисковой квоты ДО сборки
	// Мы не знаем размер будущего образа, но если квота УЖЕ исчерпана, нет смысла начинать сборку.
	quotaMB, err := s.repo.GetUserDiskQuota(ctx, ownerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	usedMB, err := s.repo.GetUserUsedDiskSpace(ctx, ownerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	if usedMB >= quotaMB {
		return uuid.Nil, uuid.Nil, apperrors.ErrQuotaExceeded
	}

	// 2. Резервируем "пустой" образ в БД
	imageID := uuid.New()
	img := domain.Image{
		ID:       imageID,
		OwnerID:  ownerID,
		Tag:      tag,
		SizeMB:   0, // Размер пока неизвестен
		IsCustom: true,
	}

	if err := s.repo.Save(ctx, img); err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	// 3. Создаем запись о начале сборки
	buildID := uuid.New()
	build := domain.Build{
		ID:          buildID,
		ImageID:     imageID,
		Status:      domain.BuildStatusPending, // Или "running", так как процесс уже пошел
		LogFilePath: logFilePath,
		StartedAt:   time.Now(),
	}

	if err := s.buildRepo.Save(ctx, build); err != nil {
		// В случае ошибки удаляем зарезервированный образ (откат)
		s.repo.Delete(ctx, imageID)
		return uuid.Nil, uuid.Nil, err
	}

	return buildID, imageID, nil
}

func (s *ImageService) CompleteBuildRecord(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error {
	// 1. Обновляем статус самой сборки (логгер закончил работу)
	err := s.buildRepo.UpdateStatus(ctx, buildID, status)
	if err != nil {
		return err
	}

	if status != domain.BuildStatusSuccess {
		// Если сборка упала (ошибка или таймаут), пустой образ нам больше не нужен
		_ = s.repo.Delete(ctx, imageID)
		return nil
	}

	// 2. Если сборка успешна, проверяем финальный размер
	img, err := s.repo.GetByID(ctx, imageID)
	if err != nil {
		return err
	}

	quotaMB, err := s.repo.GetUserDiskQuota(ctx, img.OwnerID)
	if err != nil {
		return err
	}

	usedMB, err := s.repo.GetUserUsedDiskSpace(ctx, img.OwnerID)
	if err != nil {
		return err
	}

	// 3. Пост-проверка (Admission Control 2)
	// usedMB уже включает старые образы. Проверяем, влезает ли новый.
	if usedMB+int64(sizeMB) > quotaMB {
		// Квота превышена! Откатываем операцию:
		// А) Удаляем физический образ из Докера (чтобы не забивал диск)
		_ = s.dockerAPI.RemoveImage(context.Background(), img.Tag, true)
		// Б) Удаляем метаданные из БД
		_ = s.repo.Delete(ctx, imageID)
		// В) Обновляем статус сборки на специфичную ошибку
		_ = s.buildRepo.UpdateStatus(ctx, buildID, "failed_quota_exceeded")

		return apperrors.ErrQuotaExceeded
	}

	// 4. Если всё хорошо, фиксируем реальный размер образа в БД
	return s.repo.UpdateSize(ctx, imageID, sizeMB)
}
