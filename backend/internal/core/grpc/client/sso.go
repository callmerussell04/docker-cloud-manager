package client

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/backend/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

type SSOClient struct {
	userAPI sso.UserAPIClient
}

func NewSSOClient(cc *grpc.ClientConn) *SSOClient {
	return &SSOClient{userAPI: sso.NewUserAPIClient(cc)}
}

func (c *SSOClient) GetUser(ctx context.Context, userID uuid.UUID) (model.UserInfo, error) {
	resp, err := c.userAPI.GetUser(ctx, &sso.GetUserRequest{UserId: userID.String()})
	if err != nil {
		return model.UserInfo{}, grpcerrors.FromGRPC(err)
	}
	return userFromProto(resp)
}

func (c *SSOClient) GetUsers(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]model.UserInfo, error) {
	rawIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		rawIDs = append(rawIDs, id.String())
	}

	resp, err := c.userAPI.BatchGetUsers(ctx, &sso.BatchGetUsersRequest{UserIds: rawIDs})
	if err != nil {
		return nil, grpcerrors.FromGRPC(err)
	}

	users := make(map[uuid.UUID]model.UserInfo, len(resp.GetUsers()))
	for _, pbUser := range resp.GetUsers() {
		user, err := userFromProto(pbUser)
		if err != nil {
			return nil, err
		}
		users[user.ID] = user
	}
	return users, nil
}

func userFromProto(pbUser *sso.UserData) (model.UserInfo, error) {
	userID, err := uuid.Parse(pbUser.GetUserId())
	if err != nil {
		return model.UserInfo{}, apperrors.ErrInternal
	}
	return model.UserInfo{
		ID:          userID,
		Status:      pbUser.GetStatus(),
		Username:    pbUser.GetUsername(),
		Role:        pbUser.GetRole(),
		QuotaRAMMB:  pbUser.GetQuotaRamMb(),
		QuotaDiskMB: pbUser.GetQuotaDiskMb(),
		QuotaCPU:    pbUser.GetQuotaCpu(),
	}, nil
}
