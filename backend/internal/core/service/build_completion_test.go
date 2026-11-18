package service_test

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const completionImageBytes = int64(600 << 20)

// completionWorld scripts one owner's completing builds. The repositories are
// scripted mocks; the outcome each completion writes is captured per build.
type completionWorld struct {
	owner    uuid.UUID
	lock     *coretest.ContainerStoreFake
	repo     *coremocks.BuildRepository
	images   *coremocks.BuildImageRepository
	registry *coremocks.ImageRegistryAPI

	mu       sync.Mutex
	outcomes map[uuid.UUID]string
	// onQuotaRead, when set, runs at each effective quota read.
	onQuotaRead func()
}

// observedQuotas runs onRead at each effective quota read of the production
// quota service it wraps.
type observedQuotas struct {
	service.UserQuotaProvider
	onRead func()
}

func (q observedQuotas) EffectiveUserQuota(ctx context.Context, ownerID uuid.UUID) (model.UserQuota, error) {
	q.onRead()
	return q.UserQuotaProvider.EffectiveUserQuota(ctx, ownerID)
}

func newCompletionWorld(t *testing.T) *completionWorld {
	t.Helper()
	return &completionWorld{
		owner:    uuid.New(),
		lock:     coretest.NewContainerStoreFake(),
		repo:     newBuildRepository(t),
		images:   coremocks.NewBuildImageRepository(t),
		registry: coremocks.NewImageRegistryAPI(t),
		outcomes: map[uuid.UUID]string{},
	}
}

// addBuild records a running build whose pushed image is completionImageBytes.
func (w *completionWorld) addBuild(tag string) model.Build {
	b := model.Build{ID: uuid.New(), ImageID: uuid.New(), OwnerID: w.owner, Status: model.BuildStatusRunning}
	w.repo.EXPECT().GetByID(mock.Anything, b.ID).Return(b, nil).Maybe()
	w.images.EXPECT().GetByID(mock.Anything, b.ImageID).
		Return(model.Image{ID: b.ImageID, OwnerID: w.owner, Tag: tag, RegistryTag: b.ImageID.String()}, nil).Maybe()
	w.registry.EXPECT().GetImageSizeAndDigest(mock.Anything, mock.Anything, b.ImageID.String()).
		Return(completionImageBytes, "sha256:digest", nil).Maybe()
	return b
}

// captureOutcomes records the status each completion writes.
func (w *completionWorld) captureOutcomes() {
	w.images.EXPECT().UpdateBuildAndImageSizeTx(mock.Anything, mock.Anything, mock.Anything, model.BuildStatusSuccess, completionImageBytes, mock.Anything).
		RunAndReturn(func(_ context.Context, buildID, _ uuid.UUID, status string, _ int64, _ *string) ([]model.Image, error) {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.outcomes[buildID] = status
			return nil, nil
		}).Maybe()
	w.images.EXPECT().MarkBuildFailedAndDeleteImageTx(mock.Anything, mock.Anything, mock.Anything, model.BuildStatusFailedQuotaExceeded).
		RunAndReturn(func(_ context.Context, buildID, _ uuid.UUID, status string) error {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.outcomes[buildID] = status
			return nil
		}).Maybe()
}

func (w *completionWorld) service(t *testing.T, deps service.BuildServiceDeps) *service.BuildService {
	t.Helper()
	deps.Repository = w.repo
	deps.Images = w.images
	deps.Registry = w.registry
	deps.CapacityLock = w.lock
	// 1000 MB holds one 600 MiB image and not two.
	var quotas service.UserQuotaProvider = testQuotas(t, nil, map[uuid.UUID]model.UserQuota{w.owner: {CPUMillicores: 1000, RAMMB: 1024, DiskMB: 1000}})
	if w.onQuotaRead != nil {
		quotas = observedQuotas{UserQuotaProvider: quotas, onRead: w.onQuotaRead}
	}
	deps.Quotas = quotas
	return newTestBuildService(t, deps)
}

