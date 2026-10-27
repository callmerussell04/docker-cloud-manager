package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	outboxTestBatchSize = 7
	outboxTestExchange  = "ex"
)

var (
	errOutboxLookup  = errors.New("db down")
	errOutboxPublish = errors.New("broker down")
	errOutboxMark    = errors.New("status write failed")
)

// outboxAggregate is the state of the aggregate a leased outbox record points at.
type outboxAggregate int

const (
	outboxAggregateActive outboxAggregate = iota
	outboxAggregateTerminal
	outboxAggregateMissing
	outboxAggregateUnavailable
	// outboxAggregateCanceledLookup fails the lookup with the context's error, as a database driver would.
	outboxAggregateCanceledLookup
	// The container queue additionally requires the container record.
	outboxContainerMissing
	outboxContainerUnavailable
)

type outboxMark int

const (
	outboxMarkPublished outboxMark = iota
	outboxMarkPending
	outboxMarkDiscarded
)

// outboxQueueFixture adapts one Core outbox queue class to the shared scenarios.
// It records publications and status marks in call order; lookups are verified
// by the strict mocks.
type outboxQueueFixture struct {
	publisher *coremocks.BuildQueuePublisher
	interval  atomic.Int64
	leases    atomic.Int32
	outboxIDs []uuid.UUID
	targetIDs []uuid.UUID

	mu     sync.Mutex
	events []string

	expectLease     func(records int, err error)
	expectAggregate func(i int, state outboxAggregate)
	expectMark      func(mark outboxMark, i int, err error)
	run             func(ctx context.Context)
}

type outboxQueueClass struct {
	name       string
	newFixture func(t *testing.T) *outboxQueueFixture
}

var outboxQueueClasses = []outboxQueueClass{
	{name: "build", newFixture: newBuildOutboxFixture},
	{name: "compose", newFixture: newComposeOutboxFixture},
	{name: "container", newFixture: newContainerOutboxFixture},
	{name: "resource", newFixture: newResourceOutboxFixture},
}

func TestOutboxWorkersApplyAggregatePolicyAndPublication(t *testing.T) {
	scenarios := []struct {
		name    string
		arrange func(f *outboxQueueFixture)
		want    []string
	}{
		{
			name: "active aggregate is published before it is marked published",
			arrange: func(f *outboxQueueFixture) {
				f.expectLease(1, nil)
				f.expectAggregate(0, outboxAggregateActive)
				f.expectPublish(0, nil)
				f.expectMark(outboxMarkPublished, 0, nil)
			},
			want: []string{"publish:0", "published:0"},
		},
		{
			name: "terminal aggregate is discarded without a cause",
			arrange: func(f *outboxQueueFixture) {
				f.expectLease(1, nil)
				f.expectAggregate(0, outboxAggregateTerminal)
				f.expectMark(outboxMarkDiscarded, 0, nil)
			},
			want: []string{"discarded:0:<nil>"},
		},
		{
			name: "missing aggregate is discarded with the not-found cause",
			arrange: func(f *outboxQueueFixture) {
				f.expectLease(1, nil)
				f.expectAggregate(0, outboxAggregateMissing)
				f.expectMark(outboxMarkDiscarded, 0, nil)
			},
			want: []string{"discarded:0:not_found"},
		},
		{
			name: "aggregate lookup failure returns the record to pending",
			arrange: func(f *outboxQueueFixture) {
				f.expectLease(1, nil)
				f.expectAggregate(0, outboxAggregateUnavailable)
				f.expectMark(outboxMarkPending, 0, nil)
			},
			want: []string{"pending:0:lookup"},
		},
		{
			name: "publication failure returns the record to pending",
			arrange: func(f *outboxQueueFixture) {
				f.expectLease(1, nil)
				f.expectAggregate(0, outboxAggregateActive)
				f.expectPublish(0, errOutboxPublish)
				f.expectMark(outboxMarkPending, 0, nil)
			},
			want: []string{"publish:0", "pending:0:publish"},
		},
		{
			name: "published mark failure leaves the lease and continues the batch",
			arrange: func(f *outboxQueueFixture) {
				f.expectLease(2, nil)
				f.expectAggregate(0, outboxAggregateActive)
				f.expectPublish(0, nil)
				f.expectMark(outboxMarkPublished, 0, errOutboxMark)
				f.expectAggregate(1, outboxAggregateActive)
				f.expectPublish(1, nil)
				f.expectMark(outboxMarkPublished, 1, nil)
			},
			want: []string{"publish:0", "published:0", "publish:1", "published:1"},
		},
		{
			name: "pending mark failure continues the batch",
			arrange: func(f *outboxQueueFixture) {
				f.expectLease(2, nil)
				f.expectAggregate(0, outboxAggregateActive)
				f.expectPublish(0, errOutboxPublish)
				f.expectMark(outboxMarkPending, 0, errOutboxMark)
				f.expectAggregate(1, outboxAggregateActive)
				f.expectPublish(1, nil)
				f.expectMark(outboxMarkPublished, 1, nil)
			},
			want: []string{"publish:0", "pending:0:publish", "publish:1", "published:1"},
		},
		{
			name: "discard mark failure continues the batch",
			arrange: func(f *outboxQueueFixture) {
				f.expectLease(2, nil)
				f.expectAggregate(0, outboxAggregateMissing)
				f.expectMark(outboxMarkDiscarded, 0, errOutboxMark)
				f.expectAggregate(1, outboxAggregateActive)
				f.expectPublish(1, nil)
				f.expectMark(outboxMarkPublished, 1, nil)
			},
			want: []string{"discarded:0:not_found", "publish:1", "published:1"},
		},
		{
			name: "lease failure skips the batch",
			arrange: func(f *outboxQueueFixture) {
				f.expectLease(0, errOutboxLookup)
			},
		},
	}

	for _, class := range outboxQueueClasses {
		for _, scenario := range scenarios {
			t.Run(class.name+"/"+scenario.name, func(t *testing.T) {
				f := class.newFixture(t)
				f.interval.Store(1)
				scenario.arrange(f)

				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				f.run(ctx)

				require.Equal(t, scenario.want, f.recorded())
				require.EqualValues(t, 1, f.leases.Load())
			})
		}
	}
}

