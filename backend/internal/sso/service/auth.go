package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/permissions"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
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
}

type TokenProvider interface {
	GenerateAccessToken(user model.User, sessionID uuid.UUID, expiresAt time.Time) (string, error)
	ValidateAccessToken(token string) (model.AccessIdentity, error)
}

// OIDCProvider is the external authorization-code provider. It is unavailable
// until SSO has reached it; once available it stays available. The
// authorization request carries an S256 PKCE challenge for verifier, and the
// code exchange sends verifier.
type OIDCProvider interface {
	Available() bool
	AuthCodeURL(state, nonce, verifier string) string
	ExchangeCode(ctx context.Context, code, nonce, verifier string) (OIDCIdentity, error)
}

// OIDCIdentity holds the ID-token claims DCM reads. PreferredUsername is the
// only source of a derived username.
type OIDCIdentity struct {
	Subject           string
	Email             string
	EmailVerified     bool
	Groups            []string
	RealmRoles        []string
	PreferredUsername string
}

type AuthService struct {
	repo          UserRepository
	tokenProvider TokenProvider
	sessions      SessionRepository
	random        io.Reader
	cfg           Config
	oidcProvider  OIDCProvider
	loginAttempts LoginAttemptRepository
	now           func() time.Time
	// adminGroups are the configured administrator groups and realm roles.
	adminGroups map[string]struct{}
}

type Config struct {
	AccessTTL            time.Duration
	SessionTTL           time.Duration
	SessionIdleTTL       time.Duration
	Now                  func() time.Time
	Random               io.Reader
	LocalLoginEnabled    bool
	LocalRegisterEnabled bool
	OIDCEnabled          bool
	OIDCProviderName     string
	OIDCAdminGroups      []string
	// Logger receives warnings the caller is not told about; nil discards them.
	Logger *slog.Logger
}

type BootstrapAdminConfig struct {
	Username string
	Email    string
	Password string
}

type CreateUserInput struct {
	Username string
	Email    string
	Password string
	Role     string
	Status   string
}

type UpdateUserInput struct {
	Username string
	Email    string
	Password string
	Role     string
	Status   string
}

