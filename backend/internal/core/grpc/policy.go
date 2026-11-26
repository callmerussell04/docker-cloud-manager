package grpc

import (
	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
)

// BuilderMethods are the only calls Builder makes to Core.
var BuilderMethods = []string{
	coreapi.ImageAPI_StartBuildRecord_FullMethodName,
	coreapi.ImageAPI_CompleteBuildRecord_FullMethodName,
	coreapi.ImageAPI_GetBuildStatus_FullMethodName,
	coreapi.SystemAPI_GetBuilderRuntimeConfig_FullMethodName,
}

// CallerCredentials are the credentials of the services Core accepts.
type CallerCredentials struct {
	Gateway string
	Builder string
}

// CallerPolicies is Core's policy table for its gRPC server and internal HTTP
// routes. Gateway may make every call but Builder's and state any scope kind;
// Builder may make only its own calls, with system scope, and no HTTP request.
func CallerPolicies(credentials CallerCredentials) []internalauth.CallerPolicy {
	return []internalauth.CallerPolicy{
		{
			Caller:     internalauth.CallerGateway,
			Credential: credentials.Gateway,
			Methods:    internalauth.AllMethodsExcept(BuilderMethods...),
			Scopes:     []accessscope.Kind{accessscope.KindUser, accessscope.KindAdmin, accessscope.KindSystem},
			HTTPRoutes: true,
		},
		{
			Caller:     internalauth.CallerBuilder,
			Credential: credentials.Builder,
			Methods:    internalauth.OnlyMethods(BuilderMethods...),
			Scopes:     []accessscope.Kind{accessscope.KindSystem},
		},
	}
}
