package grpc

import (
	"context"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func timePtrUnix(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return value.Unix()
}

func requireUserOrAdminScope(ctx context.Context) error {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return err
	}
	if scope.Kind == accessscope.KindUser || scope.Kind == accessscope.KindAdmin {
		return nil
	}
	return apperrors.ErrForbidden
}

func requireUserScope(ctx context.Context) error {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return err
	}
	if scope.Kind == accessscope.KindUser {
		return nil
	}
	return apperrors.ErrForbidden
}

func requireAdminScope(ctx context.Context) error {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return err
	}
	if scope.Kind == accessscope.KindAdmin {
		return nil
	}
	return apperrors.ErrForbidden
}

func requireSystemScope(ctx context.Context) error {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return err
	}
	if scope.Kind == accessscope.KindSystem {
		return nil
	}
	return apperrors.ErrForbidden
}
