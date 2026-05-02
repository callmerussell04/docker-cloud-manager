package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/permissions"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	PermissionSystemConfigRead   = permissions.SystemConfigRead
	PermissionSystemConfigUpdate = permissions.SystemConfigUpdate

	PermissionContainersAdminList     = permissions.ContainersAdminList
	PermissionContainersAdminAction   = permissions.ContainersAdminAction
	PermissionContainersAdminStats    = permissions.ContainersAdminStats
	PermissionContainersAdminLogs     = permissions.ContainersAdminLogs
	PermissionContainersAdminTerminal = permissions.ContainersAdminTerminal

	PermissionVolumesAdminList   = permissions.VolumesAdminList
	PermissionVolumesAdminDelete = permissions.VolumesAdminDelete

	PermissionImagesAdminList   = permissions.ImagesAdminList
	PermissionImagesAdminDelete = permissions.ImagesAdminDelete

	PermissionBuildsAdminList   = permissions.BuildsAdminList
	PermissionBuildsAdminDelete = permissions.BuildsAdminDelete

	PermissionProjectsAdminList   = permissions.ProjectsAdminList
	PermissionProjectsAdminDelete = permissions.ProjectsAdminDelete
	PermissionProjectsAdminStart  = permissions.ProjectsAdminStart
	PermissionProjectsAdminStop   = permissions.ProjectsAdminStop

	PermissionUsersAdminList   = permissions.UsersAdminList
	PermissionUsersAdminRead   = permissions.UsersAdminRead
	PermissionUsersAdminCreate = permissions.UsersAdminCreate
	PermissionUsersAdminUpdate = permissions.UsersAdminUpdate
	PermissionUsersAdminDelete = permissions.UsersAdminDelete
)

const (
	defaultQuotaCPU    = 1.0
	defaultQuotaRAMMB  = 2048
	defaultQuotaDiskMB = 5120
)

var rolePermissions = map[string]map[string]struct{}{
	model.RoleAdmin: {
		PermissionSystemConfigRead:        {},
		PermissionSystemConfigUpdate:      {},
		PermissionContainersAdminList:     {},
		PermissionContainersAdminAction:   {},
		PermissionContainersAdminStats:    {},
		PermissionContainersAdminLogs:     {},
		PermissionContainersAdminTerminal: {},
		PermissionVolumesAdminList:        {},
		PermissionVolumesAdminDelete:      {},
		PermissionImagesAdminList:         {},
		PermissionImagesAdminDelete:       {},
		PermissionBuildsAdminList:         {},
		PermissionBuildsAdminDelete:       {},
		PermissionProjectsAdminList:       {},
		PermissionProjectsAdminDelete:     {},
		PermissionProjectsAdminStart:      {},
		PermissionProjectsAdminStop:       {},
		PermissionUsersAdminList:          {},
		PermissionUsersAdminRead:          {},
		PermissionUsersAdminCreate:        {},
		PermissionUsersAdminUpdate:        {},
		PermissionUsersAdminDelete:        {},
	},
	model.RoleUser: {},
}

