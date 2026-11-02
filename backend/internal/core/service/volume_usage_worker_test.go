package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// fakeVolumeDisk is a volume whose scan visits entries one at a time, each
// costing entryCost of (synctest) time and adding bytesPerEntry.
type fakeVolumeDisk struct {
	entries       int64
	bytesPerEntry int64
	entryCost     time.Duration
	failAtEntry   int64 // fail once when this many entries have been counted (0: never)
}

type volumeScanCall struct {
	volume      string
	at          time.Duration // since the fixture started
	fromEntries int64
}

// volumeUsageFixture drives VolumeUsageWorker.Run against in-memory volume
// rows, a fake filesystem scanner and recorded quota stops.
type volumeUsageFixture struct {
	start time.Time

	mu        sync.Mutex
	cfg       config.SystemConfig
	volumes   []model.Volume
	disks     map[string]*fakeVolumeDisk
	quotaMB   map[uuid.UUID]int64
	calls     []volumeScanCall
	published []string
	stops     []uuid.UUID
	// publishFailures fails that many UpdateUsage calls before succeeding.
	publishFailures int
}

func newVolumeUsageFixture(t *testing.T) *volumeUsageFixture {
	f := &volumeUsageFixture{
		start: time.Now(),
		cfg: config.SystemConfig{
			EventSyncIntervalSeconds:      5,
			VolumeUsageIntervalSeconds:    60,
			VolumeUsageCycleBudgetSeconds: 15,
			VolumeUsageScanEntryBudget:    100,
		},
		disks:   map[string]*fakeVolumeDisk{},
		quotaMB: map[uuid.UUID]int64{},
	}
	return f
}

func (f *volumeUsageFixture) addVolume(id, owner uuid.UUID, storedBytes int64, disk fakeVolumeDisk) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := fmt.Sprintf("vol-%d", len(f.volumes))
	f.volumes = append(f.volumes, model.Volume{ID: id, OwnerID: owner, DockerName: name, Status: model.VolumeStatusAvailable, UsedBytes: storedBytes})
	f.disks[name] = &disk
	return name
}

func (f *volumeUsageFixture) since() time.Duration { return time.Since(f.start) }

func (f *volumeUsageFixture) scan(ctx context.Context, name string, scan model.VolumeUsageScan, maxEntries int) (model.VolumeUsageScan, error) {
	f.mu.Lock()
	disk := f.disks[name]
	f.calls = append(f.calls, volumeScanCall{volume: name, at: f.since(), fromEntries: scan.Entries})
	f.mu.Unlock()
	counted := 0
	for {
		if scan.Entries == disk.entries {
			scan.Complete = true
			return scan, nil
		}
		if maxEntries > 0 && counted == maxEntries {
			return scan, nil
		}
		if disk.failAtEntry > 0 && scan.Entries == disk.failAtEntry {
			disk.failAtEntry = 0
			return scan, fmt.Errorf("lstat %s: transient", name)
		}
		select {
		case <-ctx.Done():
			return scan, ctx.Err()
		case <-time.After(disk.entryCost):
		}
		scan.Entries++
		scan.Bytes += disk.bytesPerEntry
		scan.Cursor = fmt.Sprint(scan.Entries)
		counted++
	}
}

