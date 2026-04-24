package service

import (
	"context"
	"errors"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	PermissionSystemConfigRead   = "system.config.read"
	PermissionSystemConfigUpdate = "system.config.update"

	PermissionContainersAdminList   = "containers.admin.list"
	PermissionContainersAdminAction = "containers.admin.action"
	PermissionContainersAdminStats  = "containers.admin.stats"

	PermissionVolumesAdminList   = "volumes.admin.list"
	PermissionVolumesAdminDelete = "volumes.admin.delete"

	PermissionImagesAdminList   = "images.admin.list"
	PermissionImagesAdminDelete = "images.admin.delete"

	PermissionBuildsAdminList   = "builds.admin.list"
	PermissionBuildsAdminDelete = "builds.admin.delete"

	PermissionProjectsAdminList   = "projects.admin.list"
	PermissionProjectsAdminDelete = "projects.admin.delete"
	PermissionProjectsAdminStop   = "projects.admin.stop"
)

var rolePermissions = map[string]map[string]struct{}{
	domain.RoleAdmin: {
		PermissionSystemConfigRead:      {},
		PermissionSystemConfigUpdate:    {},
		PermissionContainersAdminList:   {},
		PermissionContainersAdminAction: {},
		PermissionContainersAdminStats:  {},
		PermissionVolumesAdminList:      {},
		PermissionVolumesAdminDelete:    {},
		PermissionImagesAdminList:       {},
		PermissionImagesAdminDelete:     {},
		PermissionBuildsAdminList:       {},
		PermissionBuildsAdminDelete:     {},
		PermissionProjectsAdminList:     {},
		PermissionProjectsAdminDelete:   {},
		PermissionProjectsAdminStop:     {},
	},
	domain.RoleUser: {},
}

type UserRepository interface {
	SaveUser(ctx context.Context, user domain.User) error
	GetUserByUsername(ctx context.Context, username string) (domain.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	GetUsersByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.User, error)
}

type TokenProvider interface {
	GenerateTokens(user domain.User) (string, string, error)
	ValidateAccessToken(token string) (uuid.UUID, error)
	ValidateRefreshToken(token string) (uuid.UUID, error)
}

type AuthService struct {
	repo          UserRepository
	tokenProvider TokenProvider
}

func (s *AuthService) VerifyAccessToken(ctx context.Context, accessToken string) (domain.User, error) {
	userID, err := s.tokenProvider.ValidateAccessToken(accessToken)
	if err != nil {
		return domain.User{}, apperrors.ErrInvalidToken
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return domain.User{}, apperrors.ErrInvalidToken
	}
	return user, nil
}

func (s *AuthService) CheckPermission(ctx context.Context, accessToken, permission string) (domain.User, bool, error) {
	if !permissionExists(permission) {
		return domain.User{}, false, apperrors.ErrBadRequest
	}

	user, err := s.VerifyAccessToken(ctx, accessToken)
	if err != nil {
		return domain.User{}, false, err
	}

	permissions, ok := rolePermissions[user.Role]
	if !ok {
		return user, false, nil
	}
	_, allowed := permissions[permission]
	return user, allowed, nil
}

func permissionExists(permission string) bool {
	for _, permissions := range rolePermissions {
		if _, ok := permissions[permission]; ok {
			return true
		}
	}
	return false
}

func (s *AuthService) GetUser(ctx context.Context, userID uuid.UUID) (domain.User, error) {
	return s.repo.GetUserByID(ctx, userID)
}

func (s *AuthService) GetUsers(ctx context.Context, ids []uuid.UUID) ([]domain.User, error) {
	return s.repo.GetUsersByIDs(ctx, ids)
}

func NewAuthService(repo UserRepository, tokenProvider TokenProvider) *AuthService {
	return &AuthService{
		repo:          repo,
		tokenProvider: tokenProvider,
	}
}

func (s *AuthService) Register(ctx context.Context, username, email, password string) (uuid.UUID, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return uuid.Nil, apperrors.ErrInternal
	}

	user := domain.User{
		ID:           uuid.New(),
		Username:     username,
		Email:        email,
		PasswordHash: string(hash),
		Role:         domain.RoleUser,
	}

	err = s.repo.SaveUser(ctx, user)
	if err != nil {
		if errors.Is(err, apperrors.ErrAlreadyExists) {
			return uuid.Nil, apperrors.ErrAlreadyExists
		}
		return uuid.Nil, apperrors.ErrInternal
	}

	return user.ID, nil
}

func (s *AuthService) Login(ctx context.Context, username, password string) (string, string, error) {
	user, err := s.repo.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return "", "", apperrors.ErrInvalidCredentials
		}
		return "", "", apperrors.ErrInternal
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return "", "", apperrors.ErrInvalidCredentials
	}

	accessToken, refreshToken, err := s.tokenProvider.GenerateTokens(user)
	if err != nil {
		return "", "", apperrors.ErrInternal
	}

	return accessToken, refreshToken, nil
}

func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (string, string, error) {
	userID, err := s.tokenProvider.ValidateRefreshToken(refreshToken)
	if err != nil {
		return "", "", apperrors.ErrInvalidToken
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return "", "", apperrors.ErrInvalidToken
	}

	accessToken, newRefreshToken, err := s.tokenProvider.GenerateTokens(user)
	if err != nil {
		return "", "", apperrors.ErrInternal
	}

	return accessToken, newRefreshToken, nil
}
