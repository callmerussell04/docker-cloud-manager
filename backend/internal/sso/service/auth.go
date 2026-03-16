package service

import (
	"context"
	"errors"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/domain"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type UserRepository interface {
	SaveUser(ctx context.Context, user domain.User) error
	GetUserByUsername(ctx context.Context, username string) (domain.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error)
}

type TokenProvider interface {
	GenerateTokens(user domain.User) (string, string, error)
	ValidateRefreshToken(token string) (uuid.UUID, error)
}

type Auth struct {
	repo          UserRepository
	tokenProvider TokenProvider
}

func NewAuth(repo UserRepository, tokenProvider TokenProvider) *Auth {
	return &Auth{
		repo:          repo,
		tokenProvider: tokenProvider,
	}
}

func (s *Auth) Register(ctx context.Context, username, email, password string) (uuid.UUID, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return uuid.Nil, domain.ErrInternal
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
		if errors.Is(err, domain.ErrUserAlreadyExists) {
			return uuid.Nil, domain.ErrUserAlreadyExists
		}
		return uuid.Nil, domain.ErrInternal
	}

	return user.ID, nil
}

func (s *Auth) Login(ctx context.Context, username, password string) (string, string, error) {
	user, err := s.repo.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return "", "", domain.ErrInvalidCredentials
		}
		return "", "", domain.ErrInternal
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return "", "", domain.ErrInvalidCredentials
	}

	accessToken, refreshToken, err := s.tokenProvider.GenerateTokens(user)
	if err != nil {
		return "", "", domain.ErrInternal
	}

	return accessToken, refreshToken, nil
}

func (s *Auth) Refresh(ctx context.Context, refreshToken string) (string, string, error) {
	userID, err := s.tokenProvider.ValidateRefreshToken(refreshToken)
	if err != nil {
		return "", "", domain.ErrInvalidToken
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return "", "", domain.ErrInvalidToken
	}

	accessToken, newRefreshToken, err := s.tokenProvider.GenerateTokens(user)
	if err != nil {
		return "", "", domain.ErrInternal
	}

	return accessToken, newRefreshToken, nil
}
