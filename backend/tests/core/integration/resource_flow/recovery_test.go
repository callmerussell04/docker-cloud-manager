package resource_flow_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/pkg/rabbitmqtopology"
	amqp "github.com/rabbitmq/amqp091-go"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/resourcequeue"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/coretest"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Panic at an adapter boundary models process death: no service cleanup or ACK runs.
func TestResourceOperationsResumeAfterRestart(t *testing.T) {
	for _, kind := range []string{"volume create", "volume delete", "image delete"} {
		boundaries := []string{"docker"}
		if kind == "image delete" {
			boundaries = []string{"lookup", "registry", "docker"}
		}
		for _, boundary := range boundaries {
			for _, when := range []string{"before", "after"} {
				t.Run(kind+"/"+when+" "+boundary, func(t *testing.T) {
					ctx := context.Background()
					repos := coretest.OpenCoreRepositories(t)
					lifecycle := repository.NewResourceLifecycleRepository(repos.DB)
					cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
					// An interrupted final attempt must still be able to confirm its side effect.
					cfg.Config.ContainerCreateMaxAttempts = 1
					ownerID, resourceID := uuid.New(), uuid.New()
					crash := when + " " + boundary
					interrupt := func(point string) {
						if crash == point {
							panic("core stopped")
						}
					}
					volumeExists, manifestExists, imageExists := kind != "volume create", true, true
					volumeDocker := coremocks.NewVolumeDockerAPI(t)
					imageDocker := coremocks.NewImageDockerAPI(t)
					registry := coremocks.NewImageRegistryAPI(t)
					switch kind {
					case "volume create":
						volumeDocker.EXPECT().CreateVolume(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, spec model.VolumeRuntimeSpec) (string, error) {
							require.Equal(t, resourceID.String(), spec.VolumeID)
							require.Equal(t, ownerID.String(), spec.OwnerID)
							interrupt("before docker")
							volumeExists = true
							interrupt("after docker")
							return spec.VolumeName, nil
						})
					case "volume delete":
						volumeDocker.EXPECT().RemoveVolume(mock.Anything, "stable-volume", false).RunAndReturn(func(context.Context, string, bool) error {
							interrupt("before docker")
							if !volumeExists {
								return fmt.Errorf("already removed: %w", cerrdefs.ErrNotFound)
							}
							volumeExists = false
							interrupt("after docker")
							return nil
						})
					case "image delete":
						registry.EXPECT().GetImageSizeAndDigest(mock.Anything, mock.Anything, "latest").RunAndReturn(func(context.Context, string, string) (int64, string, error) {
							interrupt("before lookup")
							if !manifestExists {
								return 0, "", apperrors.ErrNotFound
							}
							interrupt("after lookup")
							return 100, "sha256:abc", nil
						})
						registry.EXPECT().DeleteManifest(mock.Anything, mock.Anything, "sha256:abc").RunAndReturn(func(context.Context, string, string) error {
							interrupt("before registry")
							manifestExists = false
							interrupt("after registry")
							return nil
						}).Maybe()
						imageDocker.EXPECT().RemoveImage(mock.Anything, mock.Anything, false).RunAndReturn(func(context.Context, string, bool) error {
							interrupt("before docker")
							if !imageExists {
								return cerrdefs.ErrNotFound
							}
							imageExists = false
							interrupt("after docker")
							return nil
						}).Maybe()
					}
					newWorker := func() (*service.VolumeService, *service.ImageService, *service.ResourceLifecycleWorker) {
						volumes := service.NewVolumeService(repos.Volumes, volumeDocker, cfg, service.VolumeServiceDeps{})
						volumes.SetLifecycleRepository(repository.NewResourceLifecycleRepository(repos.DB))
						images := service.NewImageService(repos.Images, imageDocker, registry, repos.Containers, cfg)
						images.SetLifecycleRepository(repository.NewResourceLifecycleRepository(repos.DB))
						return volumes, images, service.NewResourceLifecycleWorker(volumes, images, coretest.DiscardLogger())
					}
					volumes, images, worker := newWorker()
					switch kind {
					case "volume create":
						var err error
						resourceID, err = volumes.Create(coretest.UserContext(ownerID), model.VolumeCreateParams{Name: "data"})
						require.NoError(t, err)
					case "volume delete":
						require.NoError(t, repos.Volumes.Save(ctx, model.Volume{ID: resourceID, OwnerID: ownerID, Name: "data", DockerName: "stable-volume", Status: model.VolumeStatusAvailable}))
						require.NoError(t, volumes.Delete(coretest.UserContext(ownerID), resourceID))
					case "image delete":
						require.NoError(t, repos.Images.Save(ctx, model.Image{ID: resourceID, OwnerID: ownerID, Tag: "demo:latest"}))
						require.NoError(t, images.Delete(coretest.UserContext(ownerID), resourceID))
					}
					outbox, err := lifecycle.LeasePendingResourceOutbox(ctx, 10)
					require.NoError(t, err)
					require.Len(t, outbox, 1)
					var msg resourcequeue.LifecycleMessage
					require.NoError(t, json.Unmarshal(outbox[0].Payload, &msg))
					require.NoError(t, lifecycle.MarkResourceOutboxPublished(ctx, outbox[0].ID))
					require.PanicsWithValue(t, "core stopped", func() { _ = worker.HandleMessage(ctx, msg) })
					opID := uuid.MustParse(msg.OperationID)
					op, err := lifecycle.GetOperationByID(ctx, opID)
					require.NoError(t, err)
					require.Equal(t, model.OperationStatusRunning, op.Status)
					// The normal startup fallback must not fail resumable resource operations.
					_, err = repos.Containers.FailInterruptedNonResumableOperations(ctx, errors.New("core restarted"))
					require.NoError(t, err)
					_, err = lifecycle.RecoverInterruptedOperations(ctx, errors.New("core restarted"))
					require.NoError(t, err)
					_, err = lifecycle.RecoverInterruptedOperations(ctx, errors.New("core restarted again"))
					require.NoError(t, err)
					op, err = lifecycle.GetOperationByID(ctx, opID)
					require.NoError(t, err)
					require.Equal(t, model.OperationStatusPending, op.Status)
					require.Zero(t, op.Attempts, "process interruption does not consume the retry budget")
					outbox, err = lifecycle.LeasePendingResourceOutbox(ctx, 10)
					require.NoError(t, err)
					require.Len(t, outbox, 1)
					require.Equal(t, opID, outbox[0].OperationID)
					require.NoError(t, lifecycle.MarkResourceOutboxPublished(ctx, outbox[0].ID))
					crash = ""
					_, _, worker = newWorker()
					require.NoError(t, worker.HandleMessage(ctx, msg), "successful delivery can be ACKed")
					op, err = lifecycle.GetOperationByID(ctx, opID)
					require.NoError(t, err)
					require.Equal(t, model.OperationStatusDone, op.Status)
					require.Equal(t, 1, op.Attempts)
					if kind == "volume create" {
						vol, err := volumes.GetByID(coretest.UserContext(ownerID), resourceID)
						require.NoError(t, err)
						require.Equal(t, model.VolumeStatusAvailable, vol.Status)
						require.True(t, volumeExists)
					} else if kind == "volume delete" {
						_, err := volumes.GetByID(coretest.UserContext(ownerID), resourceID)
						require.ErrorIs(t, err, apperrors.ErrNotFound)
						require.False(t, volumeExists)
					} else {
						_, err := repos.Images.GetByID(ctx, resourceID)
						require.ErrorIs(t, err, apperrors.ErrNotFound)
						require.False(t, manifestExists)
						require.False(t, imageExists)
					}
					// ACK loss and duplicate outbox publication never repeat terminal work.
					require.NoError(t, worker.HandleMessage(ctx, msg))
					_, err = lifecycle.RecoverInterruptedOperations(ctx, errors.New("later restart"))
					require.NoError(t, err)
					pending, err := lifecycle.LeasePendingResourceOutbox(ctx, 10)
					require.NoError(t, err)
					require.Empty(t, pending)
				})
			}
		}
	}
}