// lockFree reports whether the owner capacity lock can be taken now. Inside a
// synctest bubble a held lock times out without real waiting.
func (w *completionWorld) lockFree() bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	unlock, err := w.lock.AcquireOwnerCapacityLock(ctx, w.owner)
	if err != nil {
		return false
	}
	unlock()
	return true
}

// Two completions that each fit the quota alone but not together are
// serialised by the owner capacity lock: the second reads reserved disk only
// after the first recorded its image, and ends quota-exceeded.
func TestConcurrentBuildCompletionsCannotBothTakeTheLastOfTheDiskQuota(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newCompletionWorld(t)
		first := w.addBuild("first:latest")
		second := w.addBuild("second:latest")
		w.captureOutcomes()
		w.images.EXPECT().GetReplacementImageBytes(mock.Anything, w.owner, mock.Anything, mock.Anything).Return(int64(0), nil).Maybe()

		// The first reserved disk read sees no image and waits on the gate;
		// a read after the first image is recorded sees it.
		gate := make(chan struct{})
		reads := 0
		w.images.EXPECT().GetUserImageBytes(mock.Anything, w.owner).RunAndReturn(func(context.Context, uuid.UUID) (int64, error) {
			w.mu.Lock()
			reads++
			firstRead := reads == 1
			w.mu.Unlock()
			if firstRead {
				<-gate
				return 0, nil
			}
			return completionImageBytes, nil
		}).Times(2)
		svc := w.service(t, service.BuildServiceDeps{})

		errs := make(chan error, 2)
		complete := func(b model.Build) {
			errs <- svc.CompleteBuildRecord(context.Background(), b.ID, b.ImageID, model.BuildStatusSuccess, 0)
		}
		go complete(first)
		synctest.Wait()
		go complete(second)
		synctest.Wait()
		w.mu.Lock()
		require.Equal(t, 1, reads, "the second completion waits for the lock the first holds")
		w.mu.Unlock()

		close(gate)
		require.NoError(t, <-errs)
		require.NoError(t, <-errs)

		w.mu.Lock()
		defer w.mu.Unlock()
		require.Equal(t, map[uuid.UUID]string{
			first.ID:  model.BuildStatusSuccess,
			second.ID: model.BuildStatusFailedQuotaExceeded,
		}, w.outcomes)
	})
}

// A rebuild of a tag the owner already has replaces that image, so its size
// does not count against the quota the rebuilt image needs.
func TestBuildCompletionSubtractsTheImageARebuildReplaces(t *testing.T) {
	w := newCompletionWorld(t)
	rebuild := w.addBuild("app:latest")
	w.captureOutcomes()
	w.images.EXPECT().GetUserImageBytes(mock.Anything, w.owner).Return(completionImageBytes, nil)
	w.images.EXPECT().GetReplacementImageBytes(mock.Anything, w.owner, "app:latest", rebuild.ImageID).Return(completionImageBytes, nil)
	svc := w.service(t, service.BuildServiceDeps{})

	require.NoError(t, svc.CompleteBuildRecord(context.Background(), rebuild.ID, rebuild.ImageID, model.BuildStatusSuccess, 0))
	require.Equal(t, map[uuid.UUID]string{rebuild.ID: model.BuildStatusSuccess}, w.outcomes)
}

