package service

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/google/uuid"
)

func TestBuildServiceCreateBuildJobCreatesOutboxPayload(t *testing.T) {
	repo := &buildRepoFake{}
	imageRepo := &buildImageRepoFake{}
	users := &buildUsersFake{quotaDiskMB: 1024}
	svc := NewBuildService(repo, imageRepo, &buildRegistryFake{}, users, nil, BuildServiceDeps{})
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

	buildID, imageID, err := svc.CreateBuildJob(
		ctx,
		"demo-app",
		"build-archives/source.zip",
		"build-logs/source.log",
		".",
		"Dockerfile",
		map[string]string{"VERSION": "1"},
		"request-id",
	)
	if err != nil {
		t.Fatalf("CreateBuildJob returned error: %v", err)
	}
	if repo.queuedBuild.ID != buildID || repo.queuedImage.ID != imageID {
		t.Fatalf("queued ids mismatch: build=%s/%s image=%s/%s", repo.queuedBuild.ID, buildID, repo.queuedImage.ID, imageID)
	}
	if repo.queuedBuild.LogFilePath != "build-logs/source.log" {
		t.Fatalf("log key = %q", repo.queuedBuild.LogFilePath)
	}
	if repo.queuedBuild.ArchiveObjectKey != "build-archives/source.zip" {
		t.Fatalf("archive key = %q", repo.queuedBuild.ArchiveObjectKey)
	}

	var msg buildqueue.ImageBuildMessage
	if err := json.Unmarshal(repo.queuedOutbox.Payload, &msg); err != nil {
		t.Fatalf("outbox payload is not valid queue message: %v", err)
	}
	if msg.BuildID != buildID.String() || msg.ImageID != imageID.String() || msg.OwnerID != ownerID.String() {
		t.Fatalf("message ids mismatch: %+v", msg)
	}
	if msg.Tag != "demo-app:latest" {
		t.Fatalf("message tag = %q", msg.Tag)
	}
	if repo.queuedOutbox.Exchange != buildqueue.ExchangeName || repo.queuedOutbox.RoutingKey != buildqueue.RoutingKey {
		t.Fatalf("outbox route = %s/%s", repo.queuedOutbox.Exchange, repo.queuedOutbox.RoutingKey)
	}
}

func TestBuildServiceCreateBuildFromArchiveUploadsAndCreatesJob(t *testing.T) {
	repo := &buildRepoFake{}
	imageRepo := &buildImageRepoFake{}
	objects := &buildObjectStoreFake{}
	users := &buildUsersFake{quotaDiskMB: 1024}
	svc := NewBuildService(repo, imageRepo, &buildRegistryFake{}, users, nil, BuildServiceDeps{ObjectStore: objects})
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

	result, err := svc.CreateBuildFromArchive(ctx, BuildArchiveInput{
		Tag:         "demo-app",
		ContextDir:  ".",
		Dockerfile:  "Dockerfile",
		BuildArgs:   map[string]string{"VERSION": "1"},
		ArchiveName: "source.zip",
		Archive:     strings.NewReader("archive"),
	})
	if err != nil {
		t.Fatalf("CreateBuildFromArchive returned error: %v", err)
	}
	if result.BuildID == uuid.Nil || repo.queuedBuild.ID != result.BuildID {
		t.Fatalf("build id mismatch: result=%s queued=%s", result.BuildID, repo.queuedBuild.ID)
	}
	if len(objects.uploaded) != 1 || !strings.HasPrefix(objects.uploaded[0], "build-archives/") || !strings.HasSuffix(objects.uploaded[0], ".zip") {
		t.Fatalf("uploaded objects = %v, want zip archive object", objects.uploaded)
	}
	if repo.queuedBuild.ArchiveObjectKey != objects.uploaded[0] || repo.queuedBuild.LogFilePath == "" {
		t.Fatalf("queued build object keys not set: %+v", repo.queuedBuild)
	}
}

