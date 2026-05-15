package client_test

import (
	"context"
	"net"
	"testing"
	"time"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	telemetryclient "github.com/callmerussell04/docker-cloud-manager/internal/telemetry/grpc/client"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestCoreClientMapsContainerTargetAndRuntimeConfig(t *testing.T) {
	containers := &containerServer{}
	system := &systemServer{}
	conn := newCoreConn(t, func(server *grpc.Server) {
		coreapi.RegisterContainerAPIServer(server, containers)
		coreapi.RegisterSystemAPIServer(server, system)
	})
	client := telemetryclient.NewCoreClient(conn)

	target, err := client.GetContainerTarget(context.Background(), "container-id")
	require.NoError(t, err)
	require.Equal(t, "container-id", containers.targetRequestID)
	require.Equal(t, "container-id", target.ContainerID)
	require.Equal(t, "docker-id", target.DockerID)
	require.Equal(t, "running", target.Status)
	require.Equal(t, "owner-id", target.OwnerID)
	require.Equal(t, 3, target.DockerGeneration)

	cfg, err := client.GetRuntimeConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, 500, cfg.MaxLogTailLines)
	require.Equal(t, 2, cfg.MaxLogStreamsPerUser)
	require.Equal(t, 3, cfg.MaxTerminalSessionsPerUser)
	require.Equal(t, 4*time.Second, cfg.TerminalIdleTimeout)
	require.Equal(t, 5*time.Second, cfg.TerminalMaxDuration)
	require.Equal(t, []string{"/bin/sh", "/usr/bin/top"}, cfg.AllowedExecCommands)
	require.Equal(t, 6, cfg.MaxCommandArgs)
	require.Equal(t, 7, cfg.MaxCommandArgBytes)
	require.Equal(t, int64(8192), cfg.WSReadLimitBytes)
}

func TestCoreClientMapsGRPCErrors(t *testing.T) {
	conn := newCoreConn(t, func(server *grpc.Server) {
		coreapi.RegisterContainerAPIServer(server, &containerServer{err: status.Error(codes.NotFound, apperrors.ErrNotFound.Error())})
		coreapi.RegisterSystemAPIServer(server, &systemServer{err: status.Error(codes.Unavailable, apperrors.ErrUnavailable.Error())})
	})
	client := telemetryclient.NewCoreClient(conn)

	_, err := client.GetContainerTarget(context.Background(), "container-id")
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	_, err = client.GetRuntimeConfig(context.Background())
	require.ErrorIs(t, err, apperrors.ErrUnavailable)
}

func newCoreConn(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	register(server)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

type containerServer struct {
	coreapi.UnimplementedContainerAPIServer
	err             error
	targetRequestID string
}

func (s *containerServer) GetContainerRuntimeTarget(_ context.Context, req *coreapi.ContainerRuntimeTargetRequest) (*coreapi.ContainerRuntimeTarget, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.targetRequestID = req.GetContainerId()
	return &coreapi.ContainerRuntimeTarget{
		ContainerId:      req.GetContainerId(),
		DockerId:         "docker-id",
		Status:           "running",
		OwnerId:          "owner-id",
		DockerGeneration: 3,
	}, nil
}

type systemServer struct {
	coreapi.UnimplementedSystemAPIServer
	err error
}

func (s *systemServer) GetConfig(context.Context, *coreapi.Empty) (*coreapi.SystemConfigData, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &coreapi.SystemConfigData{
		TelemetryMaxLogTailLines:            500,
		TelemetryMaxLogStreamsPerUser:       2,
		TelemetryMaxTerminalSessionsPerUser: 3,
		TelemetryTerminalIdleTimeoutSeconds: 4,
		TelemetryTerminalMaxDurationSeconds: 5,
		TelemetryAllowedExecCommands:        []string{"/bin/sh", "/usr/bin/top"},
		TelemetryMaxCommandArgs:             6,
		TelemetryMaxCommandArgBytes:         7,
		TelemetryWsReadLimitBytes:           8192,
	}, nil
}
