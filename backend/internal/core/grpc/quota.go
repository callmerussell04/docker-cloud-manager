package grpc

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// QuotaLogic is the administrators' quota use case.
type QuotaLogic interface {
	UserQuotas(ctx context.Context, ownerIDs []uuid.UUID) (map[uuid.UUID]model.UserQuota, error)
	SetUserQuota(ctx context.Context, ownerID uuid.UUID, limits model.UserQuotaLimits) (model.UserQuota, error)
	ResetUserQuota(ctx context.Context, ownerID uuid.UUID) (model.UserQuota, error)
}

type QuotaHandler struct {
	coreapi.UnimplementedQuotaAPIServer
	logic QuotaLogic
}

func RegisterQuotaAPI(gRPCServer *grpc.Server, logic QuotaLogic) {
	coreapi.RegisterQuotaAPIServer(gRPCServer, &QuotaHandler{logic: logic})
}

// GetUserQuotas answers in the order of the request.
func (h *QuotaHandler) GetUserQuotas(ctx context.Context, req *coreapi.GetUserQuotasRequest) (*coreapi.UserQuotasResponse, error) {
	if err := requireAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	ownerIDs := make([]uuid.UUID, 0, len(req.GetUserIds()))
	for _, raw := range req.GetUserIds() {
		ownerID, err := parseQuotaUserID(raw)
		if err != nil {
			return nil, grpcerrors.ToGRPC(err)
		}
		ownerIDs = append(ownerIDs, ownerID)
	}
	quotas, err := h.logic.UserQuotas(ctx, ownerIDs)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	resp := &coreapi.UserQuotasResponse{Quotas: make([]*coreapi.UserQuotaData, 0, len(ownerIDs))}
	for _, ownerID := range ownerIDs {
		resp.Quotas = append(resp.Quotas, userQuotaToProto(ownerID, quotas[ownerID]))
	}
	return resp, nil
}

func (h *QuotaHandler) SetUserQuota(ctx context.Context, req *coreapi.SetUserQuotaRequest) (*coreapi.UserQuotaData, error) {
	if err := requireAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	ownerID, err := parseQuotaUserID(req.GetUserId())
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	quota, err := h.logic.SetUserQuota(ctx, ownerID, model.UserQuotaLimits{
		CPUCores: req.GetCpu(),
		RAMMB:    req.GetRamMb(),
		DiskMB:   req.GetDiskMb(),
	})
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return userQuotaToProto(ownerID, quota), nil
}

func (h *QuotaHandler) ResetUserQuota(ctx context.Context, req *coreapi.UserQuotaActionRequest) (*coreapi.UserQuotaData, error) {
	if err := requireAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	ownerID, err := parseQuotaUserID(req.GetUserId())
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	quota, err := h.logic.ResetUserQuota(ctx, ownerID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return userQuotaToProto(ownerID, quota), nil
}

// parseQuotaUserID never echoes the rejected value.
func parseQuotaUserID(raw string) (uuid.UUID, error) {
	ownerID, err := uuid.Parse(raw)
	if err != nil || ownerID == uuid.Nil {
		return uuid.Nil, apperrors.New(apperrors.ErrBadRequest, "user_id must be a valid UUID")
	}
	return ownerID, nil
}

func userQuotaToProto(ownerID uuid.UUID, quota model.UserQuota) *coreapi.UserQuotaData {
	return &coreapi.UserQuotaData{
		UserId: ownerID.String(),
		Cpu:    float64(quota.CPUMillicores) / 1000,
		RamMb:  quota.RAMMB,
		DiskMb: quota.DiskMB,
		Custom: quota.Custom,
	}
}
