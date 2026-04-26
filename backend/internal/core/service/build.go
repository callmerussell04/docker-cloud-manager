package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	"github.com/google/uuid"
)

type BuildRepository interface {
	Save(ctx context.Context, b model.Build) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	GetUserBuilds(ctx context.Context, ownerID uuid.UUID) ([]model.Build, error)
	GetByID(ctx context.Context, id uuid.UUID) (model.Build, error)
	Delete(ctx context.Context, id uuid.UUID) error
	GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Build, int, error)
}

type BuildImageRepository interface {
	Save(ctx context.Context, img model.Image) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Image, error)
	Delete(ctx context.Context, id uuid.UUID) error
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
	UpdateBuildAndImageSizeTx(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error
	MarkBuildFailedAndDeleteImageTx(ctx context.Context, buildID, imageID uuid.UUID, status string) error
}

type BuildService struct {
	repo        BuildRepository
	imageRepo   BuildImageRepository
	registryAPI ImageRegistryAPI
	users       UserInfoProvider
	logger      *slog.Logger
}

func NewBuildService(repo BuildRepository, imageRepo BuildImageRepository, registryAPI ImageRegistryAPI, users UserInfoProvider, logger *slog.Logger) *BuildService {
	return &BuildService{
		repo:        repo,
		imageRepo:   imageRepo,
		registryAPI: registryAPI,
		users:       users,
		logger:      logging.WithComponent(logger, "build_service"),
	}
}

func (s *BuildService) InitBuildRecord(ctx context.Context, ownerID uuid.UUID, tag string, logFilePath string) (uuid.UUID, uuid.UUID, error) {
	if err := validation.ImageTag(tag); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}

	user, err := s.users.GetUser(ctx, ownerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	usedMB, err := s.imageRepo.GetUserUsedDiskSpace(ctx, ownerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if usedMB >= user.QuotaDiskMB {
		return uuid.Nil, uuid.Nil, apperrors.New(apperrors.ErrQuotaExceeded, "user disk quota exceeded")
	}

	baseName, version := parseImageTag(tag)
	imageID := uuid.New()
	img := model.Image{
		ID:       imageID,
		OwnerID:  ownerID,
		Tag:      fmt.Sprintf("%s:%s", baseName, version),
		SizeMB:   0,
		IsCustom: true,
	}
	if err := s.imageRepo.Save(ctx, img); err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	buildID := uuid.New()
	if logFilePath == "" {
		logFilePath = buildID.String() + ".log"
	}
	build := model.Build{
		ID:          buildID,
		ImageID:     imageID,
		OwnerID:     ownerID,
		Status:      model.BuildStatusPending,
		LogFilePath: logFilePath,
		StartedAt:   time.Now(),
	}
	if err := s.repo.Save(ctx, build); err != nil {
		_ = s.imageRepo.Delete(ctx, imageID)
		return uuid.Nil, uuid.Nil, err
	}

	s.logger.InfoContext(ctx, "build record initialized", "build_id", buildID, "image_id", imageID, "owner_id", ownerID, "image_tag", img.Tag)
	return buildID, imageID, nil
}

func (s *BuildService) CompleteBuildRecord(ctx context.Context, buildID, imageID uuid.UUID, status string, _ int) error {
	if status != model.BuildStatusSuccess {
		status = normalizeFailedBuildStatus(status)
		s.logger.WarnContext(ctx, "build record marked failed", "build_id", buildID, "image_id", imageID, "status", status)
		return s.imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, status)
	}

	img, err := s.imageRepo.GetByID(ctx, imageID)
	if err != nil {
		return err
	}

	baseName, version := parseImageTag(img.Tag)
	repoName := strings.ToLower(fmt.Sprintf("%s_%s", img.OwnerID.String(), baseName))

	sizeBytes, digest, err := s.registryAPI.GetImageSizeAndDigest(ctx, repoName, version)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to fetch built image metadata", "build_id", buildID, "image_id", imageID, "error", err)
		return s.imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, model.BuildStatusFailedInternal)
	}

	sizeMB := int(sizeBytes / (1024 * 1024))
	if sizeMB == 0 {
		sizeMB = 1
	}

	user, err := s.users.GetUser(ctx, img.OwnerID)
	if err != nil {
		return err
	}
	usedMB, err := s.imageRepo.GetUserUsedDiskSpace(ctx, img.OwnerID)
	if err != nil {
		return err
	}

	if usedMB+int64(sizeMB) > user.QuotaDiskMB {
		_ = s.registryAPI.DeleteManifest(ctx, repoName, digest)
		_ = s.imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, model.BuildStatusFailedQuotaExceeded)
		s.logger.WarnContext(ctx, "built image rejected by disk quota", "build_id", buildID, "image_id", imageID, "owner_id", img.OwnerID, "size_mb", sizeMB)
		return apperrors.New(apperrors.ErrQuotaExceeded, "image size exceeds user disk quota, image removed")
	}

	if err := s.imageRepo.UpdateBuildAndImageSizeTx(ctx, buildID, imageID, status, sizeMB); err != nil {
		return err
	}
	s.logger.InfoContext(ctx, "build record completed", "build_id", buildID, "image_id", imageID, "owner_id", img.OwnerID, "size_mb", sizeMB)
	return nil
}

func (s *BuildService) GetUserBuilds(ctx context.Context, ownerID uuid.UUID) ([]model.Build, error) {
	return s.repo.GetUserBuilds(ctx, ownerID)
}

func (s *BuildService) DeleteBuild(ctx context.Context, ownerID, buildID uuid.UUID) error {
	b, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return err
	}
	if b.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	return s.repo.Delete(ctx, buildID)
}

func (s *BuildService) GetAllPaginatedBuilds(ctx context.Context, limit, offset int) ([]model.Build, int, error) {
	return s.repo.GetAllPaginated(ctx, limit, offset)
}

func (s *BuildService) AdminDeleteBuild(ctx context.Context, buildID uuid.UUID) error {
	_, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, buildID)
}

func normalizeFailedBuildStatus(status string) string {
	switch status {
	case model.BuildStatusFailed,
		model.BuildStatusFailedTimeout,
		model.BuildStatusFailedQuotaExceeded,
		model.BuildStatusFailedInternal:
		return status
	default:
		return model.BuildStatusFailedInternal
	}
}
