// Package ssotest provides SSO service fixtures for tests.
package ssotest

import (
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
)

// NewAuthService builds an AuthService with local auth enabled and no OIDC provider.
func NewAuthService(repo service.UserRepository, tokens service.TokenProvider, sessions service.SessionRepository) *service.AuthService {
	return service.NewAuthServiceWithConfig(repo, tokens, sessions, service.OIDCDeps{}, service.Config{
		LocalLoginEnabled:    true,
		LocalRegisterEnabled: true,
		OIDCDefaultRole:      model.RoleUser,
		OIDCProviderName:     "keycloak",
	})
}