func TestContainerOutboxWorkerRequiresContainerRecord(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state outboxAggregate
		mark  outboxMark
		want  []string
	}{
		{name: "missing container is discarded", state: outboxContainerMissing, mark: outboxMarkDiscarded, want: []string{"discarded:0:not_found"}},
		{name: "container lookup failure returns the record to pending", state: outboxContainerUnavailable, mark: outboxMarkPending, want: []string{"pending:0:lookup"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newContainerOutboxFixture(t)
			f.interval.Store(1)
			f.expectLease(1, nil)
			f.expectAggregate(0, tc.state)
			f.expectMark(tc.mark, 0, nil)

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			f.run(ctx)

			require.Equal(t, tc.want, f.recorded())
		})
	}
}

func TestOutboxWorkersScheduleBatchesFromRuntimeInterval(t *testing.T) {
	for _, class := range outboxQueueClasses {
		t.Run(class.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := class.newFixture(t)
				f.interval.Store(2)
				f.expectLease(0, nil)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan struct{})
				go func() {
					defer close(done)
					f.run(ctx)
				}()

				synctest.Wait()
				require.EqualValues(t, 1, f.leases.Load(), "lease immediately on start")
				time.Sleep(time.Second)
				synctest.Wait()
				require.EqualValues(t, 1, f.leases.Load(), "wait for the configured interval")
				time.Sleep(time.Second)
				synctest.Wait()
				require.EqualValues(t, 2, f.leases.Load())

				// The interval is read before each wait, so a change applies from the next one.
				f.interval.Store(5)
				time.Sleep(2 * time.Second)
				synctest.Wait()
				require.EqualValues(t, 3, f.leases.Load())
				time.Sleep(4 * time.Second)
				synctest.Wait()
				require.EqualValues(t, 3, f.leases.Load())
				time.Sleep(time.Second)
				synctest.Wait()
				require.EqualValues(t, 4, f.leases.Load())

				cancel()
				synctest.Wait()
				requireClosed(t, done, "Run returns on cancellation")
				require.EqualValues(t, 4, f.leases.Load())
			})
		})
	}
}

func TestOutboxWorkersFloorNonPositiveIntervalAtOneSecond(t *testing.T) {
	for _, class := range outboxQueueClasses {
		t.Run(class.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := class.newFixture(t)
				f.expectLease(0, nil)
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan struct{})
				go func() {
					defer close(done)
					f.run(ctx)
				}()

				synctest.Wait()
				require.EqualValues(t, 1, f.leases.Load())
				time.Sleep(time.Second)
				synctest.Wait()
				require.EqualValues(t, 2, f.leases.Load())

				cancel()
				synctest.Wait()
				requireClosed(t, done, "Run returns on cancellation")
			})
		})
	}
}

