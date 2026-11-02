package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestContainerCreateImageSourcePolicy(t *testing.T) {
	for _, tc := range []struct {
		image   string
		allowed bool
	}{
		{"nginx:alpine", true}, {"docker.io/library/nginx:alpine", true}, {"my-app:v1", true},
		{"evil.example/acme/app:latest", false}, {"localhost/repo", false}, {"127.0.0.1/repo", false},
		{"registry:5000/repo", false}, {"Docker.io/nginx", false}, {"docker.io%2fnginx", false},
	} {
		t.Run(tc.image, func(t *testing.T) {
			svc := newCoreServiceMock(t)
			if tc.allowed {
				svc.EXPECT().CreateContainer(mock.Anything, mock.MatchedBy(func(input model.CreateContainerInput) bool { return input.ImageTag == tc.image })).Return(uuid.NewString(), nil).Once()
			}
			body, err := json.Marshal(map[string]string{"name": "web", "image_tag": tc.image})
			require.NoError(t, err)
			resp := perform(coreRouter(svc), http.MethodPost, "/containers", string(body), nil)
			if tc.allowed {
				require.Equal(t, http.StatusAccepted, resp.Code)
			} else {
				require.Equal(t, http.StatusBadRequest, resp.Code)
				svc.AssertNotCalled(t, "CreateContainer", mock.Anything, mock.Anything)
			}
		})
	}
}
