package service

import (
	"context"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
)

// AdminUserDirectory reads users from SSO.
type AdminUserDirectory interface {
	ListUsers(ctx context.Context, page, limit int) (model.PaginatedAdminUsers, error)
	GetUser(ctx context.Context, userID string) (model.AdminUser, error)
}

// UserQuotaAPI is Core's quota administration.
type UserQuotaAPI interface {
	GetUserQuotas(ctx context.Context, userIDs []string) ([]model.UserQuota, error)
	SetUserQuota(ctx context.Context, userID string, limits model.UserQuotaLimits) (model.UserQuota, error)
	ResetUserQuota(ctx context.Context, userID string) (model.UserQuota, error)
}

// errQuotasUnavailable replaces every failure to read quotas from Core, so a
// read never shows a guessed quota and never reports another error kind.
var errQuotasUnavailable = apperrors.New(apperrors.ErrUnavailable, "user quotas are unavailable")

// AdminUsers combines SSO's users with Core's quotas for the admin user
// pages. SSO holds identity only; Core holds quotas and does not know which
// users exist, so a quota write is made only for a user SSO confirms.
type AdminUsers struct {
	users  AdminUserDirectory
	quotas UserQuotaAPI
}

func NewAdminUsers(users AdminUserDirectory, quotas UserQuotaAPI) *AdminUsers {
	return &AdminUsers{users: users, quotas: quotas}
}

// ListUsers reads a page of users and their quotas in one Core call.
func (s *AdminUsers) ListUsers(ctx context.Context, page, limit int) (model.PaginatedAdminUsersWithQuota, error) {
	users, err := s.users.ListUsers(ctx, page, limit)
	if err != nil {
		return model.PaginatedAdminUsersWithQuota{}, err
	}
	withQuotas, err := s.withQuotas(ctx, users.Users)
	if err != nil {
		return model.PaginatedAdminUsersWithQuota{}, err
	}
	return model.PaginatedAdminUsersWithQuota{Users: withQuotas, TotalCount: users.TotalCount}, nil
}

func (s *AdminUsers) GetUser(ctx context.Context, userID string) (model.AdminUserWithQuota, error) {
	user, err := s.users.GetUser(ctx, userID)
	if err != nil {
		return model.AdminUserWithQuota{}, err
	}
	withQuotas, err := s.withQuotas(ctx, []model.AdminUser{user})
	if err != nil {
		return model.AdminUserWithQuota{}, err
	}
	return withQuotas[0], nil
}

// SetUserQuota gives an existing user a custom quota.
func (s *AdminUsers) SetUserQuota(ctx context.Context, userID string, limits model.UserQuotaLimits) (model.UserQuota, error) {
	if _, err := s.users.GetUser(ctx, userID); err != nil {
		return model.UserQuota{}, err
	}
	return s.quotas.SetUserQuota(ctx, userID, limits)
}

// ResetUserQuota returns an existing user to the default quota.
func (s *AdminUsers) ResetUserQuota(ctx context.Context, userID string) (model.UserQuota, error) {
	if _, err := s.users.GetUser(ctx, userID); err != nil {
		return model.UserQuota{}, err
	}
	return s.quotas.ResetUserQuota(ctx, userID)
}

func (s *AdminUsers) withQuotas(ctx context.Context, users []model.AdminUser) ([]model.AdminUserWithQuota, error) {
	result := make([]model.AdminUserWithQuota, 0, len(users))
	if len(users) == 0 {
		return result, nil
	}
	userIDs := make([]string, 0, len(users))
	for _, user := range users {
		userIDs = append(userIDs, user.UserID)
	}
	quotas, err := s.quotas.GetUserQuotas(ctx, userIDs)
	if err != nil {
		// %v keeps the cause for the request log without letting its kind
		// (forbidden, timeout, ...) decide the response.
		return nil, fmt.Errorf("%w: read user quotas: %v", errQuotasUnavailable, err)
	}
	byUser := make(map[string]model.UserQuota, len(quotas))
	for _, quota := range quotas {
		byUser[quota.UserID] = quota
	}
	for _, user := range users {
		quota, found := byUser[user.UserID]
		if !found {
			return nil, errQuotasUnavailable
		}
		result = append(result, model.AdminUserWithQuota{User: user, Quota: quota})
	}
	return result, nil
}
