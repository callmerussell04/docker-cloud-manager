package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
)

const RegistryService = "dcm-registry"
const RegistryTokenTTL = time.Minute

// Distribution v2.8.3 allows 60 seconds of clock skew. The private edge bounds
// admitted requests to another minute. Include one second for timestamp rounding.
// The repository applies this fence to issued build tokens and cleanup jobs.
const RegistryCleanupDelay = 181 * time.Second

// RegistryPullCredentialLifetime bounds the daemon's read-only pull identity.
const RegistryPullCredentialLifetime = 30 * time.Minute

type RegistryGrantRepository interface {
	ClaimBuild(ctx context.Context, id uuid.UUID, hash []byte, lifetime time.Duration, maxRunning int) (model.Build, bool, error)
	AuthorizeCredential(ctx context.Context, hash []byte, now time.Time) (model.RegistryPrincipal, error)
	RepositoryExists(ctx context.Context, repository string) (bool, error)
	SavePullCredential(ctx context.Context, id uuid.UUID, repository string, hash []byte, lifetime time.Duration) error
	RevokeBuild(ctx context.Context, id uuid.UUID, pushedTag model.RegistryTagDisposition, fence time.Duration) error
	FenceRepository(ctx context.Context, repository string, fence time.Duration) (time.Duration, error)
	LockRegistryRepository(ctx context.Context, repository string) (func(), error)
	HasActiveRegistryBuilds(ctx context.Context, repository string) (bool, error)
}

type RegistryTokenSigner interface {
	SignRegistryToken(ctx context.Context, grant model.RegistryTokenGrant) (string, error)
}

type RegistryAuthorization struct {
	repo      RegistryGrantRepository
	users     UserInfoProvider
	signer    RegistryTokenSigner
	authority string
}

// RegistryAuthorizationDeps are all required: claims, token exchange and
// trusted tokens each depend on every one of them.
type RegistryAuthorizationDeps struct {
	Repository RegistryGrantRepository
	Users      UserInfoProvider
	Signer     RegistryTokenSigner
	// Authority is the Registry host[:port] that build destinations name.
	Authority string
}

func NewRegistryAuthorization(deps RegistryAuthorizationDeps) (*RegistryAuthorization, error) {
	required := []struct {
		name  string
		value any
	}{
		{"repository", deps.Repository}, {"users", deps.Users}, {"signer", deps.Signer},
	}
	for _, dependency := range required {
		if dependency.value == nil || isNilServiceDependency(dependency.value) {
			return nil, fmt.Errorf("registry authorization: %s is required", dependency.name)
		}
	}
	if deps.Authority == "" {
		return nil, errors.New("registry authorization: authority is required")
	}
	return &RegistryAuthorization{repo: deps.Repository, users: deps.Users, signer: deps.Signer, authority: deps.Authority}, nil
}

// ClaimBuild moves a pending build to running and issues its Registry
// credential, unless maxRunning builds already run: then the build stays
// pending and is returned unclaimed.
func (s *RegistryAuthorization) ClaimBuild(ctx context.Context, id uuid.UUID, lifetime time.Duration, maxRunning int) (model.Build, bool, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return model.Build{}, false, apperrors.Wrap(apperrors.ErrInternal, apperrors.ErrInternal.Error(), fmt.Errorf("generate registry build credential: %w", err))
	}
	password := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(password))
	b, started, err := s.repo.ClaimBuild(ctx, id, hash[:], lifetime, maxRunning)
	if err != nil {
		return b, false, fmt.Errorf("claim registry build: %w", err)
	}
	if !started {
		return b, false, nil
	}
	b.RegistryCredential.Username = id.String()
	b.RegistryCredential.Password = password
	b.RegistryCredential.Destination = s.authority + "/" + b.RegistryCredential.Destination
	return b, true, nil
}

func (s *RegistryAuthorization) Exchange(ctx context.Context, username, password, audience string, scopes []string) (string, error) {
	if audience != RegistryService || password == "" || len(password) > 128 || len(scopes) > 32 {
		return "", apperrors.ErrForbidden
	}
	id, err := uuid.Parse(username)
	if err != nil {
		return "", apperrors.ErrForbidden
	}
	now := time.Now().UTC()
	hash := sha256.Sum256([]byte(password))
	principal, err := s.repo.AuthorizeCredential(ctx, hash[:], now)
	if err != nil {
		return "", fmt.Errorf("authorize registry credential: %w", err)
	}
	if principal.BuildID != id {
		return "", apperrors.ErrForbidden
	}
	user, err := s.users.GetUser(ctx, principal.OwnerID)
	if err != nil {
		return "", fmt.Errorf("get registry credential owner: %w", err)
	}
	if user.Status != "active" {
		return "", apperrors.ErrForbidden
	}
	access := make([]model.RegistryAccess, 0, len(scopes))
	for _, scope := range scopes {
		parts := strings.Split(scope, ":")
		if len(parts) != 3 || parts[0] != "repository" {
			continue
		}
		name := parts[1]
		pull := name == principal.Repository
		for _, base := range principal.PullRepositories {
			if base == name {
				pull = true
			}
		}
		actions := []string{}
		seen := map[string]bool{}
		for _, action := range strings.Split(parts[2], ",") {
			allowed := (action == "pull" && pull) || (action == "push" && name == principal.Repository && !principal.ReadOnly)
			if allowed && !seen[action] {
				actions = append(actions, action)
				seen[action] = true
			}
		}
		if len(actions) > 0 {
			access = append(access, model.RegistryAccess{Repository: name, Actions: actions})
		}
	}
	return s.signer.SignRegistryToken(ctx, model.RegistryTokenGrant{Subject: id.String(), Service: RegistryService, IssuedAt: now, ExpiresAt: now.Add(RegistryTokenTTL), Access: access})
}

