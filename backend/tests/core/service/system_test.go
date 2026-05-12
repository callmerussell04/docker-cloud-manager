package service_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/coretest"
	"github.com/stretchr/testify/require"
)

func TestSystemServiceGetAndUpdateConfig(t *testing.T) {
	manager, err := config.NewManager(filepath.Join(t.TempDir(), "config.json"), coretest.SystemConfig())
	require.NoError(t, err)
	svc := NewSystemService(manager)

	cfg := svc.GetConfig(context.Background())
	require.Equal(t, coretest.SystemConfig().BaseDomain, cfg.BaseDomain)

	cfg.BaseDomain = "example.test"
	require.NoError(t, svc.UpdateConfig(context.Background(), cfg))
	require.Equal(t, "example.test", svc.GetConfig(context.Background()).BaseDomain)
}

func TestSystemServiceRejectsInvalidConfig(t *testing.T) {
	manager, err := config.NewManager(filepath.Join(t.TempDir(), "config.json"), coretest.SystemConfig())
	require.NoError(t, err)
	svc := NewSystemService(manager)

	cfg := svc.GetConfig(context.Background())
	cfg.BuildPidsLimit = 0
	err = svc.UpdateConfig(context.Background(), cfg)
	require.ErrorIs(t, err, apperrors.ErrBadRequest)
	require.Equal(t, coretest.SystemConfig().BuildPidsLimit, svc.GetConfig(context.Background()).BuildPidsLimit)
}
