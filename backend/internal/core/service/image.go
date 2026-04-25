package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/validation"
	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type ImageRepository interface {
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]model.Image, error)
	GetByID(ctx context.Context, id uuid.UUID) (model.Image, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Save(ctx context.Context, img model.Image) error
	UpdateSize(ctx context.Context, id uuid.UUID, sizeMB int) error
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
	UpdateBuildAndImageSizeTx(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error
	MarkBuildFailedAndDeleteImageTx(ctx context.Context, buildID, imageID uuid.UUID, status string) error
	GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Image, int, error)
}

type ImageContainerRepository interface {
	IsImageInUse(ctx context.Context, ownerID uuid.UUID, imageTag string) (bool, error)
}

type BuildRepository interface {
	Save(ctx context.Context, b model.Build) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	GetUserBuilds(ctx context.Context, ownerID uuid.UUID) ([]model.Build, error)
	GetByID(ctx context.Context, id uuid.UUID) (model.Build, error)
	Delete(ctx context.Context, id uuid.UUID) error
	GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Build, int, error)
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
	contRepo    ImageContainerRepository
	cfg         ConfigManager
	users       UserInfoProvider
}

func NewImageService(
	repo ImageRepository,
	buildRepo BuildRepository,
	dockerAPI ImageDockerAPI,
	registryAPI ImageRegistryAPI,
	contRepo ImageContainerRepository,
	cfg ConfigManager,
	users UserInfoProvider,
) *ImageService {
	return &ImageService{
		repo:        repo,
		buildRepo:   buildRepo,
		dockerAPI:   dockerAPI,
		registryAPI: registryAPI,
		contRepo:    contRepo,
		cfg:         cfg,
		users:       users,
	}
}

func (s *ImageService) GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Image, error) {
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

	inUse, err := s.contRepo.IsImageInUse(ctx, ownerID, img.Tag)
	if err != nil {
		return err
	}
	if inUse {
		return fmt.Errorf("conflict: unable to remove image, it is currently in use by a container")
	}

	baseName, version := parseImageTag(img.Tag)
	repoName := strings.ToLower(fmt.Sprintf("%s_%s", img.OwnerID.String(), baseName))

	// Удаление из Registry (Soft Delete)
	_, digest, err := s.registryAPI.GetImageSizeAndDigest(ctx, repoName, version)
	if err == nil && digest != "" {
		_ = s.registryAPI.DeleteManifest(ctx, repoName, digest)
	}

	// Удаление из локального кэша Docker Engine
	fullTag := fmt.Sprintf("%s/%s:%s", s.cfg.Get().RegistryPublicURL, repoName, version)
	_ = s.dockerAPI.RemoveImage(ctx, fullTag, false)

	// Удаление записи из бд
	return s.repo.Delete(ctx, imageID)
}

func (s *ImageService) InitBuildRecord(ctx context.Context, ownerID uuid.UUID, tag string, logFilePath string) (uuid.UUID, uuid.UUID, error) {
	if err := validation.ImageTag(tag); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}

	// 1. Предварительная проверка дисковой квоты ДО сборки
	// Мы не знаем размер будущего образа, но если квота УЖЕ исчерпана, нет смысла начинать сборку.
	user, err := s.users.GetUser(ctx, ownerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	quotaMB := user.QuotaDiskMB

	usedMB, err := s.repo.GetUserUsedDiskSpace(ctx, ownerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	if usedMB >= quotaMB {
		return uuid.Nil, uuid.Nil, apperrors.ErrQuotaExceeded
	}

	baseName, version := parseImageTag(tag)
	normalizedTag := fmt.Sprintf("%s:%s", baseName, version)

	// 2. Резервируем "пустой" образ в БД
	imageID := uuid.New()
	img := model.Image{
		ID:       imageID,
		OwnerID:  ownerID,
		Tag:      normalizedTag,
		SizeMB:   0,
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

	build := model.Build{
		ID:          buildID,
		ImageID:     imageID,
		Status:      model.BuildStatusPending, // Или "running", так как процесс уже пошел
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
	if status != model.BuildStatusSuccess {
		return s.repo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, status)
	}

	img, err := s.repo.GetByID(ctx, imageID)
	if err != nil {
		return err
	}

	baseName, version := parseImageTag(img.Tag)
	repoName := strings.ToLower(fmt.Sprintf("%s_%s", img.OwnerID.String(), baseName))

	// Запрашиваем реальный размер образа из Registry API
	sizeBytes, digest, err := s.registryAPI.GetImageSizeAndDigest(ctx, repoName, version)
	if err != nil {
		return s.repo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, "failed")
	}

	sizeMB := int(sizeBytes / (1024 * 1024))
	if sizeMB == 0 {
		sizeMB = 1 // Минимальный размер 1 МБ
	}

	user, err := s.users.GetUser(ctx, img.OwnerID)
	if err != nil {
		return err
	}
	quotaMB := user.QuotaDiskMB

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

func (s *ImageService) GetUserBuilds(ctx context.Context, ownerID uuid.UUID) ([]model.Build, error) {
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

func parseImageTag(rawTag string) (baseName, version string) {
	parts := strings.SplitN(rawTag, ":", 2)
	if len(parts) == 1 || parts[1] == "" {
		return parts[0], "latest"
	}
	return parts[0], parts[1]
}

func (s *ImageService) GetAllPaginatedImages(ctx context.Context, limit, offset int) ([]model.Image, int, error) {
	return s.repo.GetAllPaginated(ctx, limit, offset)
}

func (s *ImageService) AdminDeleteImage(ctx context.Context, imageID uuid.UUID) error {
	img, err := s.repo.GetByID(ctx, imageID)
	if err != nil {
		return err
	}

	if !img.IsCustom {
		return errors.New("cannot delete system image")
	}

	inUse, err := s.contRepo.IsImageInUse(ctx, img.OwnerID, img.Tag)
	if err != nil {
		return err
	}
	if inUse {
		return fmt.Errorf("conflict: unable to remove image, it is currently in use")
	}

	baseName, version := parseImageTag(img.Tag)
	repoName := strings.ToLower(fmt.Sprintf("%s_%s", img.OwnerID.String(), baseName))

	_, digest, err := s.registryAPI.GetImageSizeAndDigest(ctx, repoName, version)
	if err == nil && digest != "" {
		_ = s.registryAPI.DeleteManifest(ctx, repoName, digest)
	}

	fullTag := fmt.Sprintf("%s/%s:%s", s.cfg.Get().RegistryPublicURL, repoName, version)
	_ = s.dockerAPI.RemoveImage(ctx, fullTag, false)

	return s.repo.Delete(ctx, imageID)
}

func (s *ImageService) GetAllPaginatedBuilds(ctx context.Context, limit, offset int) ([]model.Build, int, error) {
	return s.buildRepo.GetAllPaginated(ctx, limit, offset)
}

func (s *ImageService) AdminDeleteBuild(ctx context.Context, buildID uuid.UUID) error {
	_, err := s.buildRepo.GetByID(ctx, buildID)
	if err != nil {
		return err
	}
	return s.buildRepo.Delete(ctx, buildID)
}