func TestOutboxWorkersStopAfterCancellationDuringPublication(t *testing.T) {
	for _, class := range outboxQueueClasses {
		t.Run(class.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := class.newFixture(t)
				f.interval.Store(1)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				f.expectLease(2, nil)
				f.expectAggregate(0, outboxAggregateActive)
				f.publisher.EXPECT().Publish(mock.Anything, outboxTestExchange, outboxRoutingKey(0), outboxPayload(0)).
					RunAndReturn(func(ctx context.Context, _, _ string, _ []byte) error {
						f.record("publish:0")
						cancel()
						return ctx.Err()
					}).Once()
				f.expectMark(outboxMarkPending, 0, nil)
				// The remaining leased record is still visited; its lookup fails with the canceled context.
				f.expectAggregate(1, outboxAggregateCanceledLookup)
				f.expectMark(outboxMarkPending, 1, nil)

				done := make(chan struct{})
				go func() {
					defer close(done)
					f.run(ctx)
				}()
				synctest.Wait()

				requireClosed(t, done, "Run returns after the canceled batch")
				require.Equal(t, []string{"publish:0", "pending:0:canceled", "pending:1:canceled"}, f.recorded())
				require.EqualValues(t, 1, f.leases.Load(), "no lease after cancellation")
			})
		})
	}
}

func requireClosed(t *testing.T, done <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-done:
	default:
		t.Fatal(msg)
	}
}

func newOutboxQueueFixture(t *testing.T) *outboxQueueFixture {
	t.Helper()
	f := &outboxQueueFixture{publisher: coremocks.NewBuildQueuePublisher(t)}
	for range 2 {
		f.outboxIDs = append(f.outboxIDs, uuid.New())
		f.targetIDs = append(f.targetIDs, uuid.New())
	}
	return f
}

func (f *outboxQueueFixture) record(event string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

func (f *outboxQueueFixture) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.events
}

func (f *outboxQueueFixture) expectPublish(i int, err error) {
	f.publisher.EXPECT().Publish(mock.Anything, outboxTestExchange, outboxRoutingKey(i), outboxPayload(i)).
		RunAndReturn(func(context.Context, string, string, []byte) error {
			f.record(fmt.Sprintf("publish:%d", i))
			return err
		}).Once()
}

func (f *outboxQueueFixture) publishedMark(i int, err error) func(context.Context, uuid.UUID) error {
	return func(context.Context, uuid.UUID) error {
		f.record(fmt.Sprintf("published:%d", i))
		return err
	}
}

func (f *outboxQueueFixture) causeMark(mark outboxMark, i int, err error) func(context.Context, uuid.UUID, error) error {
	name := "pending"
	if mark == outboxMarkDiscarded {
		name = "discarded"
	}
	return func(_ context.Context, _ uuid.UUID, cause error) error {
		f.record(fmt.Sprintf("%s:%d:%s", name, i, outboxCauseLabel(cause)))
		return err
	}
}

func (f *outboxQueueFixture) leased(err error, records int) error {
	f.leases.Add(1)
	if err != nil {
		return err
	}
	if records > len(f.outboxIDs) {
		panic("outbox fixture supports at most two leased records")
	}
	return nil
}

// settings returns the runtime config read by the worker; batch size and
// interval are written to the class-specific fields by apply.
func (f *outboxQueueFixture) settings(apply func(cfg *config.SystemConfig, interval int64)) func() config.SystemConfig {
	return func() config.SystemConfig {
		var cfg config.SystemConfig
		apply(&cfg, f.interval.Load())
		return cfg
	}
}

func outboxRoutingKey(i int) string { return fmt.Sprintf("rk-%d", i) }

func outboxPayload(i int) []byte { return []byte(fmt.Sprintf("payload-%d", i)) }

func outboxCauseLabel(cause error) string {
	switch {
	case cause == nil:
		return "<nil>"
	case errors.Is(cause, apperrors.ErrNotFound):
		return "not_found"
	case errors.Is(cause, errOutboxLookup):
		return "lookup"
	case errors.Is(cause, errOutboxPublish):
		return "publish"
	case errors.Is(cause, context.Canceled):
		return "canceled"
	default:
		return cause.Error()
	}
}

