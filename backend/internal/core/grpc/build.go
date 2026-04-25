package grpc

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type BuildLogic interface {
	InitBuildRecord(ctx context.Context, ownerID uuid.UUID, tag string, logFilePath string) (uuid.UUID, uuid.UUID, error)
	CompleteBuildRecord(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error
	GetUserBuilds(ctx context.Context, ownerID uuid.UUID) ([]model.Build, error)
	DeleteBuild(ctx context.Context, ownerID, buildID uuid.UUID) error
	GetAllPaginatedBuilds(ctx context.Context, limit, offset int) ([]model.Build, int, error)
	AdminDeleteBuild(ctx context.Context, buildID uuid.UUID) error
}

func (h *ImageHandler) InitBuildRecord(ctx context.Context, req *coreapi.InitBuildRequest) (*coreapi.InitBuildResponse, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	if req.GetTag() == "" {
		return nil, status.Error(codes.InvalidArgument, "image tag is required")
	}

	buildID, imageID, err := h.buildLogic.InitBuildRecord(ctx, ownerID, req.GetTag(), req.GetLogFilePath())
	if err != nil {
		if errors.Is(err, apperrors.ErrBadRequest) {
			return nil, status.Error(codes.InvalidArgument, "invalid image tag")
		}
		if errors.Is(err, apperrors.ErrAlreadyExists) {
			return nil, status.Error(codes.AlreadyExists, "image already exists")
		}
		if errors.Is(err, apperrors.ErrQuotaExceeded) {
			return nil, status.Error(codes.ResourceExhausted, "disk quota exceeded")
		}
		return nil, status.Error(codes.Internal, "failed to initialize build record")
	}

	return &coreapi.InitBuildResponse{
		BuildId: buildID.String(),
		ImageId: imageID.String(),
	}, nil
}

func (h *ImageHandler) CompleteBuildRecord(ctx context.Context, req *coreapi.CompleteBuildRequest) (*coreapi.Empty, error) {
	buildID, err := uuid.Parse(req.GetBuildId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid build_id format")
	}

	imageID, err := uuid.Parse(req.GetImageId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid image_id format")
	}

	err = h.buildLogic.CompleteBuildRecord(ctx, buildID, imageID, req.GetStatus(), int(req.GetSizeMb()))
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "build or image not found")
		}
		if errors.Is(err, apperrors.ErrQuotaExceeded) {
			return nil, status.Error(codes.ResourceExhausted, "image size exceeds user quota, image removed")
		}
		return nil, status.Error(codes.Internal, "failed to complete build record")
	}

	return &coreapi.Empty{}, nil
}

func (h *ImageHandler) GetUserBuilds(ctx context.Context, req *coreapi.GetUserRequest) (*coreapi.BuildListResponse, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	builds, err := h.buildLogic.GetUserBuilds(ctx, ownerID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to retrieve builds")
	}

	var pbBuilds []*coreapi.BuildData
	for _, b := range builds {
		pbBuilds = append(pbBuilds, buildToProto(b, ""))
	}

	return &coreapi.BuildListResponse{
		Builds: pbBuilds,
	}, nil
}

func (h *ImageHandler) DeleteBuild(ctx context.Context, req *coreapi.BuildActionRequest) (*coreapi.Empty, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	buildID, err := uuid.Parse(req.GetBuildId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid build_id format")
	}

	err = h.buildLogic.DeleteBuild(ctx, ownerID, buildID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "build not found")
		}
		return nil, status.Error(codes.Internal, "failed to delete build")
	}

	return &coreapi.Empty{}, nil
}

func (h *ImageHandler) GetAllBuilds(ctx context.Context, req *coreapi.PaginationRequest) (*coreapi.PaginatedBuildResponse, error) {
	limit, offset := pagination(req)

	builds, total, err := h.buildLogic.GetAllPaginatedBuilds(ctx, limit, offset)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get builds")
	}

	var pbBuilds []*coreapi.BuildData
	usernames := h.usernamesByBuildOwner(ctx, builds)
	for _, b := range builds {
		pbBuilds = append(pbBuilds, buildToProto(b, usernames[b.OwnerID]))
	}

	return &coreapi.PaginatedBuildResponse{
		Builds:     pbBuilds,
		TotalCount: int32(total),
	}, nil
}

func (h *ImageHandler) AdminDeleteBuild(ctx context.Context, req *coreapi.BuildActionRequest) (*coreapi.Empty, error) {
	buildID, err := uuid.Parse(req.GetBuildId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid build_id")
	}

	if err := h.buildLogic.AdminDeleteBuild(ctx, buildID); err != nil {
		return nil, status.Error(codes.Internal, "failed to delete build")
	}
	return &coreapi.Empty{}, nil
}

func (h *ImageHandler) usernamesByBuildOwner(ctx context.Context, builds []model.Build) map[uuid.UUID]string {
	ids := make([]uuid.UUID, 0, len(builds))
	seen := make(map[uuid.UUID]struct{}, len(builds))
	for _, build := range builds {
		if _, ok := seen[build.OwnerID]; ok {
			continue
		}
		seen[build.OwnerID] = struct{}{}
		ids = append(ids, build.OwnerID)
	}
	return usernamesByID(ctx, h.users, ids)
}

func buildToProto(b model.Build, ownerUsername string) *coreapi.BuildData {
	var finishedAt int64
	if b.FinishedAt != nil {
		finishedAt = b.FinishedAt.Unix()
	}
	return &coreapi.BuildData{
		Id:            b.ID.String(),
		ImageId:       b.ImageID.String(),
		Status:        b.Status,
		StartedAt:     b.StartedAt.Unix(),
		FinishedAt:    finishedAt,
		LogFilePath:   b.LogFilePath,
		OwnerId:       b.OwnerID.String(),
		OwnerUsername: ownerUsername,
	}
}
