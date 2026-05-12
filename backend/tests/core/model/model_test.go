package model_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/stretchr/testify/require"
)

func TestBuildStatusHelpers(t *testing.T) {
	require.True(t, model.IsBuildTerminalStatus(model.BuildStatusCanceled))
	require.True(t, model.IsBuildTerminalStatus(model.BuildStatusFailedQuotaExceeded))
	require.False(t, model.IsBuildTerminalStatus(model.BuildStatusRunning))

	require.True(t, model.IsBuildFailedStatus(model.BuildStatusFailed))
	require.True(t, model.IsBuildFailedStatus(model.BuildStatusFailedInternal))
	require.False(t, model.IsBuildFailedStatus(model.BuildStatusCanceled))
	require.False(t, model.IsBuildFailedStatus(model.BuildStatusSuccess))
}

func TestComposeDeploymentTerminalStatusHelper(t *testing.T) {
	require.True(t, model.IsComposeDeploymentTerminalStatus(model.ComposeDeploymentStatusSucceeded))
	require.True(t, model.IsComposeDeploymentTerminalStatus(model.ComposeDeploymentStatusCanceled))
	require.True(t, model.IsComposeDeploymentTerminalStatus(model.ComposeDeploymentStatusFailed))
	require.False(t, model.IsComposeDeploymentTerminalStatus(model.ComposeDeploymentStatusRunning))
	require.False(t, model.IsComposeDeploymentTerminalStatus(model.ComposeDeploymentStatusQueued))
}
