package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
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
// rows, a fake filesystem scanner and recorded container stops.
type volumeUsageFixture struct {
	start time.Time

	mu      sync.Mutex
	cfg     config.SystemConfig
	volumes []model.Volume
	disks   map[string]*fakeVolumeDisk
	// writers are the running containers mounting each volume writable.
	writers map[uuid.UUID][]model.Container
	// busy containers have an active operation; failing ones fail to stop.
	busy, failing map[uuid.UUID]bool
	calls         []volumeScanCall
	published     []string
	// stops lists stopped container IDs in order, reasons their reasons.
	stops   []uuid.UUID
	reasons map[uuid.UUID]string
	// publishFailures fails that many UpdateUsage calls before succeeding.
	publishFailures int
}

// unlimitedSize is a volume size no test fills.
const unlimitedSize = int64(1) << 50

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
		writers: map[uuid.UUID][]model.Container{},
		busy:    map[uuid.UUID]bool{},
		failing: map[uuid.UUID]bool{},
		reasons: map[uuid.UUID]string{},
	}
	return f
}

func (f *volumeUsageFixture) addVolume(id, owner uuid.UUID, sizeBytes, storedBytes int64, disk fakeVolumeDisk) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := fmt.Sprintf("vol-%d", len(f.volumes))
	f.volumes = append(f.volumes, model.Volume{ID: id, OwnerID: owner, DockerName: name, Status: model.VolumeStatusAvailable, UsedBytes: storedBytes, SizeBytes: sizeBytes})
	f.disks[name] = &disk
	return name
}

// addWriter records a running container that mounts volumeID writable.
func (f *volumeUsageFixture) addWriter(volumeID uuid.UUID) uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := model.Container{ID: uuid.New(), OwnerID: uuid.New(), Status: model.ContainerStatusRunning}
	f.writers[volumeID] = append(f.writers[volumeID], c)
	return c.ID
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
	conts := coremocks.NewVolumeUsageContainerRepo(t)
	conts.EXPECT().ListRunningWritersOfVolume(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, volumeID uuid.UUID) ([]model.Container, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return append([]model.Container(nil), f.writers[volumeID]...), nil
	}).Maybe()
	conts.EXPECT().HasActiveOperation(mock.Anything, model.ResourceTypeContainer, mock.Anything).RunAndReturn(func(_ context.Context, _ string, id uuid.UUID) (bool, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.busy[id], nil
	}).Maybe()
	conts.EXPECT().QueueContainerOperation(mock.Anything, mock.Anything, model.ContainerStatusStopping, model.ContainerStatusExited, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, id uuid.UUID, _, _ string, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.failing[id] {
				return errors.New("db down")
			}
			var msg queue.ContainerLifecycleMessage
			if err := json.Unmarshal(outbox.Payload, &msg); err != nil {
				return err
			}
			f.stops = append(f.stops, id)
			f.reasons[id] = msg.Reason
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

	return service.NewVolumeUsageWorker(repo, conts, scanner, cfg, discardLogger())
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
		f.addVolume(uuid.New(), uuid.New(), unlimitedSize, 0, fakeVolumeDisk{entries: 1, bytesPerEntry: 1})
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
		name := f.addVolume(uuid.New(), uuid.New(), unlimitedSize, 7, fakeVolumeDisk{entries: 5, bytesPerEntry: 100})
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
		b := f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000002"), owner, unlimitedSize, 0, disk)
		c := f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000003"), owner, unlimitedSize, 0, disk)
		a := f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000001"), owner, unlimitedSize, 0, disk)
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

func TestVolumeUsageWorkerStopsOnUnfinishedScanLowerBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		f.cfg.VolumeUsageScanEntryBudget = 12
		const mb = 1 << 20
		owner := uuid.New()
		over, under, rescanning := uuid.New(), uuid.New(), uuid.New()
		// Last complete measurements are 1 MB, but the scans find more.
		f.addVolume(over, owner, 10*mb, 1*mb, fakeVolumeDisk{entries: 30, bytesPerEntry: mb})
		f.addVolume(under, owner, 10*mb, 1*mb, fakeVolumeDisk{entries: 30, bytesPerEntry: mb / 2})
		// Early in a re-scan, the last complete measurement (12 MB) still governs.
		f.addVolume(rescanning, owner, 10*mb, 12*mb, fakeVolumeDisk{entries: 30, bytesPerEntry: mb / 12})
		overWriter, rescanningWriter := f.addWriter(over), f.addWriter(rescanning)
		f.addWriter(under)
		stop := f.run(t)

		synctest.Wait()
		_, published, stops := f.snapshot()
		require.Empty(t, published, "the lower bound is not published")
		require.ElementsMatch(t, []uuid.UUID{overWriter, rescanningWriter}, stops, "12 MB already counted or last measured exceeds a 10 MB size; 6 MB does not")

		stop()
	})
}

func TestVolumeUsageWorkerDoesNotCountCompletedScanTwice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		f.cfg.VolumeUsageScanEntryBudget = 5
		const mb = 1 << 20
		volumeID := uuid.New()
		name := f.addVolume(volumeID, uuid.New(), 10*mb, 0, fakeVolumeDisk{entries: 9, bytesPerEntry: mb})
		f.addWriter(volumeID)
		stop := f.run(t)

		synctest.Wait()
		time.Sleep(60 * time.Second)
		synctest.Wait()
		_, published, stops := f.snapshot()
		require.Equal(t, []string{fmt.Sprintf("%s=%d", name, 9*mb)}, published)
		require.Empty(t, stops, "9 MB is within the 10 MB size once the scan is published")

		stop()
	})
}

