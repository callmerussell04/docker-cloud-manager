package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/google/uuid"
)

func TestBuildServiceCreateBuildJobCreatesOutboxPayload(t *testing.T) {
	repo := &buildRepoFake{}
	imageRepo := &buildImageRepoFake{}
	users := &buildUsersFake{quotaDiskMB: 1024}
	svc := NewBuildService(repo, imageRepo, &buildRegistryFake{}, users, nil)
	ownerID := uuid.New()

	buildID, imageID, err := svc.CreateBuildJob(
		context.Background(),
		ownerID,
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

func TestBuildServiceStartBuildRecordSkipsTerminalBuild(t *testing.T) {
	buildID := uuid.New()
	repo := &buildRepoFake{build: model.Build{ID: buildID, ImageID: uuid.New(), Status: model.BuildStatusSuccess}}
	svc := NewBuildService(repo, &buildImageRepoFake{}, &buildRegistryFake{}, &buildUsersFake{}, nil)

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
	svc := NewBuildService(repo, imageRepo, &buildRegistryFake{}, &buildUsersFake{}, nil)

	if err := svc.CompleteBuildRecord(context.Background(), buildID, imageID, model.BuildStatusSuccess, 0); err != nil {
		t.Fatalf("CompleteBuildRecord returned error: %v", err)
	}
	if imageRepo.updateBuildAndImageCalled || imageRepo.markFailedCalled {
		t.Fatal("CompleteBuildRecord mutated a terminal build")
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
func (f *buildRepoFake) GetUserBuilds(ctx context.Context, ownerID uuid.UUID) ([]model.Build, error) {
	return nil, nil
}
func (f *buildRepoFake) GetByID(ctx context.Context, id uuid.UUID) (model.Build, error) {
	return f.build, nil
}
func (f *buildRepoFake) Delete(ctx context.Context, id uuid.UUID) error { return nil }
func (f *buildRepoFake) GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Build, int, error) {
	return nil, 0, nil
}

type buildImageRepoFake struct {
	updateBuildAndImageCalled bool
	markFailedCalled          bool
}

func (f *buildImageRepoFake) Save(ctx context.Context, img model.Image) error { return nil }
func (f *buildImageRepoFake) GetByID(ctx context.Context, id uuid.UUID) (model.Image, error) {
	return model.Image{ID: id, OwnerID: uuid.New(), Tag: "demo-app:latest"}, nil
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
	return nil
}

type buildRegistryFake struct{}

func (f *buildRegistryFake) GetImageSizeAndDigest(ctx context.Context, repo, tag string) (int64, string, error) {
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
