package internalauth_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestUnaryInterceptorsPropagateAccessScope(t *testing.T) {
	actorID := uuid.New()
	ctx := accessscope.WithAdminScope(context.Background(), actorID, "root", "admin")

	clientInterceptor := internalauth.UnaryClientInterceptor("secret")
	serverInterceptor := internalauth.UnaryServerInterceptor("secret")

	err := clientInterceptor(ctx, "/core.ContainerAPI/ListContainers", nil, nil, nil, func(outgoing context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		md, ok := metadata.FromOutgoingContext(outgoing)
		require.True(t, ok, "outgoing metadata was not set")

		_, err := serverInterceptor(metadata.NewIncomingContext(context.Background(), md), nil, &grpc.UnaryServerInfo{FullMethod: method}, func(incoming context.Context, req any) (any, error) {
			scope, ok := accessscope.FromContext(incoming)
			require.True(t, ok, "scope was not injected into server context")
			require.Equal(t, accessscope.KindAdmin, scope.Kind)
			require.Equal(t, actorID, scope.UserID)
			require.Equal(t, "root", scope.Username)
			require.Equal(t, "admin", scope.Role)
			return nil, nil
		})
		return err
	})
	require.NoError(t, err)
}

func TestSetScopeHeadersFromContext(t *testing.T) {
	actorID := uuid.New()
	header := make(http.Header)

	internalauth.SetScopeHeadersFromContext(header, accessscope.WithUserScope(context.Background(), actorID, "alice", "user"))

	require.Equal(t, string(accessscope.KindUser), header.Get(internalauth.HeaderScope))
	require.Equal(t, actorID.String(), header.Get(internalauth.HeaderUserID))
	require.Equal(t, "alice", header.Get(internalauth.HeaderUsername))
	require.Equal(t, "user", header.Get(internalauth.HeaderRole))
}
