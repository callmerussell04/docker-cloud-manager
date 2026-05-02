package docker

import (
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/telemetry/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

func TestValidateLabels(t *testing.T) {
	target := model.ContainerTarget{
		ContainerID:      "container-id",
		OwnerID:          "owner-id",
		DockerGeneration: 2,
	}
	labels := map[string]string{
		"managed_by":        "docker-cloud-manager",
		"dcm.resource_type": "container",
		"dcm.container_id":  "container-id",
		"dcm.owner_id":      "owner-id",
		"dcm.generation":    "2",
	}

	if err := validateLabels(labels, target); err != nil {
		t.Fatalf("validateLabels() error = %v", err)
	}

	labels["dcm.owner_id"] = "other-owner"
	if err := validateLabels(labels, target); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("validateLabels() mismatch error = %v, want ErrNotFound", err)
	}
}
