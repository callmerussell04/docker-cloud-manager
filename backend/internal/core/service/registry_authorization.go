package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

const RegistryService = "dcm-registry"
const RegistryTokenTTL = time.Minute

// Distribution v2.8.3 allows 60 seconds of clock skew. The private edge bounds
// admitted requests to another minute. Include one second for timestamp rounding.
const RegistryCleanupDelay = 181 * time.Second

type RegistryGrantRepository interface {
	ClaimBuild(ctx context.Context, id uuid.UUID, hash []byte, lifetime time.Duration) (model.Build, bool, error)
	AuthorizeCredential(ctx context.Context, hash []byte, now time.Time) (model.RegistryPrincipal, error)
	RepositoryExists(ctx context.Context, repository string) (bool, error)
	SavePullCredential(ctx context.Context, id uuid.UUID, repository string, hash []byte) error
	RevokeBuild(ctx context.Context, id uuid.UUID, cleanup bool) error
	FenceRepository(ctx context.Context, repository string) (time.Duration, error)
	PendingRegistryCleanup(ctx context.Context) ([]model.RegistryCleanup, error)
	CompleteRegistryCleanup(ctx context.Context, id uuid.UUID) error
	LockRegistryRepository(ctx context.Context, repository string) (func(), error)
	RegistryCleanupState(ctx context.Context, job model.RegistryCleanup) (model.RegistryCleanupState, error)
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

func NewRegistryAuthorization(repo RegistryGrantRepository, users UserInfoProvider, signer RegistryTokenSigner, authority string) *RegistryAuthorization {
	return &RegistryAuthorization{repo: repo, users: users, signer: signer, authority: authority}
}

func (s *RegistryAuthorization) ClaimBuild(ctx context.Context, id uuid.UUID, lifetime time.Duration) (model.Build, bool, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return model.Build{}, false, apperrors.ErrInternal
	}
	password := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(password))
	b, started, err := s.repo.ClaimBuild(ctx, id, hash[:], lifetime)
	if err != nil || !started {
		return b, started, err
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
		return "", err
	}
	if principal.BuildID != id {
		return "", apperrors.ErrForbidden
	}
	user, err := s.users.GetUser(ctx, principal.OwnerID)
	if err != nil {
		return "", err
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
func (s *RegistryAuthorization) TrustedToken(ctx context.Context, repository string, cleanup bool) (string, error) {
	exists, err := s.repo.RepositoryExists(ctx, repository)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", apperrors.ErrForbidden
	}
	actions := []string{"pull"}
	subject := "core-pull"
	if cleanup {
		actions = append(actions, "delete")
		subject = "core-cleanup"
	}
	now := time.Now().UTC()
	return s.signer.SignRegistryToken(ctx, model.RegistryTokenGrant{Subject: subject, Service: RegistryService, IssuedAt: now, ExpiresAt: now.Add(RegistryTokenTTL), Access: []model.RegistryAccess{{Repository: repository, Actions: actions}}})
}

func (s *RegistryAuthorization) RevokeBuild(ctx context.Context, id uuid.UUID, cleanup bool) error {
	return s.repo.RevokeBuild(ctx, id, cleanup)
}

// FenceRepository waits until no build can still write to the repository, then
// locks it against new claims and returns the unlock function. Repository
// tokens cover all tags, so sibling-tag builds must finish first. The wait runs
// unlocked because the lock pool's single connection is shared with all
// admission; a claim that slips in before the lock fails the fence with
// ErrUnavailable so the caller requeues instead of waiting under the lock.
func (s *RegistryAuthorization) FenceRepository(ctx context.Context, repository string) (func(), error) {
	if err := s.waitForRepositoryWriters(ctx, repository); err != nil {
		return nil, err
	}
	unlock, err := s.repo.LockRegistryRepository(ctx, repository)
	if err != nil {
		return nil, err
	}
	quiet, err := s.repositoryWritersDone(ctx, repository)
	if err != nil {
		unlock()
		return nil, err
	}
	if !quiet {
		unlock()
		return nil, apperrors.New(apperrors.ErrUnavailable, "registry repository received a new build during deletion fence")
	}
	return unlock, nil
}

func (s *RegistryAuthorization) waitForRepositoryWriters(ctx context.Context, repository string) error {
	for {
		active, err := s.repo.HasActiveRegistryBuilds(ctx, repository)
		if err != nil {
			return err
		}
		if !active {
			break
		}
		if err := sleepContext(ctx, time.Second); err != nil {
			return err
		}
	}
	remaining, err := s.repo.FenceRepository(ctx, repository)
	if err != nil {
		return err
	}
	return sleepContext(ctx, remaining)
}

// repositoryWritersDone rechecks the fence without waiting; the caller holds the
// repository lock, so its answer stays true until unlock.
func (s *RegistryAuthorization) repositoryWritersDone(ctx context.Context, repository string) (bool, error) {
	active, err := s.repo.HasActiveRegistryBuilds(ctx, repository)
	if err != nil || active {
		return false, err
	}
	remaining, err := s.repo.FenceRepository(ctx, repository)
	return remaining <= 0, err
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

func (s *RegistryAuthorization) RunCleanup(ctx context.Context, registry ImageRegistryAPI) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			jobs, err := s.repo.PendingRegistryCleanup(ctx)
			if err != nil {
				continue
			}
			for _, job := range jobs {
				taskCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
				_ = s.cleanupRegistryJob(taskCtx, registry, job)
				cancel()
			}

		}
	}
}

// PullCredential gives the daemon a renewable, exact-repository identity. It
// cannot push/delete, and is never installed inside an application container.
func (s *RegistryAuthorization) PullCredential(ctx context.Context, repository string) (model.RegistryCredential, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return model.RegistryCredential{}, apperrors.ErrInternal
	}
	password := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(password))
	id := uuid.New()
	if err := s.repo.SavePullCredential(ctx, id, repository, hash[:]); err != nil {
		return model.RegistryCredential{}, err
	}
	return model.RegistryCredential{Username: id.String(), Password: password}, nil
}

func (s *RegistryAuthorization) cleanupRegistryJob(ctx context.Context, registry ImageRegistryAPI, job model.RegistryCleanup) error {
	unlock, err := s.repo.LockRegistryRepository(ctx, job.Repository)
	if err != nil {
		return err
	}
	defer unlock()
	state, err := s.repo.RegistryCleanupState(ctx, job)
	if err != nil {
		return err
	}
	if state == model.RegistryCleanupPending {
		return nil
	}
	if state == model.RegistryCleanupReady {
		_, digest, err := registry.GetImageSizeAndDigest(ctx, job.Repository, job.Tag)
		if err == nil {
			err = registry.DeleteManifest(ctx, job.Repository, digest)
		}
		if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
	}
	return s.repo.CompleteRegistryCleanup(ctx, job.ID)
}
