package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type UserProvider interface {
	ListUsers(ctx context.Context, page, limit int) (model.PaginatedAdminUsers, error)
	GetUser(ctx context.Context, userID string) (model.AdminUser, error)
	CreateUser(ctx context.Context, input model.CreateUserInput) (model.AdminUser, error)
	UpdateUser(ctx context.Context, userID string, input model.UpdateUserInput) (model.AdminUser, error)
	DeactivateUser(ctx context.Context, userID string) (model.AdminUser, error)
	ReactivateUser(ctx context.Context, userID string) (model.AdminUser, error)
}

type UserManagementService struct {
	provider UserProvider
}

func NewUserManagement(provider UserProvider) *UserManagementService {
	return &UserManagementService{provider: provider}
}

func (s *UserManagementService) ListUsers(ctx context.Context, page, limit int) (model.PaginatedAdminUsers, error) {
	return s.provider.ListUsers(ctx, page, limit)
}

func (s *UserManagementService) GetUser(ctx context.Context, userID string) (model.AdminUser, error) {
	return s.provider.GetUser(ctx, userID)
}

func (s *UserManagementService) CreateUser(ctx context.Context, input model.CreateUserInput) (model.AdminUser, error) {
	return s.provider.CreateUser(ctx, input)
}

func (s *UserManagementService) UpdateUser(ctx context.Context, userID string, input model.UpdateUserInput) (model.AdminUser, error) {
	return s.provider.UpdateUser(ctx, userID, input)
}

func (s *UserManagementService) DeactivateUser(ctx context.Context, userID string) (model.AdminUser, error) {
	return s.provider.DeactivateUser(ctx, userID)
}

func (s *UserManagementService) ReactivateUser(ctx context.Context, userID string) (model.AdminUser, error) {
	return s.provider.ReactivateUser(ctx, userID)
}