func TestBuildServiceOpenBuildLogsChecksScopedAccessBeforeObjectStore(t *testing.T) {
	ownerID := uuid.New()
	repo := &buildRepoFake{build: model.Build{
		ID:          uuid.New(),
		ImageID:     uuid.New(),
		OwnerID:     ownerID,
		LogFilePath: "build-logs/source.log",
	}}
	objects := &buildObjectStoreFake{objects: map[string]string{"build-logs/source.log": "logs"}}
	svc := NewBuildService(repo, &buildImageRepoFake{}, &buildRegistryFake{}, &buildUsersFake{}, nil, BuildServiceDeps{ObjectStore: objects})
	ctx := accessscope.WithUserScope(context.Background(), uuid.New(), "", "")

	if _, err := svc.OpenBuildLogs(ctx, repo.build.ID); err == nil {
		t.Fatal("OpenBuildLogs returned nil error for another user's build")
	}
	if objects.opened != 0 {
		t.Fatalf("object store opens = %d, want 0 before access is authorized", objects.opened)
	}
}

func TestBuildServiceStartBuildRecordSkipsTerminalBuild(t *testing.T) {
	buildID := uuid.New()
	repo := &buildRepoFake{build: model.Build{ID: buildID, ImageID: uuid.New(), Status: model.BuildStatusSuccess}}
	svc := NewBuildService(repo, &buildImageRepoFake{}, &buildRegistryFake{}, &buildUsersFake{}, nil, BuildServiceDeps{})

	_, started, err := svc.StartBuildRecord(context.Background(), buildID)
	if err != nil {
		t.Fatalf("StartBuildRecord returned error: %v", err)
	}
	if started {
		t.Fatal("StartBuildRecord started terminal build")
	}
	if repo.updatedStatus != "" {
		t.Fatalf("updated status = %q, want empty", repo.updatedStatus)
	}
}

func TestBuildServiceCompleteBuildRecordSkipsTerminalBuild(t *testing.T) {
	buildID := uuid.New()
	imageID := uuid.New()
	repo := &buildRepoFake{build: model.Build{ID: buildID, ImageID: imageID, Status: model.BuildStatusFailed}}
	imageRepo := &buildImageRepoFake{}
	svc := NewBuildService(repo, imageRepo, &buildRegistryFake{}, &buildUsersFake{}, nil, BuildServiceDeps{})

	if err := svc.CompleteBuildRecord(context.Background(), buildID, imageID, model.BuildStatusSuccess, 0); err != nil {
		t.Fatalf("CompleteBuildRecord returned error: %v", err)
	}
	if imageRepo.updateBuildAndImageCalled || imageRepo.markFailedCalled {
		t.Fatal("CompleteBuildRecord mutated a terminal build")
	}
}

func TestBuildServiceCompleteBuildRecordQuotaFailureDoesNotReturnRetryableError(t *testing.T) {
	buildID := uuid.New()
	imageID := uuid.New()
	ownerID := uuid.New()
	repo := &buildRepoFake{build: model.Build{
		ID:               buildID,
		ImageID:          imageID,
		OwnerID:          ownerID,
		Status:           model.BuildStatusRunning,
		ArchiveObjectKey: "build-archives/source.zip",
	}}
	imageRepo := &buildImageRepoFake{imageOwnerID: ownerID, imageTag: "demo-app:latest"}
	objects := &buildObjectStoreFake{}
	svc := NewBuildService(repo, imageRepo, &buildRegistryFake{sizeBytes: 2 * 1024 * 1024}, &buildUsersFake{quotaDiskMB: 1}, nil, BuildServiceDeps{ObjectStore: objects})

	if err := svc.CompleteBuildRecord(context.Background(), buildID, imageID, model.BuildStatusSuccess, 0); err != nil {
		t.Fatalf("CompleteBuildRecord returned error: %v", err)
	}
	if !imageRepo.markFailedCalled || imageRepo.markFailedStatus != model.BuildStatusFailedQuotaExceeded {
		t.Fatalf("quota failure status mismatch: called=%v status=%q", imageRepo.markFailedCalled, imageRepo.markFailedStatus)
	}
	if len(objects.deleted) != 1 || objects.deleted[0] != "build-archives/source.zip" {
		t.Fatalf("deleted objects = %v, want archive cleanup", objects.deleted)
	}
}

