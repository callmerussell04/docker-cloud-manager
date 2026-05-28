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
	List(ctx context.Context, limit, offset int) ([]model.Project, int, error)
	Delete(ctx context.Context, projectID uuid.UUID) error
	Start(ctx context.Context, projectID uuid.UUID) error
	Stop(ctx context.Context, projectID uuid.UUID) error
	Cancel(ctx context.Context, projectID uuid.UUID) error
}

type ProjectHandler struct {
	coreapi.UnimplementedProjectAPIServer
	logic ProjectLogic
	users UserDirectory
}

func RegisterProjectAPI(gRPCServer *grpc.Server, logic ProjectLogic, users UserDirectory) {
	coreapi.RegisterProjectAPIServer(gRPCServer, &ProjectHandler{logic: logic, users: users})
}

func (h *ProjectHandler) DeleteProject(ctx context.Context, req *coreapi.ProjectActionRequest) (*coreapi.Empty, error) {
	if err := requireUserOrAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	projectID, err := uuid.Parse(req.GetProjectId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid project_id format")
	}

	err = h.logic.Delete(ctx, projectID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

func (h *ProjectHandler) StartProject(ctx context.Context, req *coreapi.ProjectActionRequest) (*coreapi.Empty, error) {
	if err := requireUserOrAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	projectID, err := uuid.Parse(req.GetProjectId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid project_id format")
	}

	err = h.logic.Start(ctx, projectID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

func (h *ProjectHandler) StopProject(ctx context.Context, req *coreapi.ProjectActionRequest) (*coreapi.Empty, error) {
	if err := requireUserOrAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	projectID, err := uuid.Parse(req.GetProjectId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid project_id format")
	}

	err = h.logic.Stop(ctx, projectID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

func (h *ProjectHandler) CancelProject(ctx context.Context, req *coreapi.ProjectActionRequest) (*coreapi.Empty, error) {
	if err := requireUserOrAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	projectID, err := uuid.Parse(req.GetProjectId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid project_id format")
	}

	err = h.logic.Cancel(ctx, projectID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

func (h *ProjectHandler) ListProjects(ctx context.Context, req *coreapi.PaginationRequest) (*coreapi.PaginatedProjectResponse, error) {
	if err := requireUserOrAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	limit, offset := pagination(req)
	projects, total, err := h.logic.List(ctx, limit, offset)
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
			LastError:     errMsg,
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
