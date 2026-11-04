package internalauth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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

func TestInternalAuthRejectsUserScopeWithoutUserID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		userID string
	}{
		{name: "all-zero UUID", userID: uuid.Nil.String()},
		{name: "missing user ID", userID: ""},
		{name: "malformed user ID", userID: "not-a-uuid"},
	} {
		t.Run(tc.name+" over gRPC metadata", func(t *testing.T) {
			md := metadata.Pairs(
				internalauth.MetadataKey, "secret",
				internalauth.MetadataScope, string(accessscope.KindUser),
				internalauth.MetadataUserID, tc.userID,
			)
			called := false
			_, err := internalauth.UnaryServerInterceptor("secret")(
				metadata.NewIncomingContext(context.Background(), md), nil,
				&grpc.UnaryServerInfo{FullMethod: "/core.ContainerAPI/ListContainers"},
				func(context.Context, any) (any, error) {
					called = true
					return nil, nil
				})
			require.Equal(t, codes.Unauthenticated, status.Code(err))
			require.False(t, called, "handler must not run without a valid user ID")
		})
		t.Run(tc.name+" over HTTP headers", func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			called := false
			router.GET("/internal", internalauth.Middleware("secret"), func(c *gin.Context) {
				called = true
				c.Status(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/internal", nil)
			req.Header.Set(internalauth.HeaderName, "secret")
			req.Header.Set(internalauth.HeaderScope, string(accessscope.KindUser))
			req.Header.Set(internalauth.HeaderUserID, tc.userID)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.False(t, called, "handler must not run without a valid user ID")
		})
	}
}