func (f *volumeUsageFixture) worker(t *testing.T) *service.VolumeUsageWorker {
	repo := coremocks.NewVolumeUsageRepo(t)
	repo.EXPECT().GetReconcileCandidates(mock.Anything).RunAndReturn(func(context.Context) ([]model.Volume, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return append([]model.Volume(nil), f.volumes...), nil
	}).Maybe()
	repo.EXPECT().UpdateUsage(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, id uuid.UUID, used int64) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.publishFailures > 0 {
			f.publishFailures--
			return errors.New("db down")
		}
		for i := range f.volumes {
			if f.volumes[i].ID == id {
				f.volumes[i].UsedBytes = used
				f.published = append(f.published, fmt.Sprintf("%s=%d", f.volumes[i].DockerName, used))
			}
		}
		return nil
	}).Maybe()
	repo.EXPECT().GetOwnersWithVolumes(mock.Anything).RunAndReturn(func(context.Context) ([]uuid.UUID, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var owners []uuid.UUID
		for owner := range f.quotaMB {
			owners = append(owners, owner)
		}
		return owners, nil
	}).Maybe()
	repo.EXPECT().GetUserUsedVolumeBytes(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, owner uuid.UUID) (int64, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var used int64
		for _, v := range f.volumes {
			if v.OwnerID == owner {
				used += v.UsedBytes
			}
		}
		return used, nil
	}).Maybe()

	images := coremocks.NewVolumeUsageImageRepo(t)
	images.EXPECT().GetUserUsedDiskSpace(mock.Anything, mock.Anything).Return(0, nil).Maybe()

	users := coremocks.NewUserInfoProvider(t)
	users.EXPECT().GetUser(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, id uuid.UUID) (model.UserInfo, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return model.UserInfo{ID: id, QuotaDiskMB: f.quotaMB[id]}, nil
	}).Maybe()

	conts := coremocks.NewVolumeUsageContainerRepo(t)
	conts.EXPECT().GetRunningWithWritableVolumeMounts(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, owner uuid.UUID) ([]model.Container, error) {
		return []model.Container{{ID: uuid.New(), OwnerID: owner, Status: model.ContainerStatusRunning}}, nil
	}).Maybe()
	conts.EXPECT().HasActiveOperation(mock.Anything, model.ResourceTypeContainer, mock.Anything).Return(false, nil).Maybe()
	conts.EXPECT().QueueContainerOperation(mock.Anything, mock.Anything, model.ContainerStatusStopping, model.ContainerStatusExited, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ uuid.UUID, _, _ string, op model.ResourceOperation, _ model.ContainerLifecycleOutbox) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.stops = append(f.stops, op.OwnerID)
			return nil
		}).Maybe()

	scanner := coremocks.NewVolumeUsageDockerAPI(t)
	scanner.EXPECT().ScanVolumeUsage(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(f.scan).Maybe()

	cfg := coremocks.NewVolumeUsageConfigProvider(t)
	cfg.EXPECT().Get().RunAndReturn(func() config.SystemConfig {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.cfg
	})

	return service.NewVolumeUsageWorker(repo, conts, images, scanner, users, cfg, discardLogger())
}

// run starts the worker inside the current synctest bubble and returns a stop
// function that cancels it and requires Run to return.
func (f *volumeUsageFixture) run(t *testing.T) (stop func()) {
	w := f.worker(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()
	return func() {
		cancel()
		synctest.Wait()
		requireClosed(t, done, "Run returns on cancellation")
	}
}

func (f *volumeUsageFixture) snapshot() (calls []volumeScanCall, published []string, stops []uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]volumeScanCall(nil), f.calls...), append([]string(nil), f.published...), append([]uuid.UUID(nil), f.stops...)
}

func (f *volumeUsageFixture) scanCount() int {
	calls, _, _ := f.snapshot()
	return len(calls)
}

func TestVolumeUsageWorkerRunsOnItsOwnCadence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		f.addVolume(uuid.New(), uuid.New(), 0, fakeVolumeDisk{entries: 1, bytesPerEntry: 1})
		stop := f.run(t)

		synctest.Wait()
		require.Equal(t, 1, f.scanCount(), "account immediately on start")
		time.Sleep(59 * time.Second)
		synctest.Wait()
		require.Equal(t, 1, f.scanCount(), "event sync interval does not drive accounting")
		time.Sleep(time.Second)
		synctest.Wait()
		require.Equal(t, 2, f.scanCount())

		stop()
	})
}

func TestVolumeUsageWorkerPublishesOnlyCompleteScansAndResumesPartialOnes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		f.cfg.VolumeUsageScanEntryBudget = 2
		name := f.addVolume(uuid.New(), uuid.New(), 7, fakeVolumeDisk{entries: 5, bytesPerEntry: 100})
		stop := f.run(t)

		synctest.Wait()
		_, published, _ := f.snapshot()
		require.Empty(t, published, "a partial scan is not a measurement")
		time.Sleep(60 * time.Second)
		synctest.Wait()
		_, published, _ = f.snapshot()
		require.Empty(t, published)
		time.Sleep(60 * time.Second)
		synctest.Wait()
		calls, published, _ := f.snapshot()
		require.Equal(t, []string{name + "=500"}, published)
		require.Equal(t, []int64{0, 2, 4}, fromEntries(calls), "each cycle resumes where the last stopped")

		time.Sleep(60 * time.Second)
		synctest.Wait()
		calls, published, _ = f.snapshot()
		require.Equal(t, []string{name + "=500"}, published)
		require.Equal(t, int64(0), calls[len(calls)-1].fromEntries, "a completed volume starts a fresh pass")

		stop()
	})
}

