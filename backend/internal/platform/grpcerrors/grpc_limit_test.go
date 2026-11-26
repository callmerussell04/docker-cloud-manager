package grpcerrors_test

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// failingServer answers every Check with the gRPC form of err, as a Core
// handler answers a failed service call.
type failingServer struct {
	healthpb.UnimplementedHealthServer
	err error
}

func (s failingServer) Check(context.Context, *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return nil, s.err
}

// overTheWire sends serverErr from a real gRPC server to a real client and
// returns what the client maps it back to, so the test covers what survives
// serialization and not only what one process remembers.
func overTheWire(t *testing.T, serverErr error) error {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	healthpb.RegisterHealthServer(server, failingServer{err: serverErr})
	go func() {
		_ = server.Serve(listener) // ends with the listener; the test asserts on the client's answer
	}()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
		server.Stop()
	})

	_, callErr := healthpb.NewHealthClient(conn).Check(t.Context(), &healthpb.HealthCheckRequest{})
	require.Error(t, callErr)
	return grpcerrors.FromGRPC(callErr)
}

// A rejection keeps its kind, its message and its limit name from the service
// that decided it to the service that answers the client.
func TestLimitSurvivesTheWire(t *testing.T) {
	kinds := map[apperrors.Limit]error{
		apperrors.LimitUserMemory:               apperrors.ErrQuotaExceeded,
		apperrors.LimitUserCPU:                  apperrors.ErrQuotaExceeded,
		apperrors.LimitUserDisk:                 apperrors.ErrQuotaExceeded,
		apperrors.LimitHostMemory:               apperrors.ErrHostExhausted,
		apperrors.LimitHostCPU:                  apperrors.ErrHostExhausted,
		apperrors.LimitDiskFloor:                apperrors.ErrHostExhausted,
		apperrors.LimitContainersPerUser:        apperrors.ErrLimitExceeded,
		apperrors.LimitVolumesPerUser:           apperrors.ErrLimitExceeded,
		apperrors.LimitQueuedContainerCreates:   apperrors.ErrLimitExceeded,
		apperrors.LimitQueuedBuilds:             apperrors.ErrLimitExceeded,
		apperrors.LimitQueuedComposeDeployments: apperrors.ErrLimitExceeded,
		apperrors.LimitStagedSourceBytes:        apperrors.ErrQuotaExceeded,
	}
	require.Len(t, kinds, len(apperrors.Limits()), "every limit is covered")
	for limit, kind := range kinds {
		t.Run(string(limit), func(t *testing.T) {
			source := fmt.Errorf("failed to create service web: %w", apperrors.Exceeded(limit))

			received := overTheWire(t, grpcerrors.ToGRPC(source))

			require.ErrorIs(t, received, kind)
			require.Equal(t, limit, apperrors.LimitOf(received))
			require.Equal(t, apperrors.SafeMessage(source), apperrors.SafeMessage(received))
			require.Equal(t, codes.ResourceExhausted, grpcerrors.Code(source))
		})
	}
}

// An exhausted answer that names no limit is mapped by its message, as it
// was before limits had names.
func TestExhaustedAnswerWithoutALimitMapsByMessage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message string
		kind    error
	}{
		{name: "quota kind", message: apperrors.ErrQuotaExceeded.Error(), kind: apperrors.ErrQuotaExceeded},
		{name: "a quota message", message: "user disk quota exceeded", kind: apperrors.ErrQuotaExceeded},
		{name: "host kind", message: apperrors.ErrHostExhausted.Error(), kind: apperrors.ErrHostExhausted},
		{name: "limit kind", message: apperrors.ErrLimitExceeded.Error(), kind: apperrors.ErrLimitExceeded},
		{name: "a stream limit", message: "maximum telemetry log streams reached", kind: apperrors.ErrResourceExhausted},
		{name: "anything else", message: "server capacity reached, cannot allocate resources", kind: apperrors.ErrResourceExhausted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			received := overTheWire(t, status.Error(codes.ResourceExhausted, tc.message))

			require.ErrorIs(t, received, tc.kind)
			require.Empty(t, apperrors.LimitOf(received))
			require.Equal(t, tc.message, apperrors.SafeMessage(received))
		})
	}
}

// Only DCM's own limit names are restored: a detail from another domain, an
// unknown name, or a named limit on an answer that is not an exhausted one
// changes nothing.
func TestForeignOrUnknownLimitDetailIsIgnored(t *testing.T) {
	withDetail := func(code codes.Code, message, domain, reason string) error {
		st, err := status.New(code, message).WithDetails(&errdetails.ErrorInfo{Domain: domain, Reason: reason})
		require.NoError(t, err)
		return st.Err()
	}
	for _, tc := range []struct {
		name string
		err  error
		kind error
	}{
		{name: "another domain", err: withDetail(codes.ResourceExhausted, "user disk quota exceeded", "googleapis.com", "user_cpu"), kind: apperrors.ErrQuotaExceeded},
		{name: "an unknown limit", err: withDetail(codes.ResourceExhausted, "user disk quota exceeded", grpcerrors.LimitDomain, "made_up"), kind: apperrors.ErrQuotaExceeded},
		{name: "not an exhausted answer", err: withDetail(codes.NotFound, "not found", grpcerrors.LimitDomain, "user_cpu"), kind: apperrors.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			received := overTheWire(t, tc.err)

			require.ErrorIs(t, received, tc.kind)
			require.Empty(t, apperrors.LimitOf(received))
		})
	}
}
