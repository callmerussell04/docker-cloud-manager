package grpc

import (
	"context"

	sso "github.com/callmerussell04/docker-cloud-manager/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	ssoservice "github.com/callmerussell04/docker-cloud-manager/internal/sso/service"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

type AuthService interface {
	Register(ctx context.Context, username, email, password string) (uuid.UUID, error)
	Login(ctx context.Context, username, password string) (string, string, error)
	Refresh(ctx context.Context, refreshToken string) (string, string, error)
	AuthProviders() []string
	LocalAuthConfig() (bool, bool)
	StartOIDCLogin(ctx context.Context, provider, redirectAfter string) (string, string, error)
	CompleteOIDCCallback(ctx context.Context, provider, code, state, stateBinding string) (string, string, string, error)
	VerifyAccessToken(ctx context.Context, accessToken string) (model.User, error)
	CheckPermission(ctx context.Context, accessToken, permission string) (model.User, bool, error)
	GetUser(ctx context.Context, userID uuid.UUID) (model.User, error)
	GetUsers(ctx context.Context, ids []uuid.UUID) ([]model.User, error)
	ListUsers(ctx context.Context, limit, offset int) ([]model.User, int, error)
	CreateUser(ctx context.Context, input ssoservice.CreateUserInput) (model.User, error)
	UpdateUser(ctx context.Context, userID uuid.UUID, input ssoservice.UpdateUserInput) (model.User, error)
	DeactivateUser(ctx context.Context, userID uuid.UUID) (model.User, error)
	ReactivateUser(ctx context.Context, userID uuid.UUID) (model.User, error)
}

type Handler struct {
	sso.UnimplementedAuthServer
	sso.UnimplementedUserAPIServer
	auth AuthService
}

func Register(gRPCServer *grpc.Server, auth AuthService) {
	sso.RegisterAuthServer(gRPCServer, &Handler{auth: auth})
	sso.RegisterUserAPIServer(gRPCServer, &Handler{auth: auth})
}

func userToProto(user model.User) *sso.UserData {
	return &sso.UserData{
		UserId:      user.ID.String(),
		Username:    user.Username,
		Email:       user.Email,
		Role:        user.Role,
		QuotaRamMb:  user.QuotaRAMMB,
		QuotaDiskMb: user.QuotaDiskMB,
		QuotaCpu:    user.QuotaCPU,
		Status:      user.Status,
	}
}
