package client

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
)

func (c *CoreClient) SuspendUserWorkloads(ctx context.Context, userID string) (model.WorkloadSuspension, error) {
	resp, err := c.workloadAPI.SuspendUserWorkloads(ctx, &coreapi.SuspendUserWorkloadsRequest{UserId: userID})
	if err != nil {
		return model.WorkloadSuspension{}, grpcerrors.FromGRPC(err)
	}
	return model.WorkloadSuspension{
		StoppedContainers:   int(resp.GetStoppedContainers()),
		SkippedContainers:   int(resp.GetSkippedContainers()),
		CanceledBuilds:      int(resp.GetCanceledBuilds()),
		CanceledDeployments: int(resp.GetCanceledDeployments()),
		Failed:              int(resp.GetFailed()),
	}, nil
}
