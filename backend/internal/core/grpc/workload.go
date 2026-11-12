package grpc

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
)

// WorkloadLogic acts on all of one user's workloads.
type WorkloadLogic interface {
	SuspendUserWorkloads(ctx context.Context, ownerID uuid.UUID) (model.WorkloadSuspension, error)
}

type WorkloadHandler struct {
	coreapi.UnimplementedWorkloadAPIServer
	logic WorkloadLogic
}

func RegisterWorkloadAPI(gRPCServer *grpc.Server, logic WorkloadLogic) {
	coreapi.RegisterWorkloadAPIServer(gRPCServer, &WorkloadHandler{logic: logic})
}

func (h *WorkloadHandler) SuspendUserWorkloads(ctx context.Context, req *coreapi.SuspendUserWorkloadsRequest) (*coreapi.SuspendUserWorkloadsResponse, error) {
	if err := requireAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	ownerID, err := uuid.Parse(req.GetUserId())
	if err != nil || ownerID == uuid.Nil {
		return nil, grpcerrors.ToGRPC(apperrors.New(apperrors.ErrBadRequest, "user_id must be a valid UUID"))
	}
	result, err := h.logic.SuspendUserWorkloads(ctx, ownerID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return &coreapi.SuspendUserWorkloadsResponse{
		StoppedContainers:   int32(result.StoppedContainers),
		SkippedContainers:   int32(result.SkippedContainers),
		CanceledBuilds:      int32(result.CanceledBuilds),
		CanceledDeployments: int32(result.CanceledDeployments),
		Failed:              int32(result.Failed),
	}, nil
}