type UserRepository interface {
	SaveUser(ctx context.Context, user model.User) error
	UpdateUser(ctx context.Context, user model.User) error
	GetUserByUsername(ctx context.Context, username string) (model.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (model.User, error)
	GetUsersByIDs(ctx context.Context, ids []uuid.UUID) ([]model.User, error)
	ListUsers(ctx context.Context, opts model.ListUsersOptions) ([]model.User, int, error)
	CountActiveAdmins(ctx context.Context) (int, error)
}

type TokenProvider interface {
	GenerateTokens(user model.User) (string, string, error)
	ValidateAccessToken(token string) (uuid.UUID, error)
	ValidateRefreshToken(token string) (uuid.UUID, error)
}

type AuthService struct {
	repo          UserRepository
	tokenProvider TokenProvider
}

type BootstrapAdminConfig struct {
	Username string
	Email    string
	Password string
}

type CreateUserInput struct {
	Username    string
	Email       string
	Password    string
	Role        string
	Status      string
	QuotaCPU    float64
	QuotaRAMMB  int64
	QuotaDiskMB int64
}

type UpdateUserInput struct {
	Username    string
	Email       string
	Password    string
	Role        string
	Status      string
	QuotaCPU    float64
	QuotaRAMMB  int64
	QuotaDiskMB int64
}

func (s *AuthService) VerifyAccessToken(ctx context.Context, accessToken string) (model.User, error) {
	userID, err := s.tokenProvider.ValidateAccessToken(accessToken)
	if err != nil {
		return model.User{}, apperrors.ErrInvalidToken
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return model.User{}, apperrors.ErrInvalidToken
	}
	if user.Status != model.StatusActive {
		return model.User{}, apperrors.ErrInvalidToken
	}
	return user, nil
}

func (s *AuthService) CheckPermission(ctx context.Context, accessToken, permission string) (model.User, bool, error) {
	if !permissionExists(permission) {
		return model.User{}, false, apperrors.ErrBadRequest
	}

	user, err := s.VerifyAccessToken(ctx, accessToken)
	if err != nil {
		return model.User{}, false, err
	}

	permissions, ok := rolePermissions[user.Role]
	if !ok {
		return user, false, nil
	}
	_, allowed := permissions[permission]
	return user, allowed, nil
}

func permissionExists(permission string) bool {
	return permissions.Exists(permission)
}

func (s *AuthService) GetUser(ctx context.Context, userID uuid.UUID) (model.User, error) {
	return s.repo.GetUserByID(ctx, userID)
}

func (s *AuthService) GetUsers(ctx context.Context, ids []uuid.UUID) ([]model.User, error) {
	return s.repo.GetUsersByIDs(ctx, ids)
}

func (s *AuthService) ListUsers(ctx context.Context, limit, offset int) ([]model.User, int, error) {
	return s.repo.ListUsers(ctx, model.ListUsersOptions{Limit: limit, Offset: offset})
}

func NewAuthService(repo UserRepository, tokenProvider TokenProvider) *AuthService {
	return &AuthService{
		repo:          repo,
		tokenProvider: tokenProvider,
	}
}

func (s *AuthService) EnsureBootstrapAdmin(ctx context.Context, cfg BootstrapAdminConfig) error {
	existing, err := s.repo.GetUserByUsername(ctx, cfg.Username)
	if err == nil {
		if existing.Role == model.RoleAdmin {
			if existing.Status != model.StatusActive {
				existing.Status = model.StatusActive
				if err := s.repo.UpdateUser(ctx, existing); err != nil {
					return fmt.Errorf("failed to activate bootstrap admin: %w", err)
				}
			}
			return nil
		}
		return apperrors.New(apperrors.ErrConflict, "bootstrap admin username already exists with non-admin role")
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return fmt.Errorf("failed to check bootstrap admin: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.Password), bcrypt.DefaultCost)
	if err != nil {
		return apperrors.ErrInternal
	}

	user := model.User{
		ID:           uuid.New(),
		Username:     cfg.Username,
		Email:        cfg.Email,
		PasswordHash: string(hash),
		Role:         model.RoleAdmin,
		Status:       model.StatusActive,
		QuotaCPU:     defaultQuotaCPU,
		QuotaRAMMB:   defaultQuotaRAMMB,
		QuotaDiskMB:  defaultQuotaDiskMB,
	}

	if err := s.repo.SaveUser(ctx, user); err != nil {
		if errors.Is(err, apperrors.ErrAlreadyExists) {
			return apperrors.New(apperrors.ErrConflict, "bootstrap admin email or username already exists")
		}
		return fmt.Errorf("failed to save bootstrap admin: %w", err)
	}

	return nil
}

func (s *AuthService) Register(ctx context.Context, username, email, password string) (uuid.UUID, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return uuid.Nil, apperrors.ErrInternal
	}

	user := model.User{
		ID:           uuid.New(),
		Username:     username,
		Email:        email,
		PasswordHash: string(hash),
		Role:         model.RoleUser,
		Status:       model.StatusActive,
		QuotaCPU:     defaultQuotaCPU,
		QuotaRAMMB:   defaultQuotaRAMMB,
		QuotaDiskMB:  defaultQuotaDiskMB,
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
	if user.Status != model.StatusActive {
		return "", "", apperrors.ErrInvalidCredentials
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
	if user.Status != model.StatusActive {
		return "", "", apperrors.ErrInvalidToken
	}

	accessToken, newRefreshToken, err := s.tokenProvider.GenerateTokens(user)
	if err != nil {
		return "", "", apperrors.ErrInternal
	}

	return accessToken, newRefreshToken, nil
}

func (s *AuthService) CreateUser(ctx context.Context, input CreateUserInput) (model.User, error) {
	if err := validateUserInput(input.Role, input.Status, input.QuotaCPU, input.QuotaRAMMB, input.QuotaDiskMB); err != nil {
		return model.User{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return model.User{}, apperrors.ErrInternal
	}

	user := model.User{
		ID:           uuid.New(),
		Username:     input.Username,
		Email:        input.Email,
		PasswordHash: string(hash),
		Role:         input.Role,
		Status:       input.Status,
		QuotaCPU:     input.QuotaCPU,
		QuotaRAMMB:   input.QuotaRAMMB,
		QuotaDiskMB:  input.QuotaDiskMB,
	}
	if err := s.repo.SaveUser(ctx, user); err != nil {
		if errors.Is(err, apperrors.ErrAlreadyExists) {
			return model.User{}, apperrors.ErrAlreadyExists
		}
		return model.User{}, fmt.Errorf("failed to create user: %w", err)
	}
	return user, nil
}

func (s *AuthService) UpdateUser(ctx context.Context, userID uuid.UUID, input UpdateUserInput) (model.User, error) {
	if err := validateUserInput(input.Role, input.Status, input.QuotaCPU, input.QuotaRAMMB, input.QuotaDiskMB); err != nil {
		return model.User{}, err
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return model.User{}, err
	}
	if err := s.ensureNotLastActiveAdmin(ctx, user, input.Role, input.Status); err != nil {
		return model.User{}, err
	}

	user.Username = input.Username
	user.Email = input.Email
	user.Role = input.Role
	user.Status = input.Status
	user.QuotaCPU = input.QuotaCPU
	user.QuotaRAMMB = input.QuotaRAMMB
	user.QuotaDiskMB = input.QuotaDiskMB
	if input.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			return model.User{}, apperrors.ErrInternal
		}
		user.PasswordHash = string(hash)
	}

	if err := s.repo.UpdateUser(ctx, user); err != nil {
		if errors.Is(err, apperrors.ErrAlreadyExists) {
			return model.User{}, apperrors.ErrAlreadyExists
		}
		return model.User{}, fmt.Errorf("failed to update user: %w", err)
	}
	return user, nil
}

func (s *AuthService) DeactivateUser(ctx context.Context, userID uuid.UUID) (model.User, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return model.User{}, err
	}
	if err := s.ensureNotLastActiveAdmin(ctx, user, user.Role, model.StatusDeactivated); err != nil {
		return model.User{}, err
	}
	user.Status = model.StatusDeactivated
	if err := s.repo.UpdateUser(ctx, user); err != nil {
		return model.User{}, fmt.Errorf("failed to deactivate user: %w", err)
	}
	return user, nil
}

func (s *AuthService) ReactivateUser(ctx context.Context, userID uuid.UUID) (model.User, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return model.User{}, err
	}
	user.Status = model.StatusActive
	if err := s.repo.UpdateUser(ctx, user); err != nil {
		return model.User{}, fmt.Errorf("failed to reactivate user: %w", err)
	}
	return user, nil
}

func validateUserInput(role, status string, quotaCPU float64, quotaRAMMB, quotaDiskMB int64) error {
	if !validRole(role) {
		return apperrors.New(apperrors.ErrBadRequest, "role must be one of: admin, user")
	}
	if !validStatus(status) {
		return apperrors.New(apperrors.ErrBadRequest, "status must be one of: active, deactivated")
	}
	if quotaCPU <= 0 || quotaRAMMB <= 0 || quotaDiskMB <= 0 {
		return apperrors.New(apperrors.ErrBadRequest, "quotas must be positive")
	}
	return nil
}

func validRole(role string) bool {
	return role == model.RoleAdmin || role == model.RoleUser
}

func validStatus(status string) bool {
	return status == model.StatusActive || status == model.StatusDeactivated
}

func (s *AuthService) ensureNotLastActiveAdmin(ctx context.Context, current model.User, nextRole, nextStatus string) error {
	if current.Role != model.RoleAdmin || current.Status != model.StatusActive {
		return nil
	}
	if nextRole == model.RoleAdmin && nextStatus == model.StatusActive {
		return nil
	}
	count, err := s.repo.CountActiveAdmins(ctx)
	if err != nil {
		return fmt.Errorf("failed to count active admins: %w", err)
	}
	if count <= 1 {
		return apperrors.New(apperrors.ErrConflict, "cannot deactivate or demote the last active admin")
	}
	return nil
}