func newBuildOutboxFixture(t *testing.T) *outboxQueueFixture {
	f := newOutboxQueueFixture(t)
	repo := coremocks.NewBuildOutboxRepository(t)
	cfg := coremocks.NewBuildOutboxConfigProvider(t)
	cfg.EXPECT().Get().RunAndReturn(f.settings(func(c *config.SystemConfig, interval int64) {
		c.BuildOutboxBatchSize = outboxTestBatchSize
		c.BuildOutboxIntervalSeconds = interval
	}))

	f.expectLease = func(records int, err error) {
		repo.EXPECT().LeasePendingBuildOutbox(mock.Anything, outboxTestBatchSize).
			RunAndReturn(func(context.Context, int) ([]model.BuildQueueOutbox, error) {
				if err := f.leased(err, records); err != nil {
					return nil, err
				}
				items := make([]model.BuildQueueOutbox, 0, records)
				for i := range records {
					items = append(items, model.BuildQueueOutbox{ID: f.outboxIDs[i], BuildID: f.targetIDs[i], Exchange: outboxTestExchange, RoutingKey: outboxRoutingKey(i), Payload: outboxPayload(i)})
				}
				return items, nil
			})
	}
	f.expectAggregate = func(i int, state outboxAggregate) {
		id := f.targetIDs[i]
		call := repo.EXPECT().GetByID(mock.Anything, id)
		call.Once()
		switch state {
		case outboxAggregateActive:
			// A running build still owns its queue message.
			call.Return(model.Build{ID: id, Status: model.BuildStatusRunning}, nil)
		case outboxAggregateTerminal:
			call.Return(model.Build{ID: id, Status: model.BuildStatusCanceled}, nil)
		case outboxAggregateMissing:
			call.Return(model.Build{}, apperrors.ErrNotFound)
		case outboxAggregateUnavailable:
			call.Return(model.Build{}, errOutboxLookup)
		case outboxAggregateCanceledLookup:
			call.RunAndReturn(func(ctx context.Context, _ uuid.UUID) (model.Build, error) { return model.Build{}, ctx.Err() })
		default:
			t.Fatalf("unsupported build aggregate state %d", state)
		}
	}
	f.expectMark = func(mark outboxMark, i int, err error) {
		id := f.outboxIDs[i]
		switch mark {
		case outboxMarkPublished:
			repo.EXPECT().MarkBuildOutboxPublished(mock.Anything, id).RunAndReturn(f.publishedMark(i, err)).Once()
		case outboxMarkPending:
			repo.EXPECT().MarkBuildOutboxPending(mock.Anything, id, mock.Anything).RunAndReturn(f.causeMark(mark, i, err)).Once()
		case outboxMarkDiscarded:
			repo.EXPECT().MarkBuildOutboxDiscarded(mock.Anything, id, mock.Anything).RunAndReturn(f.causeMark(mark, i, err)).Once()
		}
	}
	f.run = func(ctx context.Context) {
		NewBuildOutboxWorker(repo, f.publisher, cfg, discardLogger()).Run(ctx)
	}
	return f
}

