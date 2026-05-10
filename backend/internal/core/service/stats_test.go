package service

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/google/uuid"
)

func TestStatsServiceGetSystemMonitoring(t *testing.T) {
	ctx := context.Background()
	contRepo := &statsContainerRepoFake{
		reservedMemory: 3 * bytesPerMB,
		statusCounts: model.ContainerStatusCounts{
			Total:   7,
			Running: 3,
			Stopped: 2,
			Error:   1,
			Missing: 1,
		},
	}
	volRepo := &statsVolumeRepoFake{total: 4, totalUsedBytes: 512}
	imgRepo := &statsImageRepoFake{total: 5, totalUsedMB: 2}
	buildRepo := &statsCountRepoFake{total: 6}
	projRepo := &statsProjectRepoFake{total: 2}
	metrics := statsMetricsFake{
		cpuPercent: 42.5,
		memory: model.HostMemoryStats{
			TotalBytes:     1000,
			UsedBytes:      600,
			AvailableBytes: 400,
		},
		disk: model.HostDiskStats{
			TotalBytes: 2000,
			UsedBytes:  1500,
			FreeBytes:  500,
		},
	}

	svc := NewStatsService(contRepo, volRepo, imgRepo, buildRepo, projRepo, metrics, "/host", nil, nil)

	stats, err := svc.GetSystemMonitoring(ctx)
	if err != nil {
		t.Fatalf("GetSystemMonitoring() error = %v", err)
	}

	if stats.CPUPercent != 42.5 {
		t.Fatalf("CPUPercent = %v, want 42.5", stats.CPUPercent)
	}
	if stats.MemoryTotalBytes != 1000 || stats.MemoryUsedBytes != 600 || stats.MemoryAvailableBytes != 400 {
		t.Fatalf("memory stats = %+v", stats)
	}
	if stats.DiskTotalBytes != 2000 || stats.DiskUsedBytes != 1500 || stats.DiskFreeBytes != 500 {
		t.Fatalf("disk stats = %+v", stats)
	}
	if stats.DCMReservedMemoryBytes != 3*bytesPerMB {
		t.Fatalf("DCMReservedMemoryBytes = %d", stats.DCMReservedMemoryBytes)
	}
	if stats.DCMDiskUsedBytes != 2*bytesPerMB+512 {
		t.Fatalf("DCMDiskUsedBytes = %d", stats.DCMDiskUsedBytes)
	}
	if stats.ContainersTotal != 7 || stats.ContainersRunning != 3 || stats.ContainersStopped != 2 || stats.ContainersError != 1 || stats.ContainersMissing != 1 {
		t.Fatalf("container counts = %+v", stats)
	}
	if stats.VolumesTotal != 4 || stats.ImagesTotal != 5 || stats.BuildsTotal != 6 || stats.ProjectsTotal != 2 {
		t.Fatalf("resource counts = %+v", stats)
	}
	if stats.ObservedAt.IsZero() {
		t.Fatal("ObservedAt is zero")
	}
}

type statsContainerRepoFake struct {
	reservedMemory int64
	statusCounts   model.ContainerStatusCounts
}

func (f *statsContainerRepoFake) GetUserReservedMemory(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	return 0, nil
}

func (f *statsContainerRepoFake) CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error) {
	return 0, nil
}

func (f *statsContainerRepoFake) List(ctx context.Context, opts model.ListOptions) ([]model.Container, int, error) {
	return nil, 0, nil
}

func (f *statsContainerRepoFake) GetTotalSystemReservedMemory(ctx context.Context) (int64, error) {
	return f.reservedMemory, nil
}

func (f *statsContainerRepoFake) GetSystemStatusCounts(ctx context.Context) (model.ContainerStatusCounts, error) {
	return f.statusCounts, nil
}

type statsVolumeRepoFake struct {
	total          int
	totalUsedBytes int64
}

func (f *statsVolumeRepoFake) CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error) {
	return 0, nil
}

func (f *statsVolumeRepoFake) GetUserUsedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	return 0, nil
}

func (f *statsVolumeRepoFake) GetTotalUsedVolumeBytes(ctx context.Context) (int64, error) {
	return f.totalUsedBytes, nil
}

func (f *statsVolumeRepoFake) CountAll(ctx context.Context) (int, error) {
	return f.total, nil
}

type statsImageRepoFake struct {
	total       int
	totalUsedMB int64
}

func (f *statsImageRepoFake) GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	return 0, nil
}

func (f *statsImageRepoFake) GetTotalUsedDiskSpace(ctx context.Context) (int64, error) {
	return f.totalUsedMB, nil
}

func (f *statsImageRepoFake) List(ctx context.Context, opts model.ListOptions) ([]model.Image, int, error) {
	return nil, f.total, nil
}

func (f *statsImageRepoFake) CountAll(ctx context.Context) (int, error) {
	return f.total, nil
}

type statsCountRepoFake struct {
	total int
}

func (f *statsCountRepoFake) CountAll(ctx context.Context) (int, error) {
	return f.total, nil
}

type statsProjectRepoFake struct {
	total int
}

func (f *statsProjectRepoFake) List(ctx context.Context, opts model.ListOptions) ([]model.Project, int, error) {
	return nil, f.total, nil
}

func (f *statsProjectRepoFake) CountAll(ctx context.Context) (int, error) {
	return f.total, nil
}

type statsMetricsFake struct {
	cpuPercent float64
	memory     model.HostMemoryStats
	disk       model.HostDiskStats
}

func (f statsMetricsFake) GetCPULoad() (float64, error) {
	return f.cpuPercent, nil
}

func (f statsMetricsFake) GetMemoryStats() (model.HostMemoryStats, error) {
	return f.memory, nil
}

func (f statsMetricsFake) GetDiskUsage(path string) (model.HostDiskStats, error) {
	return f.disk, nil
}
