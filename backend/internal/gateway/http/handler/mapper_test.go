package handler

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

func TestCreateContainerInputFromDTO(t *testing.T) {
	req := dto.CreateContainerDTO{
		Name:         "web",
		ImageTag:     "nginx:latest",
		InternalPort: 80,
		EnvVars:      map[string]string{"APP_ENV": "test"},
		DomainPrefix: "web",
		VolumeMounts: []dto.VolumeMountDTO{
			{VolumeID: "volume-id", MountPath: "/data", IsReadOnly: true},
		},
	}

	got := createContainerInputFromDTO(req)

	if got.Name != req.Name || got.ImageTag != req.ImageTag || got.InternalPort != req.InternalPort || got.DomainPrefix != req.DomainPrefix {
		t.Fatalf("input fields were not mapped correctly: %+v", got)
	}
	if got.EnvVars["APP_ENV"] != "test" {
		t.Fatalf("EnvVars were not mapped")
	}
	if len(got.VolumeMounts) != 1 || got.VolumeMounts[0].VolumeID != "volume-id" || !got.VolumeMounts[0].IsReadOnly {
		t.Fatalf("VolumeMounts were not mapped: %+v", got.VolumeMounts)
	}
}

func TestContainersToDTO(t *testing.T) {
	items := []model.Container{
		{
			ID:            "container-id",
			DockerID:      "docker-id",
			Name:          "web",
			ImageTag:      "nginx:latest",
			InternalPort:  80,
			DomainPrefix:  "web",
			Status:        "running",
			CreatedAt:     123,
			OwnerID:       "owner-id",
			OwnerUsername: "alice",
		},
	}

	got := containersToDTO(items)

	if len(got) != 1 {
		t.Fatalf("containersToDTO() len = %d, want 1", len(got))
	}
	if got[0].ID != items[0].ID || got[0].OwnerUsername != items[0].OwnerUsername {
		t.Fatalf("container was not mapped correctly: %+v", got[0])
	}
}
