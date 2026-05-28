package client_test

import (
	"context"
	"net"
	"testing"
	"time"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	grpcclient "github.com/callmerussell04/docker-cloud-manager/internal/builder/grpc/client"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestCoreClientMapsBuilderConfigAndBuildRecordRPCs(t *testing.T) {
	images := &imageServer{}
	system := &systemServer{}
	conn := newCoreConn(t, func(server *grpc.Server) {
		coreapi.RegisterImageAPIServer(server, images)
		coreapi.RegisterSystemAPIServer(server, system)
	})
	client := grpcclient.NewCoreClient(conn)

	cfg, err := client.GetBuilderConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1024), cfg.BuildMemoryBytes)
	require.Equal(t, int64(200000), cfg.BuildCPUQuota)
	require.Equal(t, int64(100000), cfg.BuildCPUPeriod)
	require.Equal(t, 2.5, cfg.BuildMemorySwapMultiplier)
	require.Equal(t, int64(512), cfg.BuildPidsLimit)
	require.Equal(t, "build_net", cfg.BuildNetworkName)
	require.Equal(t, "kaniko:test", cfg.KanikoImage)
	require.Equal(t, 9*time.Minute, cfg.MaxBuildTime)
	require.Equal(t, 3, cfg.MaxConcurrentBuilds)
	require.Equal(t, int64(700), cfg.MaxUnpackedSizeBytes)
	require.Equal(t, int64(800), cfg.MaxBuildLogSizeBytes)
	require.Equal(t, 4*time.Second, cfg.BuildCancelPollInterval)

	imageID, started, buildStatus, err := client.StartBuildRecord(context.Background(), "build-id")
	require.NoError(t, err)
	require.Equal(t, "build-id", images.startedBuildID)
	require.Equal(t, "image-id", imageID)
	require.True(t, started)
	require.Equal(t, "running", buildStatus)

	err = client.CompleteBuildRecord(context.Background(), "build-id", "image-id", "success", 42)
	require.NoError(t, err)
	require.Equal(t, "build-id", images.completed.BuildId)
	require.Equal(t, "image-id", images.completed.ImageId)
	require.Equal(t, "success", images.completed.Status)
	require.Equal(t, int32(42), images.completed.SizeMb)

	gotStatus, err := client.GetBuildStatus(context.Background(), "build-id")
	require.NoError(t, err)
	require.Equal(t, "canceled", gotStatus)
	require.Equal(t, "build-id", images.getBuildID)
}

func TestCoreClientMapsGRPCErrors(t *testing.T) {
	conn := newCoreConn(t, func(server *grpc.Server) {
		coreapi.RegisterImageAPIServer(server, &imageServer{startErr: status.Error(codes.Unavailable, apperrors.ErrUnavailable.Error())})
		coreapi.RegisterSystemAPIServer(server, &systemServer{err: status.Error(codes.InvalidArgument, apperrors.ErrBadRequest.Error())})
	})
	client := grpcclient.NewCoreClient(conn)

	_, err := client.GetBuilderConfig(context.Background())
	require.ErrorIs(t, err, apperrors.ErrBadRequest)
	_, _, _, err = client.StartBuildRecord(context.Background(), "build-id")
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

type imageServer struct {
	coreapi.UnimplementedImageAPIServer
	startErr       error
	startedBuildID string
	completed      *coreapi.CompleteBuildRequest
	getBuildID     string
}

func (s *imageServer) StartBuildRecord(_ context.Context, req *coreapi.BuildActionRequest) (*coreapi.StartBuildRecordResponse, error) {
	if s.startErr != nil {
		return nil, s.startErr
	}
	s.startedBuildID = req.BuildId
	return &coreapi.StartBuildRecordResponse{ImageId: "image-id", Started: true, Status: "running"}, nil
}

func (s *imageServer) CompleteBuildRecord(_ context.Context, req *coreapi.CompleteBuildRequest) (*coreapi.Empty, error) {
	s.completed = req
	return &coreapi.Empty{}, nil
}

func (s *imageServer) GetBuild(_ context.Context, req *coreapi.BuildActionRequest) (*coreapi.BuildData, error) {
	s.getBuildID = req.BuildId
	return &coreapi.BuildData{Id: req.BuildId, Status: "canceled"}, nil
}

func (s *imageServer) GetBuildStatus(_ context.Context, req *coreapi.BuildActionRequest) (*coreapi.BuildStatusResponse, error) {
	s.getBuildID = req.BuildId
	return &coreapi.BuildStatusResponse{Status: "canceled"}, nil
}

type systemServer struct {
	coreapi.UnimplementedSystemAPIServer
	err error
}

func (s *systemServer) GetBuilderRuntimeConfig(context.Context, *coreapi.Empty) (*coreapi.BuilderRuntimeConfigData, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &coreapi.BuilderRuntimeConfigData{
		BuildMemoryBytes:               1024,
		BuildCpuQuota:                  200000,
		BuildCpuPeriod:                 100000,
		BuildMemorySwapMultiplier:      2.5,
		BuildPidsLimit:                 512,
		BuildNetworkName:               "build_net",
		KanikoImage:                    "kaniko:test",
		MaxBuildTimeMinutes:            9,
		MaxConcurrentBuilds:            3,
		MaxUnpackedSizeBytes:           700,
		MaxBuildLogSizeBytes:           800,
		BuildCancelPollIntervalSeconds: 4,
	}, nil
}
