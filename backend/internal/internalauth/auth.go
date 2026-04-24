package internalauth

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	HeaderName  = "X-Internal-Token"
	MetadataKey = "x-internal-token"
)

func UnaryServerInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if token == "" {
			return nil, status.Error(codes.Unauthenticated, "internal token is not configured")
		}
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok || len(md.Get(MetadataKey)) == 0 || md.Get(MetadataKey)[0] != token {
			return nil, status.Error(codes.Unauthenticated, "invalid internal token")
		}
		return handler(ctx, req)
	}
}

func UnaryClientInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, MetadataKey, token)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func Middleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token == "" || c.GetHeader(HeaderName) != token {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