// TrustedToken is available only in the Core process. It never accepts a
// tenant-supplied action list and only signs exact durable repositories.
func (s *RegistryAuthorization) TrustedToken(ctx context.Context, repository string, access model.RegistryTrustedAccess) (string, error) {
	var subject string
	var actions []string
	switch access {
	case model.RegistryTrustedPull:
		subject, actions = "core-pull", []string{"pull"}
	case model.RegistryTrustedCleanup:
		subject, actions = "core-cleanup", []string{"pull", "delete"}
	default:
		return "", apperrors.ErrForbidden
	}
	exists, err := s.repo.RepositoryExists(ctx, repository)
	if err != nil {
		return "", fmt.Errorf("check trusted registry repository: %w", err)
	}
	if !exists {
		return "", apperrors.ErrForbidden
	}
	now := time.Now().UTC()
	return s.signer.SignRegistryToken(ctx, model.RegistryTokenGrant{Subject: subject, Service: RegistryService, IssuedAt: now, ExpiresAt: now.Add(RegistryTokenTTL), Access: []model.RegistryAccess{{Repository: repository, Actions: actions}}})
}

func (s *RegistryAuthorization) RevokeBuild(ctx context.Context, id uuid.UUID, pushedTag model.RegistryTagDisposition) error {
	if pushedTag != model.RegistryTagKeep && pushedTag != model.RegistryTagDelete {
		return apperrors.Wrap(apperrors.ErrInternal, apperrors.ErrInternal.Error(), fmt.Errorf("revoke registry build: unknown tag disposition %q", pushedTag))
	}
	if err := s.repo.RevokeBuild(ctx, id, pushedTag, RegistryCleanupDelay); err != nil {
		return fmt.Errorf("revoke registry build: %w", err)
	}
	return nil
}

// FenceAndLockRepository waits until no build can still write to the
// repository, then locks it against new claims and returns the unlock function.
// Repository tokens cover all tags, so sibling-tag builds must finish first. The
// wait runs unlocked because the lock pool's single connection is shared with
// all admission; a claim that slips in before the lock fails the fence with
// ErrUnavailable so the caller requeues instead of waiting under the lock.
func (s *RegistryAuthorization) FenceAndLockRepository(ctx context.Context, repository string) (func(), error) {
	for {
		active, remaining, err := s.fenceRepository(ctx, repository)
		if err != nil {
			return nil, err
		}
		if !active {
			if err := sleepContext(ctx, remaining); err != nil {
				return nil, err
			}
			break
		}
		if err := sleepContext(ctx, time.Second); err != nil {
			return nil, err
		}
	}
	unlock, err := s.repo.LockRegistryRepository(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("lock registry repository: %w", err)
	}
	// Claims take the same lock, so a quiet recheck stays true until unlock.
	active, remaining, err := s.fenceRepository(ctx, repository)
	if err != nil {
		unlock()
		return nil, err
	}
	if active || remaining > 0 {
		unlock()
		return nil, apperrors.New(apperrors.ErrUnavailable, "registry repository received a new build during deletion fence")
	}
	return unlock, nil
}

// fenceRepository reports whether a build of the repository is running and,
// when none is, revokes finished builds' credentials and returns how long their
// issued tokens may still write.
func (s *RegistryAuthorization) fenceRepository(ctx context.Context, repository string) (bool, time.Duration, error) {
	active, err := s.repo.HasActiveRegistryBuilds(ctx, repository)
	if err != nil {
		return false, 0, fmt.Errorf("check active registry builds: %w", err)
	}
	if active {
		return true, 0, nil
	}
	remaining, err := s.repo.FenceRepository(ctx, repository, RegistryCleanupDelay)
	if err != nil {
		return false, 0, fmt.Errorf("fence registry repository: %w", err)
	}
	return false, remaining, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// PullCredential gives the daemon a renewable, exact-repository identity. It
// cannot push/delete, and is never installed inside an application container.
func (s *RegistryAuthorization) PullCredential(ctx context.Context, repository string) (model.RegistryCredential, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return model.RegistryCredential{}, apperrors.Wrap(apperrors.ErrInternal, apperrors.ErrInternal.Error(), fmt.Errorf("generate registry pull credential: %w", err))
	}
	password := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(password))
	id := uuid.New()
	if err := s.repo.SavePullCredential(ctx, id, repository, hash[:], RegistryPullCredentialLifetime); err != nil {
		return model.RegistryCredential{}, fmt.Errorf("save registry pull credential: %w", err)
	}
	return model.RegistryCredential{Username: id.String(), Password: password}, nil
}