// Equivalent durable snapshots cover process death between the resource write
// and operation completion, and pending messages already published before restart.
func TestResourceRecoveryPreservesCommittedResourceState(t *testing.T) {
	for _, kind := range []string{"volume create", "volume delete", "image delete"} {
		for _, state := range []string{"pending", "resource write committed"} {
			t.Run(kind+"/"+state, func(t *testing.T) {
				ctx := context.Background()
				repos := coretest.OpenCoreRepositories(t)
				lifecycle := repository.NewResourceLifecycleRepository(repos.DB)
				ownerID, resourceID := uuid.New(), uuid.New()
				cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
				volumes := service.NewVolumeService(repos.Volumes, coremocks.NewVolumeDockerAPI(t), cfg, service.VolumeServiceDeps{})
				volumes.SetLifecycleRepository(lifecycle)
				images := service.NewImageService(repos.Images, coremocks.NewImageDockerAPI(t), coremocks.NewImageRegistryAPI(t), repos.Containers, cfg)
				images.SetLifecycleRepository(lifecycle)
				if kind == "image delete" {
					require.NoError(t, repos.Images.Save(ctx, model.Image{ID: resourceID, OwnerID: ownerID, Tag: "demo:latest"}))
					require.NoError(t, images.Delete(coretest.UserContext(ownerID), resourceID))
				} else if kind == "volume delete" {
					require.NoError(t, repos.Volumes.Save(ctx, model.Volume{ID: resourceID, OwnerID: ownerID, Name: "data", DockerName: "stable-volume", Status: model.VolumeStatusAvailable}))
					require.NoError(t, volumes.Delete(coretest.UserContext(ownerID), resourceID))
				} else {
					var err error
					resourceID, err = volumes.Create(coretest.UserContext(ownerID), model.VolumeCreateParams{Name: "data"})
					require.NoError(t, err)
				}
				outbox, err := lifecycle.LeasePendingResourceOutbox(ctx, 10)
				require.NoError(t, err)
				require.Len(t, outbox, 1)
				require.NoError(t, lifecycle.MarkResourceOutboxPublished(ctx, outbox[0].ID))
				opID := outbox[0].OperationID
				if state != "pending" {
					_, claimed, err := lifecycle.ClaimPendingOperation(ctx, opID, 3)
					require.NoError(t, err)
					require.True(t, claimed)
					switch kind {
					case "volume create":
						require.NoError(t, repos.Volumes.UpdateStatus(ctx, resourceID, model.VolumeStatusAvailable))
					case "volume delete":
						require.NoError(t, repos.Volumes.Delete(ctx, resourceID))
					case "image delete":
						require.NoError(t, repos.Images.Delete(ctx, resourceID))
					}
				}
				_, err = lifecycle.RecoverInterruptedOperations(ctx, errors.New("core restarted"))
				require.NoError(t, err)
				outbox, err = lifecycle.LeasePendingResourceOutbox(ctx, 10)
				require.NoError(t, err)
				require.Len(t, outbox, 1)
				require.Equal(t, opID, outbox[0].OperationID)
				if kind == "volume create" || state == "pending" {
					if kind == "image delete" {
						img, err := repos.Images.GetByID(ctx, resourceID)
						require.NoError(t, err)
						require.Equal(t, model.ImageStatusDeleting, img.Status)
					} else {
						vol, err := repos.Volumes.GetByID(ctx, resourceID)
						require.NoError(t, err)
						want := model.VolumeStatusCreating
						if kind == "volume delete" {
							want = model.VolumeStatusDeleting
						}
						if state != "pending" {
							want = model.VolumeStatusAvailable
						}
						require.Equal(t, want, vol.Status)
					}
				}
				if state == "pending" {
					return
				} // External boundary cases are covered above.
				if kind == "volume create" {
					docker := coremocks.NewVolumeDockerAPI(t)
					docker.EXPECT().CreateVolume(mock.Anything, mock.Anything).Return("stable-volume", nil).Once()
					volumes = service.NewVolumeService(repos.Volumes, docker, cfg, service.VolumeServiceDeps{})
					volumes.SetLifecycleRepository(lifecycle)
				}
				var msg resourcequeue.LifecycleMessage
				require.NoError(t, json.Unmarshal(outbox[0].Payload, &msg))
				worker := service.NewResourceLifecycleWorker(volumes, images, coretest.DiscardLogger())
				require.NoError(t, worker.HandleMessage(ctx, msg))
				require.NoError(t, worker.HandleMessage(ctx, msg))
				op, err := lifecycle.GetOperationByID(ctx, opID)
				require.NoError(t, err)
				require.Equal(t, model.OperationStatusDone, op.Status)
			})
		}
	}
}

