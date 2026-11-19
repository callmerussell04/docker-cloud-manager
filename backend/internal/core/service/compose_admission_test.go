package service_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
)

// The Compose plan admission is tested through Compose ingestion, in the
// compose package's admission table; here only its construction is pinned.
func TestComposePlanAdmissionRejectsMissingDependencies(t *testing.T) {
	depstest.RequireRejectsMissing(t, composePlanAdmissionDeps, service.NewComposePlanAdmission, map[string]string{
		"Containers":  "containers",
		"Volumes":     "volumes",
		"Builds":      "builds",
		"Images":      "images",
		"Deployments": "deployments",
		"Quotas":      "quotas",
		"HostMetrics": "host metrics",
		"DiskMetrics": "disk metrics",
		"Config":      "config",
	})
}

// composePlanAdmissionDeps are the production repositories, which the
// construction never calls.
func composePlanAdmissionDeps(t *testing.T) service.ComposePlanAdmissionDeps {
	t.Helper()
	return service.ComposePlanAdmissionDeps{
		Containers:  &repository.ContainerRepository{},
		Volumes:     &repository.VolumeRepository{},
		Builds:      &repository.BuildRepository{},
		Images:      &repository.ImageRepository{},
		Deployments: &repository.ProjectRepository{},
		Quotas:      testQuotas(t, nil, nil),
		HostMetrics: coremocks.NewHostMetricsProvider(t),
		DiskMetrics: coremocks.NewHostDiskMetricsProvider(t),
		Config:      testConfig(),
	}
}
