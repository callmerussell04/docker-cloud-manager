package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/permissions"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	PermissionSystemConfigRead     = permissions.SystemConfigRead
	PermissionSystemConfigUpdate   = permissions.SystemConfigUpdate
	PermissionSystemMonitoringRead = permissions.SystemMonitoringRead
	PermissionReportsAdminRead     = permissions.ReportsAdminRead

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
		PermissionSystemMonitoringRead:    {},
		PermissionReportsAdminRead:        {},
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
	GetUserByEmail(ctx context.Context, email string) (model.User, error)
	GetUserByExternalIdentity(ctx context.Context, provider, subject string) (model.User, error)
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

type OIDCProvider interface {
	AuthCodeURL(state, nonce string) string
	ExchangeCode(ctx context.Context, code, nonce string) (OIDCIdentity, error)
}

type OIDCIdentity struct {
	Subject           string
	Username          string
	Email             string
	EmailVerified     bool
	Groups            []string
	RealmRoles        []string
	PreferredUsername string
}

type AuthService struct {
	repo          UserRepository
	tokenProvider TokenProvider
	cfg           Config
	oidcProvider  OIDCProvider
	states        map[string]oidcState
	stateMu       sync.Mutex
	now           func() time.Time
}

type Config struct {
	LocalLoginEnabled    bool
	LocalRegisterEnabled bool
	OIDCEnabled          bool
	OIDCProviderName     string
	OIDCAdminGroups      []string
	OIDCDefaultRole      string
	OIDCStateTTL         time.Duration
}

type oidcState struct {
	Provider       string
	Nonce          string
	BrowserBinding string
	RedirectAfter  string
	ExpiresAt      time.Time
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
	return NewAuthServiceWithConfig(repo, tokenProvider, nil, Config{
		LocalLoginEnabled:    true,
		LocalRegisterEnabled: true,
		OIDCDefaultRole:      model.RoleUser,
		OIDCProviderName:     "keycloak",
		OIDCStateTTL:         10 * time.Minute,
	})
}

