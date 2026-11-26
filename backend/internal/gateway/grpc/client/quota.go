package client

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
)

func (c *CoreClient) GetUserQuotas(ctx context.Context, userIDs []string) ([]model.UserQuota, error) {
	resp, err := c.quotaAPI.GetUserQuotas(ctx, &coreapi.GetUserQuotasRequest{UserIds: userIDs})
	if err != nil {
		return nil, grpcerrors.FromGRPC(err)
	}
	quotas := make([]model.UserQuota, 0, len(resp.GetQuotas()))
	for _, quota := range resp.GetQuotas() {
		quotas = append(quotas, userQuotaFromProto(quota))
	}
	return quotas, nil
}

func (c *CoreClient) SetUserQuota(ctx context.Context, userID string, limits model.UserQuotaLimits) (model.UserQuota, error) {
	resp, err := c.quotaAPI.SetUserQuota(ctx, &coreapi.SetUserQuotaRequest{
		UserId: userID,
		Cpu:    limits.CPU,
		RamMb:  limits.RAMMB,
		DiskMb: limits.DiskMB,
	})
	if err != nil {
		return model.UserQuota{}, grpcerrors.FromGRPC(err)
	}
	return userQuotaFromProto(resp), nil
}

func (c *CoreClient) ResetUserQuota(ctx context.Context, userID string) (model.UserQuota, error) {
	resp, err := c.quotaAPI.ResetUserQuota(ctx, &coreapi.UserQuotaActionRequest{UserId: userID})
	if err != nil {
		return model.UserQuota{}, grpcerrors.FromGRPC(err)
	}
	return userQuotaFromProto(resp), nil
}

func userQuotaFromProto(quota *coreapi.UserQuotaData) model.UserQuota {
	return model.UserQuota{
		UserID: quota.GetUserId(),
		CPU:    quota.GetCpu(),
		RAMMB:  quota.GetRamMb(),
		DiskMB: quota.GetDiskMb(),
		Custom: quota.GetCustom(),
	}
}
