package internalauth

import (
	"context"
	"net/http"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestUnaryInterceptorsPropagateAccessScope(t *testing.T) {
	actorID := uuid.New()
	ctx := accessscope.WithAdminScope(context.Background(), actorID, "root", "admin")

	clientInterceptor := UnaryClientInterceptor("secret")
	serverInterceptor := UnaryServerInterceptor("secret")

	err := clientInterceptor(ctx, "/core.ContainerAPI/ListContainers", nil, nil, nil, func(outgoing context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		md, ok := metadata.FromOutgoingContext(outgoing)
		if !ok {
			t.Fatal("outgoing metadata was not set")
		}

		_, err := serverInterceptor(metadata.NewIncomingContext(context.Background(), md), nil, &grpc.UnaryServerInfo{FullMethod: method}, func(incoming context.Context, req any) (any, error) {
			scope, ok := accessscope.FromContext(incoming)
			if !ok {
				t.Fatal("scope was not injected into server context")
			}
			if scope.Kind != accessscope.KindAdmin || scope.UserID != actorID || scope.Username != "root" || scope.Role != "admin" {
				t.Fatalf("scope = %+v", scope)
			}
			return nil, nil
		})
		return err
	})
	if err != nil {
		t.Fatalf("interceptor round-trip returned error: %v", err)
	}
}

func TestSetScopeHeadersFromContext(t *testing.T) {
	actorID := uuid.New()
	header := make(http.Header)

	SetScopeHeadersFromContext(header, accessscope.WithUserScope(context.Background(), actorID, "alice", "user"))

	if header.Get(HeaderScope) != string(accessscope.KindUser) {
		t.Fatalf("scope header = %q", header.Get(HeaderScope))
	}
	if header.Get(HeaderUserID) != actorID.String() {
		t.Fatalf("user id header = %q", header.Get(HeaderUserID))
	}
	if header.Get(HeaderUsername) != "alice" || header.Get(HeaderRole) != "user" {
		t.Fatalf("identity headers = username %q role %q", header.Get(HeaderUsername), header.Get(HeaderRole))
	}
}