func TestBuildServiceCancelStandaloneBuildDoesNotCancelDeployment(t *testing.T) {
	buildID := uuid.New()
	imageID := uuid.New()
	ownerID := uuid.New()
	repo := &buildRepoFake{build: model.Build{
		ID:               buildID,
		ImageID:          imageID,
		OwnerID:          ownerID,
		Status:           model.BuildStatusPending,
		ArchiveObjectKey: "build-archives/source.zip",
	}}
	imageRepo := &buildImageRepoFake{}
	objects := &buildObjectStoreFake{}
	deployments := &buildDeploymentCancelerFake{}
	svc := NewBuildService(repo, imageRepo, &buildRegistryFake{}, &buildUsersFake{}, nil, BuildServiceDeps{ObjectStore: objects, Deployments: deployments})

	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	if err := svc.CancelBuildRecord(ctx, buildID); err != nil {
		t.Fatalf("CancelBuildRecord returned error: %v", err)
	}
	if !imageRepo.markFailedCalled || imageRepo.markFailedStatus != model.BuildStatusCanceled {
		t.Fatalf("build was not marked canceled, called=%v status=%q", imageRepo.markFailedCalled, imageRepo.markFailedStatus)
	}
	if len(objects.deleted) != 1 || objects.deleted[0] != "build-archives/source.zip" {
		t.Fatalf("deleted objects = %v, want canceled archive", objects.deleted)
	}
	if deployments.calls != 0 {
		t.Fatalf("deployment cancel calls = %d, want 0", deployments.calls)
	}
}

func TestBuildServiceCancelComposeBuildCancelsDeploymentOnce(t *testing.T) {
	buildID := uuid.New()
	imageID := uuid.New()
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := &buildRepoFake{build: model.Build{
		ID:        buildID,
		ImageID:   imageID,
		OwnerID:   ownerID,
		ProjectID: &projectID,
		Status:    model.BuildStatusPending,
	}}
	imageRepo := &buildImageRepoFake{onMarkFailed: func() {
		repo.build.Status = model.BuildStatusCanceled
	}}
	deployments := &buildDeploymentCancelerFake{}
	svc := NewBuildService(repo, imageRepo, &buildRegistryFake{}, &buildUsersFake{}, nil, BuildServiceDeps{Deployments: deployments})

	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	if err := svc.CancelBuildRecord(ctx, buildID); err != nil {
		t.Fatalf("CancelBuildRecord returned error: %v", err)
	}
	if deployments.calls != 1 || deployments.projectID != projectID || deployments.buildID != buildID {
		t.Fatalf("deployment cancellation mismatch: calls=%d project=%s build=%s", deployments.calls, deployments.projectID, deployments.buildID)
	}
	if err := svc.CancelBuildRecord(ctx, buildID); err != nil {
		t.Fatalf("second CancelBuildRecord returned error: %v", err)
	}
	if deployments.calls != 1 {
		t.Fatalf("deployment cancel calls after idempotent retry = %d, want 1", deployments.calls)
	}
}

type buildRepoFake struct {
	build         model.Build
	queuedImage   model.Image
	queuedBuild   model.Build
	queuedOutbox  model.BuildQueueOutbox
	updatedStatus string
}

