package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	"github.com/google/uuid"
)

type BuildRepository interface {
	Save(ctx context.Context, b model.Build) error
	CreateQueuedBuild(ctx context.Context, img model.Image, build model.Build, outbox model.BuildQueueOutbox) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Build, error)
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, opts model.ListOptions) ([]model.Build, int, error)
}

type BuildImageRepository interface {
	Save(ctx context.Context, img model.Image) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Image, error)
	Delete(ctx context.Context, id uuid.UUID) error
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
	UpdateBuildAndImageSizeTx(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error
	MarkBuildFailedAndDeleteImageTx(ctx context.Context, buildID, imageID uuid.UUID, status string) error
}

type BuildVolumeDiskRepository interface {
	GetUserUsedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type BuildService struct {
	repo        BuildRepository
	imageRepo   BuildImageRepository
	volumeRepo  BuildVolumeDiskRepository
	registryAPI ImageRegistryAPI
	users       UserInfoProvider
	logger      *slog.Logger
}

func NewBuildService(repo BuildRepository, imageRepo BuildImageRepository, registryAPI ImageRegistryAPI, users UserInfoProvider, logger *slog.Logger, deps ...any) *BuildService {
	s := &BuildService{
		repo:        repo,
		imageRepo:   imageRepo,
		registryAPI: registryAPI,
		users:       users,
		logger:      logging.WithComponent(logger, "build_service"),
	}
	for _, dep := range deps {
		if v, ok := dep.(BuildVolumeDiskRepository); ok {
			s.volumeRepo = v
		}
	}
	return s
}

func (s *BuildService) getUserUsedDiskMB(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	usedMB, err := s.imageRepo.GetUserUsedDiskSpace(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	if s.volumeRepo != nil {
		usedBytes, err := s.volumeRepo.GetUserUsedVolumeBytes(ctx, ownerID)
		if err != nil {
			return 0, err
		}
		usedMB += bytesToMBRoundedUp(usedBytes)
	}
	return usedMB, nil
}

func (s *BuildService) InitBuildRecord(ctx context.Context, tag string, logFilePath string) (uuid.UUID, uuid.UUID, error) {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := validation.ImageTag(tag); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}

	user, err := s.users.GetUser(ctx, ownerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	usedMB, err := s.getUserUsedDiskMB(ctx, ownerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if usedMB >= user.QuotaDiskMB {
		return uuid.Nil, uuid.Nil, apperrors.New(apperrors.ErrQuotaExceeded, "user disk quota exceeded")
	}

	baseName, version := parseImageTag(tag)
	imageID := uuid.New()
	img := model.Image{
		ID:      imageID,
		OwnerID: ownerID,
		Tag:     fmt.Sprintf("%s:%s", baseName, version),
		SizeMB:  0,
		Status:  model.ImageStatusBuilding,
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

func (s *BuildService) CreateBuildJob(ctx context.Context, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (uuid.UUID, uuid.UUID, error) {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := validation.ImageTag(tag); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if archiveObjectKey == "" || logObjectKey == "" {
		return uuid.Nil, uuid.Nil, apperrors.New(apperrors.ErrBadRequest, "build archive and log object keys are required")
	}

	user, err := s.users.GetUser(ctx, ownerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	usedMB, err := s.getUserUsedDiskMB(ctx, ownerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if usedMB >= user.QuotaDiskMB {
		return uuid.Nil, uuid.Nil, apperrors.New(apperrors.ErrQuotaExceeded, "user disk quota exceeded")
	}

	baseName, version := parseImageTag(tag)
	normalizedTag := fmt.Sprintf("%s:%s", baseName, version)
	imageID := uuid.New()
	buildID := uuid.New()
	now := time.Now()

	message := buildqueue.ImageBuildMessage{
		BuildID:          buildID.String(),
		ImageID:          imageID.String(),
		OwnerID:          ownerID.String(),
		Tag:              normalizedTag,
		ArchiveObjectKey: archiveObjectKey,
		LogObjectKey:     logObjectKey,
		ContextDir:       contextDir,
		Dockerfile:       dockerfile,
		BuildArgs:        buildArgs,
		RequestID:        requestID,
		CreatedAt:        now.Unix(),
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("failed to marshal build queue message: %w", err)
	}

	img := model.Image{
		ID:      imageID,
		OwnerID: ownerID,
		Tag:     normalizedTag,
		SizeMB:  0,
		Status:  model.ImageStatusBuilding,
	}
	build := model.Build{
		ID:               buildID,
		ImageID:          imageID,
		OwnerID:          ownerID,
		Status:           model.BuildStatusPending,
		LogFilePath:      logObjectKey,
		ArchiveObjectKey: archiveObjectKey,
		StartedAt:        now,
	}
	outbox := model.BuildQueueOutbox{
		ID:         uuid.New(),
		BuildID:    buildID,
		Exchange:   buildqueue.ExchangeName,
		RoutingKey: buildqueue.RoutingKey,
		Payload:    payload,
		Status:     model.BuildOutboxStatusPending,
	}

	if err := s.repo.CreateQueuedBuild(ctx, img, build, outbox); err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	s.logger.InfoContext(ctx, "build job created", "build_id", buildID, "image_id", imageID, "owner_id", ownerID, "image_tag", normalizedTag)
	return buildID, imageID, nil
}

func (s *BuildService) StartBuildRecord(ctx context.Context, buildID uuid.UUID) (model.Build, bool, error) {
	b, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return model.Build{}, false, err
	}
	if model.IsBuildTerminalStatus(b.Status) {
		return b, false, nil
	}
	if err := s.repo.UpdateStatus(ctx, buildID, model.BuildStatusRunning); err != nil {
		return model.Build{}, false, err
	}
	b.Status = model.BuildStatusRunning
	s.logger.InfoContext(ctx, "build record started", "build_id", buildID, "image_id", b.ImageID, "owner_id", b.OwnerID)
	return b, true, nil
}

func (s *BuildService) CancelBuildRecord(ctx context.Context, buildID uuid.UUID) error {
	b, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return err
	}
	if model.IsBuildTerminalStatus(b.Status) {
		return nil
	}
	s.logger.WarnContext(ctx, "build record cancelled", "build_id", buildID, "image_id", b.ImageID, "owner_id", b.OwnerID)
	return s.imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, b.ID, b.ImageID, model.BuildStatusFailed)
}

func (s *BuildService) CompleteBuildRecord(ctx context.Context, buildID, imageID uuid.UUID, status string, _ int) error {
	b, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return err
	}
	if model.IsBuildTerminalStatus(b.Status) {
		return nil
	}

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
	usedMB, err := s.getUserUsedDiskMB(ctx, img.OwnerID)
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

func (s *BuildService) GetBuild(ctx context.Context, buildID uuid.UUID) (model.Build, error) {
	b, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return model.Build{}, err
	}
	if err := accessscope.RequireOwnerAccess(ctx, b.OwnerID); err != nil {
		return model.Build{}, err
	}
	return b, nil
}

func (s *BuildService) DeleteBuild(ctx context.Context, buildID uuid.UUID) error {
	b, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, b.OwnerID); err != nil {
		return err
	}
	return s.repo.Delete(ctx, buildID)
}

func (s *BuildService) List(ctx context.Context, limit, offset int) ([]model.Build, int, error) {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.List(ctx, model.ListOptions{
		OwnerID: scope.OwnerFilter(),
		Limit:   limit,
		Offset:  offset,
	})
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
