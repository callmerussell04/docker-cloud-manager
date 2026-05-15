package authscope_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/authscope"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFromAuthUserBuildsTypedScope(t *testing.T) {
	userID := uuid.New()

	for _, tt := range []struct {
		name string
		kind accessscope.Kind
	}{
		{name: "user", kind: accessscope.KindUser},
		{name: "admin", kind: accessscope.KindAdmin},
	} {
		t.Run(tt.name, func(t *testing.T) {
			scope, err := authscope.FromAuthUser(model.AuthUser{
				UserID:   userID.String(),
				Username: "alice",
				Role:     "admin",
			}, tt.kind)

			require.NoError(t, err)
			require.Equal(t, tt.kind, scope.Kind)
			require.Equal(t, userID, scope.UserID)
			require.Equal(t, "alice", scope.Username)
			require.Equal(t, "admin", scope.Role)
		})
	}
}

func TestFromAuthUserRejectsInvalidUserID(t *testing.T) {
	_, err := authscope.FromAuthUser(model.AuthUser{UserID: "not-a-uuid"}, accessscope.KindUser)
	require.Error(t, err)
}
