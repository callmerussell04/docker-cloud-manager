package grpcclient

import (
	"context"

	ssoapi "github.com/callmerussell04/docker-cloud-manager/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SSOClient struct {
	userAPI ssoapi.UserAPIClient
}

func NewSSOClient(cc *grpc.ClientConn) *SSOClient {
	return &SSOClient{userAPI: ssoapi.NewUserAPIClient(cc)}
}

func (c *SSOClient) GetUser(ctx context.Context, userID uuid.UUID) (model.UserInfo, error) {
	resp, err := c.userAPI.GetUser(ctx, &ssoapi.GetUserRequest{UserId: userID.String()})
	if err != nil {
		return model.UserInfo{}, mapSSOError(err)
	}
	return userFromProto(resp)
}

func (c *SSOClient) GetUsers(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]model.UserInfo, error) {
	rawIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		rawIDs = append(rawIDs, id.String())
	}

	resp, err := c.userAPI.BatchGetUsers(ctx, &ssoapi.BatchGetUsersRequest{UserIds: rawIDs})
	if err != nil {
		return nil, mapSSOError(err)
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

func userFromProto(pbUser *ssoapi.UserData) (model.UserInfo, error) {
	userID, err := uuid.Parse(pbUser.GetUserId())
	if err != nil {
		return model.UserInfo{}, apperrors.ErrInternal
	}
	return model.UserInfo{
		ID:          userID,
		Username:    pbUser.GetUsername(),
		Role:        pbUser.GetRole(),
		QuotaRAMMB:  pbUser.GetQuotaRamMb(),
		QuotaDiskMB: pbUser.GetQuotaDiskMb(),
		QuotaCPU:    pbUser.GetQuotaCpu(),
	}, nil
}

func mapSSOError(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return apperrors.ErrInternal
	}
	switch st.Code() {
	case codes.NotFound:
		return apperrors.ErrNotFound
	case codes.InvalidArgument:
		return apperrors.ErrBadRequest
	case codes.Unauthenticated:
		return apperrors.ErrUnauthorized
	default:
		return apperrors.ErrInternal
	}
}
