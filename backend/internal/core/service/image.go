package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	UpdateBuildAndImageSizeTx(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error
	MarkBuildFailedAndDeleteImageTx(ctx context.Context, buildID, imageID uuid.UUID, status string) error
}

type BuildRepository interface {
	Save(ctx context.Context, b domain.Build) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	GetUserBuilds(ctx context.Context, ownerID uuid.UUID) ([]domain.Build, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.Build, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type ImageDockerAPI interface {
	RemoveImage(ctx context.Context, imageID string, force bool) error
}

type ImageRegistryAPI interface {
	GetImageSizeAndDigest(ctx context.Context, repo, tag string) (int64, string, error)
	DeleteManifest(ctx context.Context, repo, digest string) error
}

type ImageService struct {
	repo        ImageRepository
	buildRepo   BuildRepository
	dockerAPI   ImageDockerAPI
	registryAPI ImageRegistryAPI
	registryURL string
}

func NewImageService(repo ImageRepository, buildRepo BuildRepository, dockerAPI ImageDockerAPI, registryAPI ImageRegistryAPI, registryURL string) *ImageService {
	return &ImageService{
		repo:        repo,
		buildRepo:   buildRepo,
		dockerAPI:   dockerAPI,
		registryAPI: registryAPI,
		registryURL: registryURL,
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

	repoName := strings.ToLower(fmt.Sprintf("%s_%s", img.OwnerID.String(), img.Tag))

	// 1. Удаляем манифест из локального Registry
	_, digest, err := s.registryAPI.GetImageSizeAndDigest(ctx, repoName, "latest")
	if err == nil && digest != "" {
		_ = s.registryAPI.DeleteManifest(ctx, repoName, digest)
	}

	// 2. Удаляем кэш образа из Docker Engine (если он пуллился)
	fullTag := fmt.Sprintf("%s/%s:latest", s.registryURL, repoName)
	_ = s.dockerAPI.RemoveImage(ctx, fullTag, false)

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

	if logFilePath == "" {
		logFilePath = buildID.String() + ".log"
	}

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

func (s *ImageService) CompleteBuildRecord(ctx context.Context, buildID, imageID uuid.UUID, status string, _ int) error {
	if status != domain.BuildStatusSuccess {
		return s.repo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, status)
	}

	img, err := s.repo.GetByID(ctx, imageID)
	if err != nil {
		return err
	}

	repoName := strings.ToLower(fmt.Sprintf("%s_%s", img.OwnerID.String(), img.Tag))

	// Запрашиваем реальный размер образа из Registry API
	sizeBytes, digest, err := s.registryAPI.GetImageSizeAndDigest(ctx, repoName, "latest")
	if err != nil {
		return s.repo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, "failed_registry_error")
	}

	sizeMB := int(sizeBytes / (1024 * 1024))
	if sizeMB == 0 {
		sizeMB = 1 // Минимальный размер 1 МБ
	}

	quotaMB, err := s.repo.GetUserDiskQuota(ctx, img.OwnerID)
	if err != nil {
		return err
	}

	usedMB, err := s.repo.GetUserUsedDiskSpace(ctx, img.OwnerID)
	if err != nil {
		return err
	}

	// Admission Control 2: Проверка квоты по реальному размеру
	if usedMB+int64(sizeMB) > quotaMB {
		// Удаляем из Registry
		_ = s.registryAPI.DeleteManifest(ctx, repoName, digest)

		_ = s.repo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, "failed_quota_exceeded")
		return apperrors.ErrQuotaExceeded
	}

	return s.repo.UpdateBuildAndImageSizeTx(ctx, buildID, imageID, status, sizeMB)
}

func (s *ImageService) GetUserBuilds(ctx context.Context, ownerID uuid.UUID) ([]domain.Build, error) {
	return s.buildRepo.GetUserBuilds(ctx, ownerID)
}

func (s *ImageService) DeleteBuild(ctx context.Context, ownerID, buildID uuid.UUID) error {
	b, err := s.buildRepo.GetByID(ctx, buildID)
	if err != nil {
		return err
	}

	img, err := s.repo.GetByID(ctx, b.ImageID)
	if err != nil {
		return err
	}

	if img.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	return s.buildRepo.Delete(ctx, buildID)
}
