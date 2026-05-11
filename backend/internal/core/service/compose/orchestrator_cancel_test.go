package compose

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

func TestWaitForBuildsAbortsOnCanceledBuild(t *testing.T) {
	canceledBuildID := uuid.New()
	siblingBuildID := uuid.New()
	builder := &orchestratorBuilderClientFake{}
	o := &Orchestrator{
		buildRepo: &orchestratorBuildRepoFake{builds: map[uuid.UUID]model.Build{
			canceledBuildID: {ID: canceledBuildID, Status: model.BuildStatusCanceled},
			siblingBuildID:  {ID: siblingBuildID, Status: model.BuildStatusRunning},
		}},
		builderClient: builder,
		cfg:           orchestratorConfigFake{},
	}

	err := o.waitForBuilds(context.Background(), nil, []uuid.UUID{canceledBuildID, siblingBuildID})
	if !errors.Is(err, errComposeDeploymentCanceled) {
		t.Fatalf("waitForBuilds error = %v, want compose cancellation", err)
	}
	if len(builder.canceled) != 2 {
		t.Fatalf("canceled builds = %v, want both builds canceled", builder.canceled)
	}
}

func TestWaitForBuildsAbortsOnMissingBuild(t *testing.T) {
	buildID := uuid.New()
	builder := &orchestratorBuilderClientFake{}
	o := &Orchestrator{
		buildRepo:     &orchestratorBuildRepoFake{err: apperrors.ErrNotFound},
		builderClient: builder,
		cfg:           orchestratorConfigFake{},
	}

	err := o.waitForBuilds(context.Background(), nil, []uuid.UUID{buildID})
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("waitForBuilds error = %v, want not found", err)
	}
	if len(builder.canceled) != 1 || builder.canceled[0] != buildID {
		t.Fatalf("canceled builds = %v, want missing build canceled", builder.canceled)
	}
}

type orchestratorBuildRepoFake struct {
	builds map[uuid.UUID]model.Build
	err    error
}

func (f *orchestratorBuildRepoFake) GetByID(ctx context.Context, id uuid.UUID) (model.Build, error) {
	if f.err != nil {
		return model.Build{}, f.err
	}
	build, ok := f.builds[id]
	if !ok {
		return model.Build{}, apperrors.ErrNotFound
	}
	return build, nil
}

func (f *orchestratorBuildRepoFake) GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Build, error) {
	if f.err != nil {
		return nil, f.err
	}
	builds := make([]model.Build, 0, len(f.builds))
	for _, build := range f.builds {
		if build.ProjectID != nil && *build.ProjectID == projectID {
			builds = append(builds, build)
		}
	}
	return builds, nil
}

type orchestratorBuilderClientFake struct {
	canceled []uuid.UUID
}

func (f *orchestratorBuilderClientFake) TriggerBuild(ctx context.Context, projectID uuid.UUID, srv model.ComposeService, sourceObjectKey string) (uuid.UUID, error) {
	return uuid.Nil, nil
}

func (f *orchestratorBuilderClientFake) CancelBuild(ctx context.Context, buildID uuid.UUID) error {
	f.canceled = append(f.canceled, buildID)
	return nil
}

type orchestratorConfigFake struct{}

func (orchestratorConfigFake) Get() config.SystemConfig {
	return config.SystemConfig{ComposeBuildPollIntervalSeconds: 1}
}