// The lock spans the quota decision and its write only: the Registry read
// comes before it, and revocation, archive cleanup, replaced image removal
// and the Compose cascade after it.
func TestBuildCompletionHoldsTheCapacityLockOnlyForTheQuotaDecision(t *testing.T) {
	for _, tc := range []struct {
		name      string
		otherDisk int64
		outcome   string
	}{
		{"success", 0, model.BuildStatusSuccess},
		{"quota exceeded", completionImageBytes, model.BuildStatusFailedQuotaExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				w := newCompletionWorld(t)
				projectID := uuid.New()
				b := model.Build{
					ID: uuid.New(), ImageID: uuid.New(), OwnerID: w.owner, ProjectID: &projectID,
					Status: model.BuildStatusRunning, ArchiveObjectKey: "build-archives/source.zip",
				}
				held := map[string]bool{}
				var mu sync.Mutex
				note := func(step string) {
					free := w.lockFree()
					mu.Lock()
					held[step] = !free
					mu.Unlock()
				}
				w.onQuotaRead = func() { note("quota read") }

				w.repo.EXPECT().GetByID(mock.Anything, b.ID).Return(b, nil)
				w.images.EXPECT().GetByID(mock.Anything, b.ImageID).
					Return(model.Image{ID: b.ImageID, OwnerID: w.owner, Tag: "app:latest", RegistryTag: b.ImageID.String()}, nil)
				w.registry.EXPECT().GetImageSizeAndDigest(mock.Anything, mock.Anything, b.ImageID.String()).
					RunAndReturn(func(context.Context, string, string) (int64, string, error) {
						note("registry read")
						return completionImageBytes, "sha256:digest", nil
					})
				w.images.EXPECT().GetUserImageBytes(mock.Anything, w.owner).RunAndReturn(func(context.Context, uuid.UUID) (int64, error) {
					note("reserved disk read")
					return tc.otherDisk, nil
				})
				w.images.EXPECT().GetReplacementImageBytes(mock.Anything, w.owner, "app:latest", b.ImageID).
					RunAndReturn(func(context.Context, uuid.UUID, string, uuid.UUID) (int64, error) {
						note("replaced image read")
						return 0, nil
					})
				w.images.EXPECT().UpdateBuildAndImageSizeTx(mock.Anything, b.ID, b.ImageID, model.BuildStatusSuccess, completionImageBytes, mock.Anything).
					RunAndReturn(func(context.Context, uuid.UUID, uuid.UUID, string, int64, *string) ([]model.Image, error) {
						note("outcome write")
						return []model.Image{{ID: uuid.New(), OwnerID: w.owner, Tag: "app:latest", RegistryTag: "old"}}, nil
					}).Maybe()
				w.images.EXPECT().MarkBuildFailedAndDeleteImageTx(mock.Anything, b.ID, b.ImageID, model.BuildStatusFailedQuotaExceeded).
					RunAndReturn(func(context.Context, uuid.UUID, uuid.UUID, string) error {
						note("outcome write")
						return nil
					}).Maybe()

				auth := coremocks.NewBuildRegistryAuthorizer(t)
				auth.EXPECT().RevokeBuild(mock.Anything, b.ID, mock.Anything).
					RunAndReturn(func(_ context.Context, _ uuid.UUID, tag model.RegistryTagDisposition) error {
						if tag == model.RegistryTagDelete {
							note("registry revocation")
						}
						return nil
					})
				objects := coremocks.NewBuildObjectStore(t)
				objects.EXPECT().DeleteObject(mock.Anything, "build-archives/source.zip").
					RunAndReturn(func(context.Context, string) error {
						note("archive cleanup")
						return nil
					})
				docker := coremocks.NewBuildImageCache(t)
				docker.EXPECT().RemoveImage(mock.Anything, mock.Anything, false).
					RunAndReturn(func(context.Context, string, bool) error {
						note("replaced image removal")
						return nil
					}).Maybe()
				deployments := coremocks.NewBuildDeploymentCanceler(t)
				deployments.EXPECT().CancelDeploymentForBuild(mock.Anything, projectID, b.ID).
					RunAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
						note("compose cascade")
						return nil
					}).Maybe()

				svc := w.service(t, service.BuildServiceDeps{
					RegistryAuth: auth, ObjectStore: objects, Docker: docker, Deployments: deployments,
				})
				require.NoError(t, svc.CompleteBuildRecord(context.Background(), b.ID, b.ImageID, model.BuildStatusSuccess, 0))

				want := map[string]bool{
					"registry read":       false,
					"quota read":          true,
					"reserved disk read":  true,
					"replaced image read": true,
					"outcome write":       true,
					"archive cleanup":     false,
				}
				if tc.outcome == model.BuildStatusSuccess {
					want["replaced image removal"] = false
				} else {
					want["registry revocation"] = false
					want["compose cascade"] = false
				}
				require.Equal(t, want, held)
			})
		})
	}
}
