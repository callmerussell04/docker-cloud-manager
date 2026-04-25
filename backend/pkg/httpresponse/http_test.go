package httpresponse

import (
	"net/http"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

func TestStatusMapsConflict(t *testing.T) {
	err := apperrors.New(apperrors.ErrResourceInUse, "image is currently used by a container")

	if got := Status(err); got != http.StatusConflict {
		t.Fatalf("Status() = %d, want %d", got, http.StatusConflict)
	}
}
