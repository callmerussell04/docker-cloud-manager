package coretest_test

import (
	"context"
	"testing"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coregrpcmocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/grpc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCoreGRPCConnBoundsCallsWithoutCallerDeadline(t *testing.T) {
	containers := coregrpcmocks.NewMockContainerLogic(t)
	containers.EXPECT().List(mock.Anything, 10, 0, (*uuid.UUID)(nil)).
		RunAndReturn(func(ctx context.Context, _, _ int, _ *uuid.UUID) ([]model.Container, int, error) {
			if _, ok := ctx.Deadline(); !ok {
				return nil, 0, apperrors.ErrInternal
			}
			<-ctx.Done()
			return nil, 0, apperrors.ErrTimeout
		}).Once()
	conn := coretest.NewCoreGRPCConn(t, coretest.CoreGRPCRegistration{Containers: containers})
	ctx := coretest.InternalGRPCContext(context.Background(), coretest.GatewayCredential, accessscope.Scope{Kind: accessscope.KindUser, UserID: uuid.New()})

	_, err := coreapi.NewContainerAPIClient(conn).ListContainers(ctx, &coreapi.PaginationRequest{Limit: 10})
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
}
