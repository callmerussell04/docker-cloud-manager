package build_flow_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	builderconfig "github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/archive"
	builderrabbit "github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/storage"
	buildermodel "github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/model"
	builderservice "github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	platformrabbitmq "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/brokertest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	builderservicemocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/builder/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/testenv"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBuilderFinalizationSurvivesCoreOutageAndWorkerRestart(t *testing.T) {
	for _, scenario := range []struct {
		name                 string
		lostResponse, cancel bool
	}{
		{name: "Core unavailable"}, {name: "accepted completion response lost", lostResponse: true}, {name: "canceled before retry", cancel: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			url := testenv.Lookup(t, "BUILDER_TEST_RABBITMQ_URL")
			buildQueue := brokertest.UniqueQueue(t, url, buildqueue.Queue)
			store := objectstorage.NewLazyStorage(coretest.ObjectStorageConfigFromEnv(t))
			repos := coretest.OpenCoreRepositories(t)
			ownerID := uuid.New()
			builds := newBuildServiceWithStore(t, repos, ownerID, store)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var source bytes.Buffer
			zw := zip.NewWriter(&source)
			df, err := zw.Create("Dockerfile")
			require.NoError(t, err)
			_, err = io.WriteString(df, "FROM scratch\n")
			require.NoError(t, err)
			require.NoError(t, zw.Close())
			created, err := builds.CreateBuildFromArchive(coretest.UserContext(ownerID), service.BuildArchiveInput{Tag: "finalize:latest", ArchiveName: "source.zip", Archive: bytes.NewReader(source.Bytes())})
			require.NoError(t, err)
			build, err := builds.GetBuild(coretest.UserContext(ownerID), created.BuildID)
			require.NoError(t, err)
			t.Cleanup(func() { _ = builds.DeleteBuild(coretest.UserContext(ownerID), created.BuildID) })
			var msg buildqueue.ImageBuildMessage
			require.NoError(t, json.Unmarshal(mustBuildOutbox(t, repos.DB)[0].Payload, &msg))

			conn, err := amqp.Dial(url)
			require.NoError(t, err)
			defer func() { _ = conn.Close() }() // test teardown; broker close failure is not actionable
			ch, err := conn.Channel()
			require.NoError(t, err)
			defer func() { _ = ch.Close() }() // test teardown; closing the connection already releases it
			require.NoError(t, platformrabbitmq.Declare(ch, buildQueue))

			dir := t.TempDir()
			files, err := storage.NewFileManager(dir)
			require.NoError(t, err)
			workspaces, err := storage.NewWorkspaceManager(dir)
			require.NoError(t, err)
			logs, err := storage.NewLogManager(filepath.Join(dir, "logs"))
			require.NoError(t, err)
			docker := builderservicemocks.NewDockerAPI(t)
			docker.EXPECT().RunBuildContainer(mock.Anything, mock.Anything).Return("kaniko", io.NopCloser(bytes.NewBufferString("built\n")), nil).Once()
			docker.EXPECT().WaitForBuild(mock.Anything, "kaniko").Return(nil).Once()
			docker.EXPECT().CleanBuildContainer(mock.Anything, "kaniko").Return(nil).Once()
			network := builderservicemocks.NewBuildNetworkAdmission(t)
			network.EXPECT().ValidateBuildNetwork(mock.Anything, mock.Anything).Return(nil).Maybe()
			core := builderservicemocks.NewCoreClient(t)
			core.EXPECT().StartBuildRecord(mock.Anything, msg.BuildID).RunAndReturn(func(ctx context.Context, _ string) (buildermodel.BuildClaim, bool, buildermodel.BuildStatus, error) {
				b, started, err := builds.StartBuildRecord(ctx, created.BuildID)
				return buildermodel.BuildClaim{ImageID: b.ImageID.String(), RegistryUsername: "scoped", RegistryPassword: "secret", Destination: "registry:5000/build:latest"}, started, buildermodel.BuildStatus(b.Status), err
			}).Once()
			core.EXPECT().GetBuildStatus(mock.Anything, msg.BuildID).Return("running", nil).Maybe()
			var completions atomic.Int32
			core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, buildermodel.BuildStatus("success"), 0).RunAndReturn(func(ctx context.Context, _, _ string, status buildermodel.BuildStatus, size int) error {
				if completions.Add(1) == 1 {
					if scenario.lostResponse {
						if err := builds.CompleteBuildRecord(ctx, created.BuildID, build.ImageID, string(status), size); err != nil {
							return err
						}
					}
					return apperrors.ErrUnavailable
				}
				return builds.CompleteBuildRecord(ctx, created.BuildID, build.ImageID, string(status), size)
			}).Twice()
			first := make(chan error, 1)
			done := make(chan error, 1)
			retry := make(chan struct{})
			var deliveries atomic.Int32
			consumer := platformrabbitmq.NewConsumer(builderrabbit.BuildConsumerConfig(url, uuid.NewString(), buildQueue, coretest.DiscardLogger()))
			consumer.Run(ctx, 1, func(ctx context.Context, msg buildqueue.ImageBuildMessage) error {
				attempt := deliveries.Add(1)
				if attempt > 1 {
					select {
					case <-retry:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				// Reopen the journal and reconstruct Builder on every broker delivery.
				results, err := storage.NewResultStore(dir)
				if err != nil {
					return err
				}
				cfg := builderconfig.BuilderConfig{MaxBuildTime: time.Minute, MaxConcurrentBuilds: 1, MaxUnpackedSizeBytes: 1 << 20, MaxBuildLogSizeBytes: 1 << 20, BuildCancelPollInterval: time.Hour}
				worker, err := builderservice.NewBuilderService(builderservice.BuilderServiceDeps{
					Files: files, Workspaces: workspaces, Extractor: archive.NewExtractor(), Docker: docker,
					NetworkAdmission: network, Logs: logs, ObjectStore: store, Core: core, Results: results,
					Config: builderconfig.NewRuntimeManager(cfg, nil, nil), Logger: coretest.DiscardLogger(),
				})
				if err != nil {
					return err
				}
				err = worker.HandleBuildMessage(ctx, msg)
				if attempt == 1 {
					first <- err
				} else {
					done <- err
				}
				return err
			})
			t.Cleanup(func() {
				cancel()
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer stopCancel()
				_ = consumer.Stop(stopCtx)
			})
			body, err := json.Marshal(msg)
			require.NoError(t, err)
			require.NoError(t, ch.PublishWithContext(ctx, buildQueue.Exchange, buildQueue.RoutingKey, false, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body}))
			select {
			case err := <-first:
				require.ErrorIs(t, err, apperrors.ErrUnavailable)
			case <-ctx.Done():
				t.Fatal("first delivery timed out")
			}
			require.FileExists(t, logs.LogPath(msg.BuildID))
			// Simulate extracted files left by a process killed during Core completion.
			abandonedWorkspace, _, err := workspaces.Create(msg.BuildID)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(abandonedWorkspace, "source"), []byte("source"), 0600))
			if !scenario.lostResponse {
				b, err := builds.GetBuild(coretest.UserContext(ownerID), created.BuildID)
				require.NoError(t, err)
				require.Equal(t, model.BuildStatusRunning, b.Status)
				reserved, err := repos.Staged.ActiveBytesByOwner(ctx, ownerID)
				require.NoError(t, err)
				require.Positive(t, reserved)
				_, err = store.StatObject(ctx, build.ArchiveObjectKey)
				require.NoError(t, err)
			}
			if scenario.cancel {
				require.NoError(t, builds.CancelBuildRecord(coretest.UserContext(ownerID), created.BuildID))
			}
			close(retry)
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal("finalization retry timed out")
			}
			cancel()
			require.NoError(t, consumer.Stop(context.Background()))
			b, err := builds.GetBuild(coretest.UserContext(ownerID), created.BuildID)
			require.NoError(t, err)
			wantStatus := model.BuildStatusSuccess
			if scenario.cancel {
				wantStatus = model.BuildStatusCanceled
			}
			require.Equal(t, wantStatus, b.Status)
			reserved, err := repos.Staged.ActiveBytesByOwner(context.Background(), ownerID)
			require.NoError(t, err)
			require.Zero(t, reserved)
			_, err = store.StatObject(context.Background(), build.ArchiveObjectKey)
			require.ErrorIs(t, err, apperrors.ErrNotFound)
			require.NoFileExists(t, logs.LogPath(msg.BuildID))
			require.NoDirExists(t, abandonedWorkspace)
			results, err := storage.NewResultStore(dir)
			require.NoError(t, err)
			_, found, err := results.Load(context.Background(), msg.BuildID)
			require.NoError(t, err)
			require.False(t, found)
			queue, err := ch.QueueDeclarePassive(buildQueue.Name, true, false, false, false, nil)
			require.NoError(t, err)
			require.Zero(t, queue.Messages)
			require.Equal(t, int32(2), deliveries.Load())
		})
	}
}
