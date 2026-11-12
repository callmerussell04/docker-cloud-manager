package grpc

import (
	"github.com/callmerussell04/docker-cloud-manager/backend/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
)

// CoreMethods are the only calls Core makes to SSO: reading users.
var CoreMethods = []string{
	sso.UserAPI_GetUser_FullMethodName,
	sso.UserAPI_BatchGetUsers_FullMethodName,
}

// CallerCredentials are the credentials of the services SSO accepts.
type CallerCredentials struct {
	Gateway string
	Core    string
}

// CallerPolicies is SSO's policy table. Gateway may make every call and state
// any scope kind; Core may only read users, with system scope.
func CallerPolicies(credentials CallerCredentials) []internalauth.CallerPolicy {
	return []internalauth.CallerPolicy{
		{
			Caller:     internalauth.CallerGateway,
			Credential: credentials.Gateway,
			Methods:    internalauth.AllMethods(),
			Scopes:     []accessscope.Kind{accessscope.KindUser, accessscope.KindAdmin, accessscope.KindSystem},
		},
		{
			Caller:     internalauth.CallerCore,
			Credential: credentials.Core,
			Methods:    internalauth.OnlyMethods(CoreMethods...),
			Scopes:     []accessscope.Kind{accessscope.KindSystem},
		},
	}
}