func (f *buildRepoFake) Save(ctx context.Context, b model.Build) error { return nil }
func (f *buildRepoFake) CreateQueuedBuild(ctx context.Context, img model.Image, build model.Build, outbox model.BuildQueueOutbox) error {
	f.queuedImage = img
	f.queuedBuild = build
	f.queuedOutbox = outbox
	f.build = build
	return nil
}
func (f *buildRepoFake) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	f.updatedStatus = status
	f.build.Status = status
	return nil
}
func (f *buildRepoFake) StartBuild(ctx context.Context, id uuid.UUID) (model.Build, bool, error) {
	if f.build.Status != model.BuildStatusPending {
		return f.build, false, nil
	}
	f.updatedStatus = model.BuildStatusRunning
	f.build.Status = model.BuildStatusRunning
	return f.build, true, nil
}
func (f *buildRepoFake) GetByID(ctx context.Context, id uuid.UUID) (model.Build, error) {
	return f.build, nil
}
func (f *buildRepoFake) Delete(ctx context.Context, id uuid.UUID) error { return nil }
func (f *buildRepoFake) List(ctx context.Context, opts model.ListOptions) ([]model.Build, int, error) {
	return nil, 0, nil
}

type buildImageRepoFake struct {
	updateBuildAndImageCalled bool
	markFailedCalled          bool
	markFailedStatus          string
	onMarkFailed              func()
	imageOwnerID              uuid.UUID
	imageTag                  string
}

func (f *buildImageRepoFake) Save(ctx context.Context, img model.Image) error { return nil }
func (f *buildImageRepoFake) GetByID(ctx context.Context, id uuid.UUID) (model.Image, error) {
	ownerID := f.imageOwnerID
	if ownerID == uuid.Nil {
		ownerID = uuid.New()
	}
	tag := f.imageTag
	if tag == "" {
		tag = "demo-app:latest"
	}
	return model.Image{ID: id, OwnerID: ownerID, Tag: tag}, nil
}
func (f *buildImageRepoFake) Delete(ctx context.Context, id uuid.UUID) error { return nil }
func (f *buildImageRepoFake) GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	return 0, nil
}
func (f *buildImageRepoFake) UpdateBuildAndImageSizeTx(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error {
	f.updateBuildAndImageCalled = true
	return nil
}
func (f *buildImageRepoFake) MarkBuildFailedAndDeleteImageTx(ctx context.Context, buildID, imageID uuid.UUID, status string) error {
	f.markFailedCalled = true
	f.markFailedStatus = status
	if f.onMarkFailed != nil {
		f.onMarkFailed()
	}
	return nil
}

type buildObjectStoreFake struct {
	uploaded []string
	deleted  []string
	objects  map[string]string
	opened   int
}

func (f *buildObjectStoreFake) UploadStream(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) error {
	f.uploaded = append(f.uploaded, objectKey)
	_, err := io.Copy(io.Discard, reader)
	return err
}

func (f *buildObjectStoreFake) OpenObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	f.opened++
	return io.NopCloser(strings.NewReader(f.objects[objectKey])), nil
}

func (f *buildObjectStoreFake) DeleteObject(ctx context.Context, objectKey string) error {
	f.deleted = append(f.deleted, objectKey)
	return nil
}

type buildDeploymentCancelerFake struct {
	calls     int
	projectID uuid.UUID
	buildID   uuid.UUID
}

func (f *buildDeploymentCancelerFake) CancelDeploymentForBuild(ctx context.Context, projectID, buildID uuid.UUID) error {
	f.calls++
	f.projectID = projectID
	f.buildID = buildID
	return nil
}

type buildRegistryFake struct {
	sizeBytes int64
}

func (f *buildRegistryFake) GetImageSizeAndDigest(ctx context.Context, repo, tag string) (int64, string, error) {
	if f.sizeBytes > 0 {
		return f.sizeBytes, "digest", nil
	}
	return 1, "digest", nil
}
func (f *buildRegistryFake) DeleteManifest(ctx context.Context, repo, digest string) error {
	return nil
}

type buildUsersFake struct {
	quotaDiskMB int64
}

func (f *buildUsersFake) GetUser(ctx context.Context, userID uuid.UUID) (model.UserInfo, error) {
	quota := f.quotaDiskMB
	if quota == 0 {
		quota = 1024
	}
	return model.UserInfo{ID: userID, QuotaDiskMB: quota}, nil
}
