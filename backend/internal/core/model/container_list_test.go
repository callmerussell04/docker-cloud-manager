package model_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/stretchr/testify/require"
)

// Each filter's count is the containers of the classes it shows.
func TestContainerFilterCountsShownMatchesTheFilterClasses(t *testing.T) {
	var counts model.ContainerFilterCounts
	perClass := map[model.ContainerStatusClass]int{}
	for i, class := range model.ContainerStatusClasses {
		n := 1 << i
		counts.Add(class, n)
		perClass[class] = n
	}

	for _, filter := range []model.ContainerStatusFilter{
		model.ContainerFilterAll,
		model.ContainerFilterRunning,
		model.ContainerFilterStopped,
		model.ContainerFilterAttention,
	} {
		t.Run(string(filter), func(t *testing.T) {
			want := 0
			for _, class := range filter.Classes() {
				want += perClass[class]
			}
			require.Equal(t, want, counts.Shown(filter))
		})
	}
}
