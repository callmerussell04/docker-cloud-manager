package grpc

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ProjectLogic interface {
	GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Project, error)
	Delete(ctx context.Context, ownerID, projectID uuid.UUID) error
	Stop(ctx context.Context, ownerID, projectID uuid.UUID) error
	GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Project, int, error)
	AdminStop(ctx context.Context, projectID uuid.UUID) error
	AdminDelete(ctx context.Context, projectID uuid.UUID) error
}

type ProjectHandler struct {
	coreapi.UnimplementedProjectAPIServer
	logic ProjectLogic
	users UserDirectory
}

func RegisterProjectAPI(gRPCServer *grpc.Server, logic ProjectLogic, users UserDirectory) {
	coreapi.RegisterProjectAPIServer(gRPCServer, &ProjectHandler{logic: logic, users: users})
}

func (h *ProjectHandler) GetUserProjects(ctx context.Context, req *coreapi.GetUserRequest) (*coreapi.ProjectListResponse, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	projects, err := h.logic.GetByOwner(ctx, ownerID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	var pbProjects []*coreapi.ProjectData
	for _, p := range projects {

		errMsg := ""
		if p.ErrorMessage != nil {
			errMsg = *p.ErrorMessage
		}

		pbProjects = append(pbProjects, &coreapi.ProjectData{
			Id:           p.ID.String(),
			Name:         p.Name,
			Status:       p.Status,
			ErrorMessage: errMsg,
			CreatedAt:    p.CreatedAt.Unix(),
		})
	}

	return &coreapi.ProjectListResponse{Projects: pbProjects}, nil
}

func (h *ProjectHandler) DeleteProject(ctx context.Context, req *coreapi.ProjectActionRequest) (*coreapi.Empty, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	projectID, err := uuid.Parse(req.GetProjectId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid project_id format")
	}

	err = h.logic.Delete(ctx, ownerID, projectID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

func (h *ProjectHandler) StopProject(ctx context.Context, req *coreapi.ProjectActionRequest) (*coreapi.Empty, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	projectID, err := uuid.Parse(req.GetProjectId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid project_id format")
	}

	err = h.logic.Stop(ctx, ownerID, projectID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

func (h *ProjectHandler) GetAllProjects(ctx context.Context, req *coreapi.PaginationRequest) (*coreapi.PaginatedProjectResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := (int(req.GetPage()) - 1) * limit
	if offset < 0 {
		offset = 0
	}

	projects, total, err := h.logic.GetAllPaginated(ctx, limit, offset)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	var pbProjects []*coreapi.ProjectData
	usernames := h.usernamesByOwner(ctx, projects)
	for _, p := range projects {
		errMsg := ""
		if p.ErrorMessage != nil {
			errMsg = *p.ErrorMessage
		}
		pbProjects = append(pbProjects, &coreapi.ProjectData{
			Id:            p.ID.String(),
			Name:          p.Name,
			Status:        p.Status,
			ErrorMessage:  errMsg,
			CreatedAt:     p.CreatedAt.Unix(),
			OwnerId:       p.OwnerID.String(),
			OwnerUsername: usernames[p.OwnerID],
		})
	}

	return &coreapi.PaginatedProjectResponse{
		Projects:   pbProjects,
		TotalCount: int32(total),
	}, nil
}

func (h *ProjectHandler) usernamesByOwner(ctx context.Context, projects []model.Project) map[uuid.UUID]string {
	ids := make([]uuid.UUID, 0, len(projects))
	seen := make(map[uuid.UUID]struct{}, len(projects))
	for _, p := range projects {
		if _, ok := seen[p.OwnerID]; ok {
			continue
		}
		seen[p.OwnerID] = struct{}{}
		ids = append(ids, p.OwnerID)
	}
	return usernamesByID(ctx, h.users, ids)
}

func (h *ProjectHandler) AdminDeleteProject(ctx context.Context, req *coreapi.ProjectActionRequest) (*coreapi.Empty, error) {
	projectID, err := uuid.Parse(req.GetProjectId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid project_id")
	}

	if err := h.logic.AdminDelete(ctx, projectID); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return &coreapi.Empty{}, nil
}

func (h *ProjectHandler) AdminStopProject(ctx context.Context, req *coreapi.ProjectActionRequest) (*coreapi.Empty, error) {
	projectID, err := uuid.Parse(req.GetProjectId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid project_id")
	}

	if err := h.logic.AdminStop(ctx, projectID); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return &coreapi.Empty{}, nil
}