func fromEntries(calls []volumeScanCall) []int64 {
	out := make([]int64, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.fromEntries)
	}
	return out
}

func TestVolumeUsageWorkerBoundsEachCycleAndRotatesFairly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		f.cfg.VolumeUsageScanEntryBudget = 5 // 10s of scanning per volume slice
		f.cfg.VolumeUsageCycleBudgetSeconds = 15
		disk := fakeVolumeDisk{entries: 20, bytesPerEntry: 100, entryCost: 2 * time.Second}
		owner := uuid.New()
		// Rows arrive out of ID order; rotation follows volume ID.
		b := f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000002"), owner, 0, disk)
		c := f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000003"), owner, 0, disk)
		a := f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000001"), owner, 0, disk)
		stop := f.run(t)

		synctest.Wait()
		time.Sleep(3*75*time.Second - time.Second)
		synctest.Wait()
		calls, published, _ := f.snapshot()
		require.Equal(t, []volumeScanCall{
			{volume: a, at: 0, fromEntries: 0},
			{volume: b, at: 10 * time.Second, fromEntries: 0}, // interrupted by the cycle deadline at 15s
			// Next cycle, 60s after the bounded one: the interrupted b leads with
			// a whole budget, then c is interrupted after 5s.
			{volume: b, at: 75 * time.Second, fromEntries: 2},
			{volume: c, at: 85 * time.Second, fromEntries: 0},
			{volume: c, at: 150 * time.Second, fromEntries: 2},
			{volume: a, at: 160 * time.Second, fromEntries: 5},
		}, calls)
		require.Empty(t, published)

		// 120s of scanning in 15s cycles completes every volume.
		time.Sleep(12 * 75 * time.Second)
		synctest.Wait()
		_, published, _ = f.snapshot()
		require.Subset(t, published, []string{a + "=2000", b + "=2000", c + "=2000"})
		for _, p := range published {
			require.Contains(t, []string{a + "=2000", b + "=2000", c + "=2000"}, p, "only complete measurements are published")
		}

		stop()
	})
}

func TestVolumeUsageWorkerEnforcesQuotaOnUnfinishedScanLowerBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		f.cfg.VolumeUsageScanEntryBudget = 12
		const mb = 1 << 20
		over, under, rescanning := uuid.New(), uuid.New(), uuid.New()
		f.quotaMB[over] = 10
		f.quotaMB[under] = 10
		f.quotaMB[rescanning] = 10
		// Last complete measurements are 1 MB, but the scans find more.
		f.addVolume(uuid.New(), over, 1*mb, fakeVolumeDisk{entries: 30, bytesPerEntry: mb})
		f.addVolume(uuid.New(), under, 1*mb, fakeVolumeDisk{entries: 30, bytesPerEntry: mb / 2})
		// Early in a re-scan, the last complete measurement (12 MB) still governs.
		f.addVolume(uuid.New(), rescanning, 12*mb, fakeVolumeDisk{entries: 30, bytesPerEntry: mb / 12})
		stop := f.run(t)

		synctest.Wait()
		_, published, stops := f.snapshot()
		require.Empty(t, published, "the lower bound is not published")
		require.ElementsMatch(t, []uuid.UUID{over, rescanning}, stops, "12 MB already counted or last measured exceeds the 10 MB quota; 6 MB does not")

		stop()
	})
}

