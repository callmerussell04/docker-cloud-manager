package grpc

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
)

type BuildLogic interface {
	StartBuildRecord(ctx context.Context, buildID uuid.UUID) (model.Build, bool, error)
	CancelBuildRecord(ctx context.Context, buildID uuid.UUID) error
	CompleteBuildRecord(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error
	List(ctx context.Context, limit, offset int) ([]model.Build, int, error)
	GetBuild(ctx context.Context, buildID uuid.UUID) (model.Build, error)
	DeleteBuild(ctx context.Context, buildID uuid.UUID) error
}

func (h *ImageHandler) StartBuildRecord(ctx context.Context, req *coreapi.BuildActionRequest) (*coreapi.StartBuildRecordResponse, error) {
	buildID, err := uuid.Parse(req.GetBuildId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid build_id format")
	}

	build, started, err := h.buildLogic.StartBuildRecord(ctx, buildID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.StartBuildRecordResponse{
		Started: started,
		Status:  build.Status,
		ImageId: build.ImageID.String(),
	}, nil
}

func (h *ImageHandler) CancelBuildRecord(ctx context.Context, req *coreapi.BuildActionRequest) (*coreapi.Empty, error) {
	buildID, err := uuid.Parse(req.GetBuildId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid build_id format")
	}

	if err := h.buildLogic.CancelBuildRecord(ctx, buildID); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return &coreapi.Empty{}, nil
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
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

func (h *ImageHandler) GetBuild(ctx context.Context, req *coreapi.BuildActionRequest) (*coreapi.BuildData, error) {
	buildID, err := uuid.Parse(req.GetBuildId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid build_id format")
	}

	build, err := h.buildLogic.GetBuild(ctx, buildID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return buildToProto(build, ""), nil
}

func (h *ImageHandler) DeleteBuild(ctx context.Context, req *coreapi.BuildActionRequest) (*coreapi.Empty, error) {
	buildID, err := uuid.Parse(req.GetBuildId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid build_id format")
	}

	err = h.buildLogic.DeleteBuild(ctx, buildID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

func (h *ImageHandler) ListBuilds(ctx context.Context, req *coreapi.PaginationRequest) (*coreapi.PaginatedBuildResponse, error) {
	limit, offset := pagination(req)

	builds, total, err := h.buildLogic.List(ctx, limit, offset)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
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
	projectID := ""
	if b.ProjectID != nil {
		projectID = b.ProjectID.String()
	}
	return &coreapi.BuildData{
		Id:                 b.ID.String(),
		ImageId:            b.ImageID.String(),
		Status:             b.Status,
		StartedAt:          b.StartedAt.Unix(),
		FinishedAt:         finishedAt,
		LogFilePath:        b.LogFilePath,
		OwnerId:            b.OwnerID.String(),
		OwnerUsername:      ownerUsername,
		ProjectId:          projectID,
		ProjectServiceName: b.ProjectServiceName,
	}
}