func (s *AuthService) VerifyAccessToken(ctx context.Context, accessToken string) (model.User, error) {
	identity, err := s.tokenProvider.ValidateAccessToken(accessToken)
	if err != nil {
		return model.User{}, apperrors.ErrInvalidToken
	}
	user, err := s.sessions.VerifySession(ctx, identity, s.now())
	if errors.Is(err, apperrors.ErrNotFound) {
		return model.User{}, apperrors.ErrInvalidToken
	}
	return user, err
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

// OIDCDeps are the optional dependencies of OIDC sign-in. Both are nil with
// OIDC off; a provider requires the login-attempt store.
type OIDCDeps struct {
	Provider      OIDCProvider
	LoginAttempts LoginAttemptRepository
}

func NewAuthServiceWithConfig(repo UserRepository, tokenProvider TokenProvider, sessions SessionRepository, oidc OIDCDeps, cfg Config) *AuthService {
	if sessions == nil {
		panic("session repository is required")
	}
	if oidc.Provider != nil && oidc.LoginAttempts == nil {
		panic("login attempt repository is required with an OIDC provider")
	}
	if cfg.AccessTTL <= 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	if cfg.SessionIdleTTL <= 0 {
		cfg.SessionIdleTTL = 7 * 24 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Random == nil {
		cfg.Random = rand.Reader
	}
	if cfg.OIDCProviderName == "" {
		cfg.OIDCProviderName = "keycloak"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	adminGroups := map[string]struct{}{}
	for _, group := range cfg.OIDCAdminGroups {
		if group = strings.TrimSpace(group); group != "" {
			adminGroups[group] = struct{}{}
		}
	}
	return &AuthService{
		adminGroups:   adminGroups,
		repo:          repo,
		tokenProvider: tokenProvider,
		cfg:           cfg,
		oidcProvider:  oidc.Provider,
		loginAttempts: oidc.LoginAttempts,
		now:           cfg.Now,
		random:        cfg.Random,
		sessions:      sessions,
	}
}

func (s *AuthService) EnsureBootstrapAdmin(ctx context.Context, cfg BootstrapAdminConfig) error {
	cfg.Username = normalizeIdentity(cfg.Username)
	cfg.Email = normalizeIdentity(cfg.Email)
	existing, err := s.repo.GetUserByUsername(ctx, cfg.Username)
	if err == nil {
		if existing.Role == model.RoleAdmin {
			if existing.Status != model.StatusActive {
				existing.Status = model.StatusActive
				if err := s.updateUser(ctx, existing, false, true); err != nil {
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
	if err := validatePasswordLength(password); err != nil {
		return uuid.Nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return uuid.Nil, apperrors.ErrInternal
	}

	user := model.User{
		ID:           uuid.New(),
		Username:     normalizeIdentity(username),
		Email:        normalizeIdentity(email),
		PasswordHash: string(hash),
		Role:         model.RoleUser,
		Status:       model.StatusActive,
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

func (s *AuthService) Login(ctx context.Context, username, password string) (model.SignIn, error) {
	if !s.cfg.LocalLoginEnabled {
		return model.SignIn{}, apperrors.ErrNotFound
	}

	user, err := s.repo.GetUserByUsername(ctx, normalizeIdentity(username))
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return model.SignIn{}, apperrors.ErrInvalidCredentials
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return model.SignIn{}, apperrors.ErrTimeout
		}
		return model.SignIn{}, apperrors.ErrUnavailable
	}
	if user.Status != model.StatusActive {
		return model.SignIn{}, apperrors.ErrInvalidCredentials
	}
	if user.AuthSource != "" && user.AuthSource != model.AuthSourceLocal {
		return model.SignIn{}, apperrors.ErrInvalidCredentials
	}

	// bcrypt compares only the first 72 bytes; no stored password is longer, so a
	// longer one would otherwise match the password it starts with.
	if len(password) > maxPasswordBytes {
		return model.SignIn{}, apperrors.ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return model.SignIn{}, apperrors.ErrInvalidCredentials
	}

	return s.createSession(ctx, user, true)
}

// AuthProviders lists the enabled external providers and whether each is available.
func (s *AuthService) AuthProviders() []model.AuthProvider {
	if !s.oidcEnabled() {
		return []model.AuthProvider{}
	}
	return []model.AuthProvider{{Name: s.cfg.OIDCProviderName, Available: s.oidcProvider.Available()}}
}

func (s *AuthService) LocalAuthConfig() (loginEnabled bool, registerEnabled bool) {
	return s.cfg.LocalLoginEnabled, s.cfg.LocalRegisterEnabled
}

// StartOIDCLogin stores a new login attempt and returns what the browser
// needs to go to the provider. Only digests of the state and of the browser
// binding are stored.
func (s *AuthService) StartOIDCLogin(ctx context.Context, provider, redirectAfter string) (model.OIDCLoginStart, error) {
	if err := s.requireOIDCProvider(provider); err != nil {
		return model.OIDCLoginStart{}, err
	}

	// Each is 32 random bytes in base64url; the verifier is a 43-character
	// RFC 7636 verifier.
	state, err := randomToken()
	if err != nil {
		return model.OIDCLoginStart{}, apperrors.ErrInternal
	}
	nonce, err := randomToken()
	if err != nil {
		return model.OIDCLoginStart{}, apperrors.ErrInternal
	}
	verifier, err := randomToken()
	if err != nil {
		return model.OIDCLoginStart{}, apperrors.ErrInternal
	}
	binding, err := randomToken()
	if err != nil {
		return model.OIDCLoginStart{}, apperrors.ErrInternal
	}
	redirectAfter = strings.TrimSpace(redirectAfter)
	if redirectAfter == "" {
		redirectAfter = "/"
	}

	now := s.now()
	attempt := model.LoginAttempt{
		StateDigest:   secretDigest(state),
		Provider:      provider,
		Nonce:         nonce,
		PKCEVerifier:  verifier,
		BindingDigest: secretDigest(binding),
		RedirectAfter: redirectAfter,
		CreatedAt:     now,
		ExpiresAt:     now.Add(OIDCLoginAttemptTTL),
	}
	if err := s.loginAttempts.SaveLoginAttempt(ctx, attempt); err != nil {
		return model.OIDCLoginStart{}, err
	}
	return model.OIDCLoginStart{
		AuthURL:      s.oidcProvider.AuthCodeURL(state, nonce, verifier),
		State:        state,
		StateBinding: binding,
		ExpiresAt:    attempt.ExpiresAt,
	}, nil
}

// CompleteOIDCCallback consumes the attempt and signs its user in. Expected
// rejections are outcomes; provider or storage unavailability and timeouts
// are errors. A rejection after the attempt was consumed carries its target.
func (s *AuthService) CompleteOIDCCallback(ctx context.Context, provider, code, state, stateBinding string) (model.OIDCCallbackResult, error) {
	if err := s.requireOIDCProvider(provider); err != nil {
		return model.OIDCCallbackResult{}, err
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(state) == "" || strings.TrimSpace(stateBinding) == "" {
		return model.OIDCCallbackResult{Outcome: model.OIDCCallbackInvalidAttempt}, nil
	}

	attempt, err := s.loginAttempts.ConsumeLoginAttempt(ctx, secretDigest(state), provider, secretDigest(stateBinding), s.now())
	if errors.Is(err, apperrors.ErrNotFound) {
		return model.OIDCCallbackResult{Outcome: model.OIDCCallbackInvalidAttempt}, nil
	}
	if err != nil {
		return model.OIDCCallbackResult{}, err
	}
	rejected := func(outcome model.OIDCCallbackOutcome) (model.OIDCCallbackResult, error) {
		return model.OIDCCallbackResult{Outcome: outcome, RedirectAfter: attempt.RedirectAfter}, nil
	}

	identity, err := s.oidcProvider.ExchangeCode(ctx, code, attempt.Nonce, attempt.PKCEVerifier)
	if errors.Is(err, apperrors.ErrInvalidCredentials) {
		return rejected(model.OIDCCallbackIdentityRejected)
	}
	if err != nil {
		return model.OIDCCallbackResult{}, err
	}

	user, err := s.provisionOIDCUser(ctx, provider, identity)
	switch {
	case errors.Is(err, apperrors.ErrInvalidCredentials):
		return rejected(model.OIDCCallbackIdentityRejected)
	case errors.Is(err, apperrors.ErrConflict):
		return rejected(model.OIDCCallbackAccountConflict)
	case err != nil:
		return model.OIDCCallbackResult{}, err
	}
	// The user has proven their identity at the provider, so unlike local
	// sign-in the answer says the account is deactivated.
	if user.Status != model.StatusActive {
		return rejected(model.OIDCCallbackAccountDeactivated)
	}

	signIn, err := s.createSession(ctx, user, false)
	if errors.Is(err, apperrors.ErrInvalidCredentials) {
		// Deactivated between provisioning and the session lock.
		return rejected(model.OIDCCallbackAccountDeactivated)
	}
	if err != nil {
		return model.OIDCCallbackResult{}, err
	}
	return model.OIDCCallbackResult{Outcome: model.OIDCCallbackSuccess, Tokens: signIn.Tokens, User: signIn.User, RedirectAfter: attempt.RedirectAfter}, nil
}

func (s *AuthService) oidcEnabled() bool {
	return s.cfg.OIDCEnabled && s.oidcProvider != nil
}

// requireOIDCProvider answers not found for a provider that is not enabled and
// unavailable for an enabled one SSO has not reached yet.
func (s *AuthService) requireOIDCProvider(provider string) error {
	if !s.oidcEnabled() || provider != s.cfg.OIDCProviderName {
		return apperrors.ErrNotFound
	}
	if !s.oidcProvider.Available() {
		return apperrors.New(apperrors.ErrUnavailable, "identity provider is not available")
	}
	return nil
}

// provisionOIDCUser returns the user bound to the identity, creating it on the
// first sign-in and otherwise synchronizing its provider-managed profile. A
// deactivated user is returned unchanged, before any write.
func (s *AuthService) provisionOIDCUser(ctx context.Context, provider string, identity OIDCIdentity) (model.User, error) {
	if strings.TrimSpace(identity.Subject) == "" {
		return model.User{}, apperrors.ErrInvalidCredentials
	}
	identityHash := oidcIdentityHash(provider, identity.Subject)

	existing, err := s.repo.GetUserByExternalIdentity(ctx, provider, identity.Subject)
	if errors.Is(err, apperrors.ErrNotFound) {
		var created model.User
		created, err = s.createOIDCUser(ctx, provider, identity, identityHash)
		if !errors.Is(err, apperrors.ErrAlreadyExists) {
			return created, err
		}
		// A simultaneous first sign-in of the same identity may have saved it
		// first; continue as that user. Otherwise the save hit another user.
		existing, err = s.repo.GetUserByExternalIdentity(ctx, provider, identity.Subject)
		if errors.Is(err, apperrors.ErrNotFound) {
			return model.User{}, apperrors.New(apperrors.ErrConflict, "oidc user email or username already exists")
		}
	}
	if err != nil {
		return model.User{}, fmt.Errorf("failed to get oidc user: %w", err)
	}
	if existing.Status != model.StatusActive {
		return existing, nil
	}
	return s.syncOIDCUser(ctx, existing, identity, identityHash)
}

// createOIDCUser saves a new user for the identity. A unique violation is
// returned as ErrAlreadyExists for the caller to resolve.
func (s *AuthService) createOIDCUser(ctx context.Context, provider string, identity OIDCIdentity, identityHash string) (model.User, error) {
	userID := uuid.New()
	username, err := s.resolveOIDCUsername(ctx, userID, "", deriveOIDCUsername(identity.PreferredUsername, identityHash), identityHash)
	if err != nil {
		return model.User{}, err
	}
	email, err := s.resolveOIDCEmail(ctx, userID, identity, identityHash)
	if err != nil {
		return model.User{}, err
	}
	role := model.RoleUser
	if s.providerManagesRoles() {
		role = s.roleFromProvider(identity)
	}
	user := model.User{
		ID:               userID,
		Username:         username,
		Email:            email,
		Role:             role,
		Status:           model.StatusActive,
		AuthSource:       model.AuthSourceOIDC,
		ExternalProvider: provider,
		ExternalSubject:  identity.Subject,
		ExternalUsername: strings.TrimSpace(identity.PreferredUsername),
	}
	if err := s.repo.SaveUser(ctx, user); err != nil {
		if errors.Is(err, apperrors.ErrAlreadyExists) {
			return model.User{}, err
		}
		return model.User{}, fmt.Errorf("failed to save oidc user: %w", err)
	}
	return user, nil
}

// syncOIDCUser writes the provider-managed username and email, and the role
// when the provider manages it. A demotion the last-admin guard refuses is
// skipped with a warning, so the last active administrator still signs in.
func (s *AuthService) syncOIDCUser(ctx context.Context, user model.User, identity OIDCIdentity, identityHash string) (model.User, error) {
	username, err := s.resolveOIDCUsername(ctx, user.ID, user.Username, deriveOIDCUsername(identity.PreferredUsername, identityHash), identityHash)
	if err != nil {
		return model.User{}, err
	}
	email, err := s.resolveOIDCEmail(ctx, user.ID, identity, identityHash)
	if err != nil {
		return model.User{}, err
	}
	user.Username = username
	user.Email = email
	user.ExternalUsername = strings.TrimSpace(identity.PreferredUsername)
	if s.providerManagesRoles() {
		user.Role = s.roleFromProvider(identity)
	}
	err = s.updateUser(ctx, user, false, false)
	if errors.Is(err, errLastActiveAdmin) {
		s.cfg.Logger.WarnContext(ctx, "oidc role synchronization skipped: the user is the last active admin", "user_id", user.ID)
		user.Role = model.RoleAdmin
		err = s.updateUser(ctx, user, false, false)
	}
	if errors.Is(err, apperrors.ErrAlreadyExists) {
		return model.User{}, apperrors.New(apperrors.ErrConflict, "oidc user email or username already exists")
	}
	if err != nil {
		return model.User{}, fmt.Errorf("failed to update oidc user: %w", err)
	}
	return user, nil
}

// resolveOIDCUsername picks the first candidate no other user holds: the
// derived base, the base with 8 characters of the identity hash, then "oidc-"
// and 24 of them. Past those an existing user keeps currentUsername, and a new
// user (empty currentUsername) is an account conflict.
func (s *AuthService) resolveOIDCUsername(ctx context.Context, currentUserID uuid.UUID, currentUsername, base, identityHash string) (string, error) {
	suffix := "-" + identityHash[:8]
	candidates := []string{base, cutUsername(base, maxDerivedUsername-len(suffix)) + suffix, "oidc-" + identityHash[:24]}
	for _, candidate := range candidates {
		user, err := s.repo.GetUserByUsername(ctx, candidate)
		if errors.Is(err, apperrors.ErrNotFound) || (err == nil && user.ID == currentUserID) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("failed to check oidc username: %w", err)
		}
	}
	if currentUsername != "" {
		return currentUsername, nil
	}
	return "", apperrors.New(apperrors.ErrConflict, "oidc username already exists")
}

// resolveOIDCEmail stores the provider's email only when it is verified, valid
// and no other user holds it; otherwise the deterministic fallback address.
func (s *AuthService) resolveOIDCEmail(ctx context.Context, currentUserID uuid.UUID, identity OIDCIdentity, identityHash string) (string, error) {
	if email := normalizeExternalEmail(identity.Email); identity.EmailVerified && email != "" {
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

	email := "oidc-" + identityHash[:24] + "@oidc.local"
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

// providerManagesRoles reports whether the identity provider owns the role of
// an OIDC user, which it does when administrator groups are configured.
func (s *AuthService) providerManagesRoles() bool {
	return len(s.adminGroups) > 0
}

// RoleManagedByProvider reports whether the identity provider owns user's role:
// an OIDC user's while administrator groups are configured.
func (s *AuthService) RoleManagedByProvider(user model.User) bool {
	return user.AuthSource == model.AuthSourceOIDC && s.providerManagesRoles()
}

// roleFromProvider is admin when any configured administrator group appears in
// the identity's groups or realm roles, and user otherwise.
func (s *AuthService) roleFromProvider(identity OIDCIdentity) string {
	for _, value := range append(slices.Clone(identity.Groups), identity.RealmRoles...) {
		if _, ok := s.adminGroups[value]; ok {
			return model.RoleAdmin
		}
	}
	return model.RoleUser
}

func (s *AuthService) CreateUser(ctx context.Context, input CreateUserInput) (model.User, error) {
	if err := validateUserInput(input.Role, input.Status); err != nil {
		return model.User{}, err
	}
	if err := validatePasswordLength(input.Password); err != nil {
		return model.User{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return model.User{}, apperrors.ErrInternal
	}

	user := model.User{
		ID:           uuid.New(),
		Username:     normalizeIdentity(input.Username),
		Email:        normalizeIdentity(input.Email),
		PasswordHash: string(hash),
		Role:         input.Role,
		Status:       input.Status,
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
	if err := validateUserInput(input.Role, input.Status); err != nil {
		return model.User{}, err
	}
	if err := validatePasswordLength(input.Password); err != nil {
		return model.User{}, err
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return model.User{}, err
	}
	if err := s.refuseProviderManagedEdit(user, input); err != nil {
		return model.User{}, err
	}

	user.Username = normalizeIdentity(input.Username)
	user.Email = normalizeIdentity(input.Email)
	user.Role = input.Role
	user.Status = input.Status
	if input.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			return model.User{}, apperrors.ErrInternal
		}
		user.PasswordHash = string(hash)
	}

	if err := s.updateUser(ctx, user, input.Password != "", true); err != nil {
		if errors.Is(err, apperrors.ErrAlreadyExists) {
			return model.User{}, apperrors.ErrAlreadyExists
		}
		if errors.Is(err, apperrors.ErrConflict) {
			return model.User{}, err
		}
		return model.User{}, fmt.Errorf("failed to update user: %w", err)
	}
	return user, nil
}

var (
	errProfileManagedByProvider = apperrors.New(apperrors.ErrBadRequest, "username, email and password of this user are managed by the identity provider")
	errRoleManagedByProvider    = apperrors.New(apperrors.ErrBadRequest, "role of this user is managed by the identity provider")
)

// refuseProviderManagedEdit refuses an update that would change what the
// identity provider owns and so be reverted at the user's next sign-in. The
// refusals echo no value.
func (s *AuthService) refuseProviderManagedEdit(user model.User, input UpdateUserInput) error {
	if user.AuthSource != model.AuthSourceOIDC {
		return nil
	}
	if normalizeIdentity(input.Username) != user.Username || normalizeIdentity(input.Email) != user.Email || input.Password != "" {
		return errProfileManagedByProvider
	}
	if s.RoleManagedByProvider(user) && input.Role != user.Role {
		return errRoleManagedByProvider
	}
	return nil
}

func (s *AuthService) DeactivateUser(ctx context.Context, userID uuid.UUID) (model.User, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return model.User{}, err
	}
	user.Status = model.StatusDeactivated
	if err := s.updateUser(ctx, user, false, true); err != nil {
		if errors.Is(err, apperrors.ErrConflict) {
			return model.User{}, err
		}
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
	if err := s.updateUser(ctx, user, false, true); err != nil {
		return model.User{}, fmt.Errorf("failed to reactivate user: %w", err)
	}
	return user, nil
}

func validateUserInput(role, status string) error {
	if !validRole(role) {
		return apperrors.New(apperrors.ErrBadRequest, "role must be one of: admin, user")
	}
	if !validStatus(status) {
		return apperrors.New(apperrors.ErrBadRequest, "status must be one of: active, deactivated")
	}
	return nil
}

// maxPasswordBytes is the longest password bcrypt hashes; it counts bytes, not characters.
const maxPasswordBytes = 72

// validatePasswordLength runs before hashing, so a too-long password is the
// caller's mistake rather than a hashing failure.
func validatePasswordLength(password string) error {
	if len(password) > maxPasswordBytes {
		return apperrors.New(apperrors.ErrBadRequest, "password must be at most 72 bytes")
	}
	return nil
}

func validRole(role string) bool {
	return role == model.RoleAdmin || role == model.RoleUser
}

func validStatus(status string) bool {
	return status == model.StatusActive || status == model.StatusDeactivated
}

func randomToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// secretDigest is the stored form of a login attempt's state or browser
// binding; neither secret is stored.
func secretDigest(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

// normalizeIdentity is the stored form of a username or an email: trimmed and
// lowercased, so identities that differ only by case are one identity.
func normalizeIdentity(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// maxDerivedUsername bounds a username derived from the provider, leaving room
// below the local limit.
const maxDerivedUsername = 30

// usernameSeparators are the characters besides a-z and 0-9 a derived
// username keeps; it neither starts nor ends with one.
const usernameSeparators = "._-"

// deriveOIDCUsername turns preferred_username into a username that follows the
// local rule: the part before the first @, lowercased, without characters
// outside a-z, 0-9 and the separators, cut to 30 characters. A result shorter
// than 3 characters becomes "oidc-" and 8 characters of the identity hash.
func deriveOIDCUsername(preferredUsername, identityHash string) string {
	local, _, _ := strings.Cut(strings.TrimSpace(preferredUsername), "@")
	var b strings.Builder
	for _, r := range strings.ToLower(local) {
		if ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') || strings.ContainsRune(usernameSeparators, r) {
			b.WriteRune(r)
		}
	}
	value := cutUsername(strings.TrimLeft(b.String(), usernameSeparators), maxDerivedUsername)
	if len(value) < 3 {
		return "oidc-" + identityHash[:8]
	}
	return value
}

// cutUsername cuts an ASCII username to limit and trims trailing separators.
func cutUsername(value string, limit int) string {
	if len(value) > limit {
		value = value[:limit]
	}
	return strings.TrimRight(value, usernameSeparators)
}

// oidcIdentityHash is the hex SHA-256 of provider:subject. Derived usernames and
// the fallback email use its prefixes, so they differ for every identity.
func oidcIdentityHash(provider, subject string) string {
	sum := sha256.Sum256([]byte(provider + ":" + subject))
	return hex.EncodeToString(sum[:])
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