// One pass stops only the running containers that can write to a volume
// grown beyond its size, each with the volume size reason. A container with
// an active operation is passed over, and a failed stop does not end the pass.
// The store lists only writable mounts of running containers, so read-only
// mounts and stopped containers never reach the worker.
func TestVolumeUsageWorkerStopsOnlyWritersOfOversizedVolume(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		const mb = 1 << 20
		owner := uuid.New()
		oversized, alsoOversized, within := uuid.New(), uuid.New(), uuid.New()
		f.addVolume(oversized, owner, 2*mb, 0, fakeVolumeDisk{entries: 3, bytesPerEntry: mb})
		f.addVolume(alsoOversized, owner, mb, 0, fakeVolumeDisk{entries: 2, bytesPerEntry: mb})
		f.addVolume(within, owner, 10*mb, 0, fakeVolumeDisk{entries: 9, bytesPerEntry: mb})
		failingWriter := f.addWriter(oversized)
		busyWriter := f.addWriter(oversized)
		stoppedWriter := f.addWriter(oversized)
		otherStoppedWriter := f.addWriter(alsoOversized)
		f.addWriter(within)
		f.failing[failingWriter] = true
		f.busy[busyWriter] = true
		stop := f.run(t)

		synctest.Wait()
		_, _, stops := f.snapshot()
		require.ElementsMatch(t, []uuid.UUID{stoppedWriter, otherStoppedWriter}, stops)
		f.mu.Lock()
		for _, id := range stops {
			require.Equal(t, service.VolumeSizeStopReason, f.reasons[id])
		}
		f.mu.Unlock()

		stop()
	})
}

// Volumes are checked one by one against their own sizes: an owner whose
// volumes are each within size loses no container, however much they hold in
// total.
func TestVolumeUsageWorkerIgnoresTotalWhenEveryVolumeFits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		const gb = 1 << 30
		owner := uuid.New()
		for range 3 {
			id := uuid.New()
			f.addVolume(id, owner, 500*gb, 400*gb, fakeVolumeDisk{entries: 4, bytesPerEntry: 100 * gb})
			f.addWriter(id)
		}
		stop := f.run(t)

		synctest.Wait()
		_, published, stops := f.snapshot()
		require.Len(t, published, 3)
		require.Empty(t, stops)

		stop()
	})
}

func TestVolumeUsageWorkerRetriesFailedScanFromLastCountedEntry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		name := f.addVolume(uuid.New(), uuid.New(), unlimitedSize, 0, fakeVolumeDisk{entries: 6, bytesPerEntry: 10, failAtEntry: 4})
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
		volumeID := uuid.New()
		f.addVolume(volumeID, uuid.New(), 1<<20, 0, fakeVolumeDisk{entries: 10, bytesPerEntry: 1 << 20, entryCost: time.Second})
		f.addWriter(volumeID)
		stop := f.run(t)

		time.Sleep(5500 * time.Millisecond)
		stop()

		calls, published, stops := f.snapshot()
		require.Len(t, calls, 1)
		require.Empty(t, published)
		require.Empty(t, stops, "no size decisions with a canceled context")
	})
}

func TestVolumeUsageWorkerGivesInterruptedVolumeTheNextCycleHeadStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newVolumeUsageFixture(t)
		f.cfg.VolumeUsageCycleBudgetSeconds = 15
		owner := uuid.New()
		// b completes a fresh 14s pass every cycle it starts, leaving a 1s
		// remainder, shorter than one of a's 2s entries.
		a := f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000001"), owner, unlimitedSize, 0, fakeVolumeDisk{entries: 30, bytesPerEntry: 1, entryCost: 2 * time.Second})
		f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000002"), owner, unlimitedSize, 0, fakeVolumeDisk{entries: 7, bytesPerEntry: 1, entryCost: 2 * time.Second})
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
		f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000001"), owner, unlimitedSize, 0, fakeVolumeDisk{entries: 1000, bytesPerEntry: 1, entryCost: time.Second})
		b := f.addVolume(uuid.MustParse("00000000-0000-0000-0000-000000000002"), owner, unlimitedSize, 0, fakeVolumeDisk{entries: 3, bytesPerEntry: 1, entryCost: time.Second})
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
		volumeID := uuid.New()
		f.publishFailures = 1
		name := f.addVolume(volumeID, uuid.New(), 10*mb, 1*mb, fakeVolumeDisk{entries: 12, bytesPerEntry: mb})
		writer := f.addWriter(volumeID)
		stop := f.run(t)

		synctest.Wait()
		_, published, stops := f.snapshot()
		require.Empty(t, published)
		require.Equal(t, []uuid.UUID{writer}, stops, "the unpublished 12 MB still exceeds the 10 MB size")

		time.Sleep(60 * time.Second)
		synctest.Wait()
		calls, published, _ := f.snapshot()
		require.Equal(t, []string{fmt.Sprintf("%s=%d", name, 12*mb)}, published)
		require.Equal(t, int64(12), calls[1].fromEntries, "re-published without rescanning")

		stop()
	})
}