func newComposeOutboxFixture(t *testing.T) *outboxQueueFixture {
	f := newOutboxQueueFixture(t)
	repo := coremocks.NewComposeOutboxRepository(t)
	cfg := coremocks.NewComposeOutboxConfigProvider(t)
	cfg.EXPECT().Get().RunAndReturn(f.settings(func(c *config.SystemConfig, interval int64) {
		c.ComposeOutboxBatchSize = outboxTestBatchSize
		c.ComposeOutboxIntervalSeconds = interval
	}))

	f.expectLease = func(records int, err error) {
		repo.EXPECT().LeasePendingComposeOutbox(mock.Anything, outboxTestBatchSize).
			RunAndReturn(func(context.Context, int) ([]model.ComposeDeploymentOutbox, error) {
				if err := f.leased(err, records); err != nil {
					return nil, err
				}
				items := make([]model.ComposeDeploymentOutbox, 0, records)
				for i := range records {
					items = append(items, model.ComposeDeploymentOutbox{ID: f.outboxIDs[i], JobID: f.targetIDs[i], Exchange: outboxTestExchange, RoutingKey: outboxRoutingKey(i), Payload: outboxPayload(i)})
				}
				return items, nil
			})
	}
	f.expectAggregate = func(i int, state outboxAggregate) {
		id := f.targetIDs[i]
		call := repo.EXPECT().GetComposeDeploymentJob(mock.Anything, id)
		call.Once()
		switch state {
		case outboxAggregateActive:
			// A canceling job still needs its message to reach cancellation handling.
			call.Return(model.ComposeDeploymentJob{ID: id, Status: model.ComposeDeploymentStatusCanceling}, nil)
		case outboxAggregateTerminal:
			call.Return(model.ComposeDeploymentJob{ID: id, Status: model.ComposeDeploymentStatusFailed}, nil)
		case outboxAggregateMissing:
			call.Return(model.ComposeDeploymentJob{}, apperrors.ErrNotFound)
		case outboxAggregateUnavailable:
			call.Return(model.ComposeDeploymentJob{}, errOutboxLookup)
		case outboxAggregateCanceledLookup:
			call.RunAndReturn(func(ctx context.Context, _ uuid.UUID) (model.ComposeDeploymentJob, error) {
				return model.ComposeDeploymentJob{}, ctx.Err()
			})
		default:
			t.Fatalf("unsupported compose aggregate state %d", state)
		}
	}
	f.expectMark = func(mark outboxMark, i int, err error) {
		id := f.outboxIDs[i]
		switch mark {
		case outboxMarkPublished:
			repo.EXPECT().MarkComposeOutboxPublished(mock.Anything, id).RunAndReturn(f.publishedMark(i, err)).Once()
		case outboxMarkPending:
			repo.EXPECT().MarkComposeOutboxPending(mock.Anything, id, mock.Anything).RunAndReturn(f.causeMark(mark, i, err)).Once()
		case outboxMarkDiscarded:
			repo.EXPECT().MarkComposeOutboxDiscarded(mock.Anything, id, mock.Anything).RunAndReturn(f.causeMark(mark, i, err)).Once()
		}
	}
	f.run = func(ctx context.Context) {
		NewComposeOutboxWorker(repo, f.publisher, cfg, discardLogger()).Run(ctx)
	}
	return f
}

func newContainerOutboxFixture(t *testing.T) *outboxQueueFixture {
	f := newOutboxQueueFixture(t)
	repo := coremocks.NewContainerOutboxRepository(t)
	cfg := coremocks.NewContainerOutboxConfigProvider(t)
	cfg.EXPECT().Get().RunAndReturn(f.settings(func(c *config.SystemConfig, interval int64) {
		c.ContainerCreateOutboxBatchSize = outboxTestBatchSize
		c.ContainerCreateOutboxIntervalSeconds = interval
	}))
	containerIDs := []uuid.UUID{uuid.New(), uuid.New()}

	f.expectLease = func(records int, err error) {
		repo.EXPECT().LeasePendingContainerOutbox(mock.Anything, outboxTestBatchSize).
			RunAndReturn(func(context.Context, int) ([]model.ContainerLifecycleOutbox, error) {
				if err := f.leased(err, records); err != nil {
					return nil, err
				}
				items := make([]model.ContainerLifecycleOutbox, 0, records)
				for i := range records {
					items = append(items, model.ContainerLifecycleOutbox{ID: f.outboxIDs[i], OperationID: f.targetIDs[i], ContainerID: containerIDs[i], Exchange: outboxTestExchange, RoutingKey: outboxRoutingKey(i), Payload: outboxPayload(i)})
				}
				return items, nil
			})
	}
	f.expectAggregate = func(i int, state outboxAggregate) {
		opID := f.targetIDs[i]
		containerID := containerIDs[i]
		op := repo.EXPECT().GetOperationByID(mock.Anything, opID)
		op.Once()
		pending := model.ResourceOperation{ID: opID, Status: model.OperationStatusPending}
		switch state {
		case outboxAggregateActive:
			op.Return(pending, nil)
			repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{ID: containerID}, nil).Once()
		case outboxAggregateTerminal:
			// Only a pending operation is publishable; a running one is discarded.
			op.Return(model.ResourceOperation{ID: opID, Status: model.OperationStatusRunning}, nil)
		case outboxAggregateMissing:
			op.Return(model.ResourceOperation{}, apperrors.ErrNotFound)
		case outboxAggregateUnavailable:
			op.Return(model.ResourceOperation{}, errOutboxLookup)
		case outboxAggregateCanceledLookup:
			op.RunAndReturn(func(ctx context.Context, _ uuid.UUID) (model.ResourceOperation, error) {
				return model.ResourceOperation{}, ctx.Err()
			})
		case outboxContainerMissing:
			op.Return(pending, nil)
			repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{}, apperrors.ErrNotFound).Once()
		case outboxContainerUnavailable:
			op.Return(pending, nil)
			repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{}, errOutboxLookup).Once()
		default:
			t.Fatalf("unsupported container aggregate state %d", state)
		}
	}
	f.expectMark = func(mark outboxMark, i int, err error) {
		id := f.outboxIDs[i]
		switch mark {
		case outboxMarkPublished:
			repo.EXPECT().MarkContainerOutboxPublished(mock.Anything, id).RunAndReturn(f.publishedMark(i, err)).Once()
		case outboxMarkPending:
			repo.EXPECT().MarkContainerOutboxPending(mock.Anything, id, mock.Anything).RunAndReturn(f.causeMark(mark, i, err)).Once()
		case outboxMarkDiscarded:
			repo.EXPECT().MarkContainerOutboxDiscarded(mock.Anything, id, mock.Anything).RunAndReturn(f.causeMark(mark, i, err)).Once()
		}
	}
	f.run = func(ctx context.Context) {
		NewContainerOutboxWorker(repo, f.publisher, cfg, discardLogger()).Run(ctx)
	}
	return f
}