func NewAuthServiceWithConfig(repo UserRepository, tokenProvider TokenProvider, oidcProvider OIDCProvider, cfg Config) *AuthService {
	if cfg.OIDCProviderName == "" {
		cfg.OIDCProviderName = "keycloak"
	}
	if cfg.OIDCDefaultRole == "" {
		cfg.OIDCDefaultRole = model.RoleUser
	}
	if cfg.OIDCStateTTL <= 0 {
		cfg.OIDCStateTTL = 10 * time.Minute
	}
	return &AuthService{
		repo:          repo,
		tokenProvider: tokenProvider,
		cfg:           cfg,
		oidcProvider:  oidcProvider,
		states:        map[string]oidcState{},
		now:           time.Now,
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
		AuthSource:   model.AuthSourceLocal,
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
	if !s.cfg.LocalRegisterEnabled {
		return uuid.Nil, apperrors.ErrNotFound
	}

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
		AuthSource:   model.AuthSourceLocal,
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
	if !s.cfg.LocalLoginEnabled {
		return "", "", apperrors.ErrNotFound
	}

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
	if user.AuthSource != "" && user.AuthSource != model.AuthSourceLocal {
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

func (s *AuthService) AuthProviders() []string {
	if !s.cfg.OIDCEnabled || s.oidcProvider == nil {
		return []string{}
	}
	return []string{s.cfg.OIDCProviderName}
}

func (s *AuthService) LocalAuthConfig() (loginEnabled bool, registerEnabled bool) {
	return s.cfg.LocalLoginEnabled, s.cfg.LocalRegisterEnabled
}

func (s *AuthService) StartOIDCLogin(ctx context.Context, provider, redirectAfter string) (string, string, error) {
	_ = ctx
	if !s.oidcAvailable(provider) {
		return "", "", apperrors.ErrNotFound
	}

	state, err := randomToken()
	if err != nil {
		return "", "", apperrors.ErrInternal
	}
	nonce, err := randomToken()
	if err != nil {
		return "", "", apperrors.ErrInternal
	}
	browserBinding, err := randomToken()
	if err != nil {
		return "", "", apperrors.ErrInternal
	}
	if strings.TrimSpace(redirectAfter) == "" {
		redirectAfter = "/"
	}

	s.stateMu.Lock()
	s.cleanupExpiredStatesLocked()
	s.states[state] = oidcState{
		Provider:       provider,
		Nonce:          nonce,
		BrowserBinding: browserBinding,
		RedirectAfter:  strings.TrimSpace(redirectAfter),
		ExpiresAt:      s.now().Add(s.cfg.OIDCStateTTL),
	}
	s.stateMu.Unlock()

	return s.oidcProvider.AuthCodeURL(state, nonce), browserBinding, nil
}

func (s *AuthService) CompleteOIDCCallback(ctx context.Context, provider, code, state, stateBinding string) (string, string, string, error) {
	if !s.oidcAvailable(provider) {
		return "", "", "", apperrors.ErrNotFound
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(state) == "" || strings.TrimSpace(stateBinding) == "" {
		return "", "", "", apperrors.ErrBadRequest
	}

	oidcState, err := s.consumeOIDCState(provider, state, stateBinding)
	if err != nil {
		return "", "", "", err
	}

	identity, err := s.oidcProvider.ExchangeCode(ctx, code, oidcState.Nonce)
	if err != nil {
		return "", "", "", err
	}

	user, err := s.provisionOIDCUser(ctx, provider, identity)
	if err != nil {
		return "", "", "", err
	}
	if user.Status != model.StatusActive {
		return "", "", "", apperrors.ErrInvalidCredentials
	}

	accessToken, refreshToken, err := s.tokenProvider.GenerateTokens(user)
	if err != nil {
		return "", "", "", apperrors.ErrInternal
	}

	return accessToken, refreshToken, oidcState.RedirectAfter, nil
}

func (s *AuthService) oidcAvailable(provider string) bool {
	return s.cfg.OIDCEnabled && s.oidcProvider != nil && provider == s.cfg.OIDCProviderName
}

func (s *AuthService) consumeOIDCState(provider, state, stateBinding string) (oidcState, error) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.cleanupExpiredStatesLocked()

	stored, ok := s.states[state]
	if !ok || stored.Provider != provider || !secureStringEqual(stored.BrowserBinding, stateBinding) {
		return oidcState{}, apperrors.ErrInvalidCredentials
	}
	delete(s.states, state)
	return stored, nil
}

func (s *AuthService) cleanupExpiredStatesLocked() {
	now := s.now()
	for state, stored := range s.states {
		if now.After(stored.ExpiresAt) {
			delete(s.states, state)
		}
	}
}

func (s *AuthService) provisionOIDCUser(ctx context.Context, provider string, identity OIDCIdentity) (model.User, error) {
	if strings.TrimSpace(identity.Subject) == "" {
		return model.User{}, apperrors.ErrInvalidCredentials
	}

	now := s.now()
	role := s.roleForOIDCIdentity(identity)
	baseUsername := normalizeExternalUsername(identity.Username, identity.PreferredUsername, identity.Subject)

	existing, err := s.repo.GetUserByExternalIdentity(ctx, provider, identity.Subject)
	if err == nil {
		username, err := s.resolveOIDCUsername(ctx, existing.ID, existing.Username, baseUsername, identity.Subject)
		if err != nil {
			return model.User{}, err
		}
		email, err := s.resolveOIDCEmail(ctx, existing.ID, provider, identity.Subject, identity.Email)
		if err != nil {
			return model.User{}, err
		}
		if err := s.ensureNotLastActiveAdmin(ctx, existing, role, existing.Status); err != nil {
			return model.User{}, err
		}
		existing.Username = username
		existing.Email = email
		existing.Role = role
		existing.AuthSource = model.AuthSourceOIDC
		existing.ExternalProvider = provider
		existing.ExternalSubject = identity.Subject
		existing.ExternalUsername = firstNonEmpty(identity.Username, identity.PreferredUsername)
		existing.LastLoginAt = &now
		if err := s.repo.UpdateUser(ctx, existing); err != nil {
			if errors.Is(err, apperrors.ErrAlreadyExists) {
				return model.User{}, apperrors.New(apperrors.ErrConflict, "oidc user email or username already exists")
			}
			return model.User{}, fmt.Errorf("failed to update oidc user: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return model.User{}, fmt.Errorf("failed to get oidc user: %w", err)
	}

	userID := uuid.New()
	username, err := s.resolveOIDCUsername(ctx, userID, "", baseUsername, identity.Subject)
	if err != nil {
		return model.User{}, err
	}
	email, err := s.resolveOIDCEmail(ctx, userID, provider, identity.Subject, identity.Email)
	if err != nil {
		return model.User{}, err
	}

	user := model.User{
		ID:               userID,
		Username:         username,
		Email:            email,
		Role:             role,
		Status:           model.StatusActive,
		QuotaCPU:         defaultQuotaCPU,
		QuotaRAMMB:       defaultQuotaRAMMB,
		QuotaDiskMB:      defaultQuotaDiskMB,
		AuthSource:       model.AuthSourceOIDC,
		ExternalProvider: provider,
		ExternalSubject:  identity.Subject,
		ExternalUsername: firstNonEmpty(identity.Username, identity.PreferredUsername),
		LastLoginAt:      &now,
	}
	if err := s.repo.SaveUser(ctx, user); err != nil {
		if errors.Is(err, apperrors.ErrAlreadyExists) {
			return model.User{}, apperrors.New(apperrors.ErrConflict, "oidc user email or username already exists")
		}
		return model.User{}, fmt.Errorf("failed to save oidc user: %w", err)
	}
	return user, nil
}

func (s *AuthService) resolveOIDCUsername(ctx context.Context, currentUserID uuid.UUID, currentUsername, base, subject string) (string, error) {
	user, err := s.repo.GetUserByUsername(ctx, base)
	if errors.Is(err, apperrors.ErrNotFound) {
		return base, nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to check oidc username: %w", err)
	}
	if user.ID == currentUserID {
		return base, nil
	}

	suffix := shortSubjectSuffix(subject)
	candidate := trimUsername(base, 30-len(suffix)-1) + "-" + suffix
	user, err = s.repo.GetUserByUsername(ctx, candidate)
	if errors.Is(err, apperrors.ErrNotFound) {
		return candidate, nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to check oidc username: %w", err)
	}
	if user.ID == currentUserID {
		return candidate, nil
	}
	if currentUsername != "" {
		return currentUsername, nil
	}
	return "", apperrors.New(apperrors.ErrConflict, "oidc username already exists")
}

func (s *AuthService) resolveOIDCEmail(ctx context.Context, currentUserID uuid.UUID, provider, subject, upstreamEmail string) (string, error) {
	if email := normalizeExternalEmail(upstreamEmail); email != "" {
		user, err := s.repo.GetUserByEmail(ctx, email)
		if errors.Is(err, apperrors.ErrNotFound) {
			return email, nil
		}
		if err != nil {
			return "", fmt.Errorf("failed to check oidc email: %w", err)
		}
		if user.ID == currentUserID {
			return email, nil
		}
	}

	email := oidcFallbackEmail(provider, subject)
	user, err := s.repo.GetUserByEmail(ctx, email)
	if errors.Is(err, apperrors.ErrNotFound) {
		return email, nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to check oidc fallback email: %w", err)
	}
	if user.ID == currentUserID {
		return email, nil
	}
	return "", apperrors.New(apperrors.ErrConflict, "oidc fallback email already exists")
}

func (s *AuthService) roleForOIDCIdentity(identity OIDCIdentity) string {
	adminGroups := map[string]struct{}{}
	for _, group := range s.cfg.OIDCAdminGroups {
		group = strings.TrimSpace(group)
		if group != "" {
			adminGroups[group] = struct{}{}
		}
	}
	for _, value := range append(identity.Groups, identity.RealmRoles...) {
		if _, ok := adminGroups[value]; ok {
			return model.RoleAdmin
		}
	}
	if validRole(s.cfg.OIDCDefaultRole) {
		return s.cfg.OIDCDefaultRole
	}
	return model.RoleUser
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
		AuthSource:   model.AuthSourceLocal,
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

func randomToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func secureStringEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aHash := sha256.Sum256([]byte(a))
	bHash := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(aHash[:], bHash[:]) == 1
}

func normalizeExternalUsername(username, preferredUsername, subject string) string {
	base := firstNonEmpty(username, preferredUsername, subject)
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune('-')
		}
	}
	value := strings.Trim(b.String(), "-")
	if len(value) < 3 {
		value = "oidc-" + shortSubjectSuffix(subject)
	}
	return trimUsername(value, 30)
}

func trimUsername(value string, limit int) string {
	if limit < 3 {
		limit = 3
	}
	if len(value) <= limit {
		return value
	}
	value = strings.Trim(value[:limit], "-")
	if len(value) < 3 {
		return value + strings.Repeat("0", 3-len(value))
	}
	return value
}

func normalizeExternalEmail(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return ""
	}
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" {
		return ""
	}
	return email
}

func oidcFallbackEmail(provider, subject string) string {
	sum := sha256.Sum256([]byte(provider + ":" + subject))
	return "oidc-" + hex.EncodeToString(sum[:])[:24] + "@oidc.local"
}

func shortSubjectSuffix(subject string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(subject) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
		if b.Len() >= 8 {
			break
		}
	}
	if b.Len() == 0 {
		return "external"
	}
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