func TestVolumeUsageWorkerDoesNotCountCompletedScanTwice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		f.cfg.VolumeUsageScanEntryBudget = 5
		const mb = 1 << 20
		owner := uuid.New()
		f.quotaMB[owner] = 10
		name := f.addVolume(uuid.New(), owner, 0, fakeVolumeDisk{entries: 9, bytesPerEntry: mb})
		stop := f.run(t)

		synctest.Wait()
		time.Sleep(60 * time.Second)
		synctest.Wait()
		_, published, stops := f.snapshot()
		require.Equal(t, []string{fmt.Sprintf("%s=%d", name, 9*mb)}, published)
		require.Empty(t, stops, "9 MB is under the 10 MB quota once the scan is published")

		stop()
	})
}

func TestVolumeUsageWorkerRetriesFailedScanFromLastCountedEntry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		name := f.addVolume(uuid.New(), uuid.New(), 0, fakeVolumeDisk{entries: 6, bytesPerEntry: 10, failAtEntry: 4})
		stop := f.run(t)

		synctest.Wait()
		time.Sleep(60 * time.Second)
		synctest.Wait()
		calls, published, _ := f.snapshot()
		require.Equal(t, []int64{0, 4}, fromEntries(calls))
		require.Equal(t, []string{name + "=60"}, published)

		stop()
	})
}

func TestVolumeUsageWorkerStopsMidScanWithoutPublishingOrEnforcing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		owner := uuid.New()
		f.quotaMB[owner] = 1
		f.addVolume(uuid.New(), owner, 0, fakeVolumeDisk{entries: 10, bytesPerEntry: 1 << 20, entryCost: time.Second})
		stop := f.run(t)

		time.Sleep(5500 * time.Millisecond)
		stop()

		calls, published, stops := f.snapshot()
		require.Len(t, calls, 1)
		require.Empty(t, published)
		require.Empty(t, stops, "no quota decisions with a canceled context")
	})
}

func TestVolumeUsageWorkerGivesInterruptedVolumeTheNextCycleHeadStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		f.cfg.VolumeUsageCycleBudgetSeconds = 15
		owner := uuid.New()
		// b completes a fresh 14s pass every cycle it starts, leaving a 1s
		// remainder, shorter than one of a's 2s entries.
		a := f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000001"), owner, 0, fakeVolumeDisk{entries: 30, bytesPerEntry: 1, entryCost: 2 * time.Second})
		f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000002"), owner, 0, fakeVolumeDisk{entries: 7, bytesPerEntry: 1, entryCost: 2 * time.Second})
		stop := f.run(t)

		synctest.Wait()
		time.Sleep(12 * 75 * time.Second)
		synctest.Wait()
		_, published, _ := f.snapshot()
		require.Contains(t, published, a+"=30", "the long volume is not starved by the remainder")

		stop()
	})
}

func TestVolumeUsageWorkerPassesVolumeInterruptedWhileLeading(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		f.cfg.VolumeUsageCycleBudgetSeconds = 15
		owner := uuid.New()
		// a's 100-entry slice never fits in one cycle.
		f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000001"), owner, 0, fakeVolumeDisk{entries: 1000, bytesPerEntry: 1, entryCost: time.Second})
		b := f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000002"), owner, 0, fakeVolumeDisk{entries: 3, bytesPerEntry: 1, entryCost: time.Second})
		stop := f.run(t)

		synctest.Wait()
		time.Sleep(80 * time.Second) // the next cycle starts at 75s and b finishes at 78s
		synctest.Wait()
		_, published, _ := f.snapshot()
		require.Equal(t, []string{b + "=3"}, published)

		stop()
	})
}

func TestVolumeUsageWorkerKeepsCompleteScanWhenPublishFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		const mb = 1 << 20
		owner := uuid.New()
		f.quotaMB[owner] = 10
		f.publishFailures = 1
		name := f.addVolume(uuid.New(), owner, 1*mb, fakeVolumeDisk{entries: 12, bytesPerEntry: mb})
		stop := f.run(t)

		synctest.Wait()
		_, published, stops := f.snapshot()
		require.Empty(t, published)
		require.Equal(t, []uuid.UUID{owner}, stops, "the unpublished 12 MB still counts toward the quota")

		time.Sleep(60 * time.Second)
		synctest.Wait()
		calls, published, _ := f.snapshot()
		require.Equal(t, []string{fmt.Sprintf("%s=%d", name, 12*mb)}, published)
		require.Equal(t, int64(12), calls[1].fromEntries, "re-published without rescanning")

		stop()
	})
}