func newResourceOutboxFixture(t *testing.T) *outboxQueueFixture {
	f := newOutboxQueueFixture(t)
	repo := coremocks.NewResourceOutboxRepository(t)
	cfg := coremocks.NewResourceOutboxConfigProvider(t)
	cfg.EXPECT().Get().RunAndReturn(f.settings(func(c *config.SystemConfig, interval int64) {
		c.ContainerCreateOutboxBatchSize = outboxTestBatchSize
		c.ContainerCreateOutboxIntervalSeconds = interval
	}))

	f.expectLease = func(records int, err error) {
		repo.EXPECT().LeasePendingResourceOutbox(mock.Anything, outboxTestBatchSize).
			RunAndReturn(func(context.Context, int) ([]model.ResourceLifecycleOutbox, error) {
				if err := f.leased(err, records); err != nil {
					return nil, err
				}
				items := make([]model.ResourceLifecycleOutbox, 0, records)
				for i := range records {
					items = append(items, model.ResourceLifecycleOutbox{ID: f.outboxIDs[i], OperationID: f.targetIDs[i], ResourceType: model.ResourceTypeVolume, ResourceID: uuid.New(), Exchange: outboxTestExchange, RoutingKey: outboxRoutingKey(i), Payload: outboxPayload(i)})
				}
				return items, nil
			})
	}
	f.expectAggregate = func(i int, state outboxAggregate) {
		opID := f.targetIDs[i]
		op := repo.EXPECT().GetOperationByID(mock.Anything, opID)
		op.Once()
		switch state {
		case outboxAggregateActive:
			op.Return(model.ResourceOperation{ID: opID, Status: model.OperationStatusPending}, nil)
		case outboxAggregateTerminal:
			op.Return(model.ResourceOperation{ID: opID, Status: model.OperationStatusRunning}, nil)
		case outboxAggregateMissing:
			op.Return(model.ResourceOperation{}, apperrors.ErrNotFound)
		case outboxAggregateUnavailable:
			op.Return(model.ResourceOperation{}, errOutboxLookup)
		case outboxAggregateCanceledLookup:
			op.RunAndReturn(func(ctx context.Context, _ uuid.UUID) (model.ResourceOperation, error) {
				return model.ResourceOperation{}, ctx.Err()
			})
		default:
			t.Fatalf("unsupported resource aggregate state %d", state)
		}
	}
	f.expectMark = func(mark outboxMark, i int, err error) {
		id := f.outboxIDs[i]
		switch mark {
		case outboxMarkPublished:
			repo.EXPECT().MarkResourceOutboxPublished(mock.Anything, id).RunAndReturn(f.publishedMark(i, err)).Once()
		case outboxMarkPending:
			repo.EXPECT().MarkResourceOutboxPending(mock.Anything, id, mock.Anything).RunAndReturn(f.causeMark(mark, i, err)).Once()
		case outboxMarkDiscarded:
			repo.EXPECT().MarkResourceOutboxDiscarded(mock.Anything, id, mock.Anything).RunAndReturn(f.causeMark(mark, i, err)).Once()
		}
	}
	f.run = func(ctx context.Context) {
		NewResourceOutboxWorker(repo, f.publisher, cfg, discardLogger()).Run(ctx)
	}
	return f
}