func TestResourceRecoveryPolicy(t *testing.T) {
	ctx := context.Background()
	repos := coretest.OpenCoreRepositories(t)
	lifecycle := repository.NewResourceLifecycleRepository(repos.DB)
	for _, tc := range []struct{ resource, operation, initial, want string }{
		{model.ResourceTypeContainer, model.OperationCreate, model.OperationStatusRunning, model.OperationStatusRunning},
		{model.ResourceTypeContainer, model.OperationStart, model.OperationStatusRunning, model.OperationStatusFailed},
		{model.ResourceTypeVolume, model.OperationCreate, model.OperationStatusRunning, model.OperationStatusPending},
		{model.ResourceTypeVolume, model.OperationDelete, model.OperationStatusRunning, model.OperationStatusPending},
		{model.ResourceTypeImage, model.OperationDelete, model.OperationStatusRunning, model.OperationStatusPending},
		{model.ResourceTypeImage, model.OperationCreate, model.OperationStatusRunning, model.OperationStatusFailed},
		{model.ResourceTypeVolume, model.OperationCreate, model.OperationStatusDone, model.OperationStatusDone},
		{model.ResourceTypeImage, model.OperationDelete, model.OperationStatusFailed, model.OperationStatusFailed},
		{model.ResourceTypeContainer, model.OperationStop, model.OperationStatusPending, model.OperationStatusPending},
	} {
		t.Run(tc.resource+"/"+tc.operation+"/"+tc.initial, func(t *testing.T) {
			id := uuid.New()
			require.NoError(t, repos.Containers.CreateOperation(ctx, model.ResourceOperation{ID: id, ResourceType: tc.resource, ResourceID: uuid.New(), OwnerID: uuid.New(), Operation: tc.operation, Status: tc.initial}))
			_, err := lifecycle.RecoverInterruptedOperations(ctx, errors.New("core restarted"))
			require.NoError(t, err)
			_, err = repos.Containers.FailInterruptedNonResumableOperations(ctx, errors.New("core restarted"))
			require.NoError(t, err)
			op, err := lifecycle.GetOperationByID(ctx, id)
			require.NoError(t, err)
			require.Equal(t, tc.want, op.Status)
		})
	}
}

