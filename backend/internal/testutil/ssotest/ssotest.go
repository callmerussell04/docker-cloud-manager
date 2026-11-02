// Package ssotest provides SSO service fixtures for tests.
package ssotest

import (
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
)

// NewAuthService builds an AuthService with local auth enabled and no OIDC provider.
func NewAuthService(repo service.UserRepository, tokens service.TokenProvider, sessions service.SessionRepository) *service.AuthService {
	return service.NewAuthServiceWithConfig(repo, tokens, sessions, nil, service.Config{
		LocalLoginEnabled:    true,
		LocalRegisterEnabled: true,
		OIDCDefaultRole:      model.RoleUser,
		OIDCProviderName:     "keycloak",
		OIDCStateTTL:         10 * time.Minute,
	})
}