func TestRecoveredResourceDeliveryRetriesAndACKs(t *testing.T) {
	url := coretest.RabbitMQURLFromEnv(t)
	for _, kind := range []string{"volume create", "volume delete", "image delete"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			repos := coretest.OpenCoreRepositories(t)
			lifecycle := repository.NewResourceLifecycleRepository(repos.DB)
			cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
			ownerID, resourceID := uuid.New(), uuid.New()
			volumeDocker := coremocks.NewVolumeDockerAPI(t)
			imageDocker := coremocks.NewImageDockerAPI(t)
			registry := coremocks.NewImageRegistryAPI(t)
			// The first delivery after recovery must NACK/requeue, then reach done and ACK.
			switch kind {
			case "volume create":
				volumeDocker.EXPECT().CreateVolume(mock.Anything, mock.Anything).Return("", apperrors.ErrUnavailable).Once()
				volumeDocker.EXPECT().CreateVolume(mock.Anything, mock.Anything).Return("stable-volume", nil).Once()
			case "volume delete":
				volumeDocker.EXPECT().RemoveVolume(mock.Anything, "stable-volume", false).Return(apperrors.ErrUnavailable).Once()
				volumeDocker.EXPECT().RemoveVolume(mock.Anything, "stable-volume", false).Return(nil).Once()
			case "image delete":
				registry.EXPECT().GetImageSizeAndDigest(mock.Anything, mock.Anything, "latest").Return(int64(0), "", apperrors.ErrUnavailable).Once()
				registry.EXPECT().GetImageSizeAndDigest(mock.Anything, mock.Anything, "latest").Return(int64(0), "", apperrors.ErrNotFound).Once()
				imageDocker.EXPECT().RemoveImage(mock.Anything, mock.Anything, false).Return(cerrdefs.ErrNotFound).Once()
			}
			volumes := service.NewVolumeService(repos.Volumes, volumeDocker, cfg, service.VolumeServiceDeps{})
			volumes.SetLifecycleRepository(lifecycle)
			images := service.NewImageService(repos.Images, imageDocker, registry, repos.Containers, cfg)
			images.SetLifecycleRepository(lifecycle)
			switch kind {
			case "volume create":
				var err error
				resourceID, err = volumes.Create(coretest.UserContext(ownerID), model.VolumeCreateParams{Name: "data"})
				require.NoError(t, err)
			case "volume delete":
				require.NoError(t, repos.Volumes.Save(ctx, model.Volume{ID: resourceID, OwnerID: ownerID, Name: "data", DockerName: "stable-volume", Status: model.VolumeStatusAvailable}))
				require.NoError(t, volumes.Delete(coretest.UserContext(ownerID), resourceID))
			case "image delete":
				require.NoError(t, repos.Images.Save(ctx, model.Image{ID: resourceID, OwnerID: ownerID, Tag: "demo:latest"}))
				require.NoError(t, images.Delete(coretest.UserContext(ownerID), resourceID))
			}
			outbox, err := lifecycle.LeasePendingResourceOutbox(ctx, 10)
			require.NoError(t, err)
			require.Len(t, outbox, 1)
			opID := outbox[0].OperationID
			require.NoError(t, lifecycle.MarkResourceOutboxPublished(ctx, outbox[0].ID))
			_, claimed, err := lifecycle.ClaimPendingOperation(ctx, opID, 3)
			require.NoError(t, err)
			require.True(t, claimed)
			_, err = lifecycle.RecoverInterruptedOperations(ctx, errors.New("core restarted"))
			require.NoError(t, err)

			conn, err := amqp.Dial(url)
			require.NoError(t, err)
			defer conn.Close()
			ch, err := conn.Channel()
			require.NoError(t, err)
			defer ch.Close()
			require.NoError(t, rabbitmqtopology.DeclareResource(ch))
			for _, queue := range []string{resourcequeue.QueueName, resourcequeue.DLQName} {
				_, err := ch.QueuePurge(queue, false)
				require.NoError(t, err)
			}
			publisher := rabbitmq.NewPublisher(url)
			defer publisher.Close()
			worker := service.NewResourceLifecycleWorker(volumes, images, coretest.DiscardLogger())
			consumer := rabbitmq.NewResourceConsumer(url, "recovery-test", coretest.DiscardLogger())
			results := make(chan error, 10)
			consumer.Run(ctx, 1, func(ctx context.Context, msg resourcequeue.LifecycleMessage) error {
				err := worker.HandleMessage(ctx, msg)
				select {
				case results <- err:
				case <-ctx.Done():
				}
				return err
			})
			outboxDone := make(chan struct{})
			outboxWorker := service.NewResourceOutboxWorker(lifecycle, publisher, cfg, coretest.DiscardLogger())
			go func() { defer close(outboxDone); outboxWorker.Run(ctx) }()
			// Join both workers before closing their infrastructure or checking broker ACKs.
			stop := func() {
				cancel()
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer stopCancel()
				require.NoError(t, consumer.Stop(stopCtx))
				select {
				case <-outboxDone:
				case <-stopCtx.Done():
					t.Fatal("outbox worker did not stop")
				}
			}
			t.Cleanup(stop)
			for _, wantErr := range []error{apperrors.ErrUnavailable, nil} {
				select {
				case err := <-results:
					require.ErrorIs(t, err, wantErr)
				case <-ctx.Done():
					t.Fatal("resource delivery did not finish")
				}
			}
			op, err := lifecycle.GetOperationByID(ctx, opID)
			require.NoError(t, err)
			require.Equal(t, model.OperationStatusDone, op.Status)
			require.Equal(t, 2, op.Attempts)
			require.NoError(t, publisher.Publish(ctx, outbox[0].Exchange, outbox[0].RoutingKey, outbox[0].Payload))
			select {
			case err := <-results:
				require.NoError(t, err, "duplicate terminal delivery must ACK")
			case <-ctx.Done():
				t.Fatal("duplicate delivery did not finish")
			}
			stop()
			for _, queue := range []string{resourcequeue.QueueName, resourcequeue.DLQName} {
				// Consumer shutdown closes the channel, returning any unacknowledged work.
				require.Eventually(t, func() bool {
					info, err := ch.QueueInspect(queue)
					return err == nil && info.Messages == 0 && info.Consumers == 0
				}, 3*time.Second, 20*time.Millisecond, "no unacknowledged or dead-lettered work remains")
			}
		})
	}
}
