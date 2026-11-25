package repository_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
)

const listTestMiB = 1 << 20

// listFixture is one owner's containers, another owner's container and a
// project, saved through the repository.
type listFixture struct {
	repo      *repository.ContainerRepository
	ownerID   uuid.UUID
	otherID   uuid.UUID
	projectID uuid.UUID
}

type listRow struct {
	name, image, status, domain string
	memoryMiB                   int64
	age                         time.Duration
	inProject                   bool
}

func newListFixture(t *testing.T) listFixture {
	t.Helper()
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	f := listFixture{
		repo:      repository.NewContainerRepository(db, db),
		ownerID:   uuid.New(),
		otherID:   uuid.New(),
		projectID: uuid.New(),
	}
	require.NoError(t, repository.NewProjectRepository(db).Save(ctx, model.Project{ID: f.projectID, OwnerID: f.ownerID, Name: "shop", Status: model.ProjectStatusRunning}))
	for _, row := range []listRow{
		{name: "web-10", image: "nginx:1", status: model.ContainerStatusRunning, domain: "shop", memoryMiB: 512, age: 5 * time.Hour},
		{name: "web-2", image: "nginx:2", status: model.ContainerStatusRunning, memoryMiB: 128, age: 4 * time.Hour, inProject: true},
		{name: "Api", image: "acme/api:v1", status: model.ContainerStatusError, domain: "api", memoryMiB: 256, age: 3 * time.Hour},
		{name: "db", image: "postgres:17", status: model.ContainerStatusExited, memoryMiB: 1024, age: 2 * time.Hour},
		{name: "cache", image: "redis:7", status: model.ContainerStatusCreated, memoryMiB: 64, age: time.Hour, inProject: true},
		{name: "worker", image: "acme/worker_1:latest", status: model.ContainerStatusStarting, memoryMiB: 300, age: 6 * time.Hour},
		{name: "lost", image: "busybox", status: model.ContainerStatusMissing, memoryMiB: 32, age: 7 * time.Hour},
	} {
		f.save(t, db, f.ownerID, row)
	}
	f.save(t, db, f.otherID, listRow{name: "other", image: "nginx:1", status: model.ContainerStatusError, memoryMiB: 128, age: time.Minute})
	return f
}

func (f listFixture) save(t *testing.T, db *pgxpool.Pool, ownerID uuid.UUID, row listRow) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	c := model.Container{
		ID:                    uuid.New(),
		OwnerID:               ownerID,
		Name:                  row.name,
		ImageTag:              row.image,
		Status:                row.status,
		DomainPrefix:          row.domain,
		BaseMemoryReservation: row.memoryMiB * listTestMiB,
	}
	if row.inProject {
		c.ProjectID = &f.projectID
	}
	require.NoError(t, f.repo.Save(ctx, c))
	// Creation time is the database's; age the row on its clock.
	_, err := db.Exec(ctx, `UPDATE containers SET created_at = NOW() - make_interval(secs => $2) WHERE id = $1`, c.ID, row.age.Seconds())
	require.NoError(t, err)
	return c.ID
}

func (f listFixture) list(t *testing.T, opts model.ContainerListOptions) model.ContainerPage {
	t.Helper()
	filter, err := opts.Normalize()
	require.NoError(t, err)
	opts.ContainerListFilter = filter
	page, err := f.repo.List(context.Background(), opts)
	require.NoError(t, err)
	return page
}

func (f listFixture) own(filter model.ContainerListFilter) model.ContainerListOptions {
	return model.ContainerListOptions{ListOptions: model.ListOptions{OwnerID: &f.ownerID}, ContainerListFilter: filter}
}

func names(page model.ContainerPage) []string {
	result := make([]string, 0, len(page.Containers))
	for _, c := range page.Containers {
		result = append(result, c.Name)
	}
	return result
}

func TestContainerRepositoryListFiltersByStatus(t *testing.T) {
	f := newListFixture(t)
	ownCounts := model.ContainerFilterCounts{All: 7, Running: 2, Stopped: 2, Attention: 3}
	tests := []struct {
		name   string
		filter model.ContainerStatusFilter
		want   []string
	}{
		{name: "all", filter: model.ContainerFilterAll, want: []string{"Api", "lost", "worker", "web-2", "web-10", "cache", "db"}},
		{name: "running", filter: model.ContainerFilterRunning, want: []string{"web-2", "web-10"}},
		{name: "stopped is created and exited", filter: model.ContainerFilterStopped, want: []string{"cache", "db"}},
		{name: "attention is failing and busy", filter: model.ContainerFilterAttention, want: []string{"Api", "lost", "worker"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page := f.list(t, f.own(model.ContainerListFilter{Status: tc.filter}))
			require.Equal(t, tc.want, names(page))
			require.Equal(t, len(tc.want), page.Total)
			require.Equal(t, ownCounts, page.Counts, "counts never follow the status filter")
		})
	}
}

func TestContainerRepositoryListStartsWithFailingContainers(t *testing.T) {
	f := newListFixture(t)
	page := f.list(t, f.own(model.ContainerListFilter{}))
	require.Equal(t, []string{"Api", "lost", "worker", "web-2", "web-10", "cache", "db"}, names(page))
	require.Equal(t, 7, page.Total)
}

func TestContainerRepositoryListSortsByEveryColumn(t *testing.T) {
	f := newListFixture(t)
	tests := []struct {
		sort       model.ContainerSortKey
		ascending  []string
		descending []string
	}{
		{
			// Classes reverse; names keep their order within a class.
			sort:       model.ContainerSortStatus,
			ascending:  []string{"Api", "lost", "worker", "web-2", "web-10", "cache", "db"},
			descending: []string{"db", "cache", "web-2", "web-10", "worker", "Api", "lost"},
		},
		{
			// Natural and case-insensitive: web-2 before web-10, Api first.
			sort:       model.ContainerSortName,
			ascending:  []string{"Api", "cache", "db", "lost", "web-2", "web-10", "worker"},
			descending: []string{"worker", "web-10", "web-2", "lost", "db", "cache", "Api"},
		},
		{
			// Unrouted containers come last in ascending order.
			sort:       model.ContainerSortAddress,
			ascending:  []string{"Api", "web-10", "cache", "db", "lost", "web-2", "worker"},
			descending: []string{"cache", "db", "lost", "web-2", "worker", "web-10", "Api"},
		},
		{
			sort:       model.ContainerSortMemory,
			ascending:  []string{"lost", "cache", "web-2", "Api", "worker", "web-10", "db"},
			descending: []string{"db", "web-10", "worker", "Api", "web-2", "cache", "lost"},
		},
		{
			// Ascending creation is the newest first, as the age column reads.
			sort:       model.ContainerSortCreated,
			ascending:  []string{"cache", "db", "Api", "web-2", "web-10", "worker", "lost"},
			descending: []string{"lost", "worker", "web-10", "web-2", "Api", "db", "cache"},
		},
	}
	for _, tc := range tests {
		t.Run(string(tc.sort), func(t *testing.T) {
			asc := f.list(t, f.own(model.ContainerListFilter{Sort: tc.sort, Order: model.SortAscending}))
			require.Equal(t, tc.ascending, names(asc))
			desc := f.list(t, f.own(model.ContainerListFilter{Sort: tc.sort, Order: model.SortDescending}))
			require.Equal(t, tc.descending, names(desc))
		})
	}
}

func TestContainerRepositoryListBreaksTiesByIDAcrossPages(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	tied := listFixture{repo: repository.NewContainerRepository(db, db)}
	var ids []string
	// Three owners' containers share a name, a status and a reservation.
	for range 3 {
		ids = append(ids, tied.save(t, db, uuid.New(), listRow{name: "tie", image: "tie:1", status: model.ContainerStatusRunning, memoryMiB: 64, age: time.Hour}).String())
	}
	sort.Strings(ids)

	for _, order := range []model.SortOrder{model.SortAscending, model.SortDescending} {
		t.Run(string(order), func(t *testing.T) {
			var got []string
			for offset := range 3 {
				page := tied.list(t, model.ContainerListOptions{
					ListOptions:         model.ListOptions{Limit: 1, Offset: offset},
					ContainerListFilter: model.ContainerListFilter{Sort: model.ContainerSortMemory, Order: order},
				})
				require.Len(t, page.Containers, 1)
				require.Equal(t, 3, page.Total)
				got = append(got, page.Containers[0].ID.String())
			}
			require.Equal(t, ids, got, "ties keep id order whichever way the column sorts")
		})
	}
}

func TestContainerRepositoryListSearchesNameAndImageLiterally(t *testing.T) {
	f := newListFixture(t)
	tests := []struct {
		name   string
		search string
		want   []string
		counts model.ContainerFilterCounts
	}{
		{name: "image, ignoring case", search: "NGINX", want: []string{"web-2", "web-10"}, counts: model.ContainerFilterCounts{All: 2, Running: 2}},
		{name: "name or image", search: "api", want: []string{"Api"}, counts: model.ContainerFilterCounts{All: 1, Attention: 1}},
		{name: "percent is literal", search: "%", want: []string{}, counts: model.ContainerFilterCounts{}},
		{name: "underscore is literal", search: "r_1", want: []string{"worker"}, counts: model.ContainerFilterCounts{All: 1, Attention: 1}},
		{name: "backslash is literal", search: `\`, want: []string{}, counts: model.ContainerFilterCounts{}},
		{name: "trimmed", search: "  cache  ", want: []string{"cache"}, counts: model.ContainerFilterCounts{All: 1, Stopped: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page := f.list(t, f.own(model.ContainerListFilter{Search: tc.search}))
			require.Equal(t, tc.want, names(page))
			require.Equal(t, tc.counts, page.Counts, "counts follow the search")
		})
	}
}

func TestContainerRepositoryListCountsFollowTheProject(t *testing.T) {
	f := newListFixture(t)
	opts := f.own(model.ContainerListFilter{Status: model.ContainerFilterRunning})
	opts.ProjectID = &f.projectID
	page := f.list(t, opts)
	require.Equal(t, []string{"web-2"}, names(page))
	require.Equal(t, 1, page.Total)
	require.Equal(t, model.ContainerFilterCounts{All: 2, Running: 1, Stopped: 1}, page.Counts)
}

func TestContainerRepositoryListIsScopedToTheOwner(t *testing.T) {
	f := newListFixture(t)

	other := f.list(t, model.ContainerListOptions{ListOptions: model.ListOptions{OwnerID: &f.otherID}})
	require.Equal(t, []string{"other"}, names(other))
	require.Equal(t, model.ContainerFilterCounts{All: 1, Attention: 1}, other.Counts)

	searched := f.list(t, f.own(model.ContainerListFilter{Search: "other"}))
	require.Empty(t, searched.Containers, "a user never finds another owner's container")
	require.Equal(t, model.ContainerFilterCounts{}, searched.Counts)

	everyone := f.list(t, model.ContainerListOptions{})
	require.Equal(t, []string{"Api", "lost", "other", "worker", "web-2", "web-10", "cache", "db"}, names(everyone))
	require.Equal(t, model.ContainerFilterCounts{All: 8, Running: 2, Stopped: 2, Attention: 4}, everyone.Counts)
}

func TestContainerRepositoryListPagesAfterFiltering(t *testing.T) {
	f := newListFixture(t)
	opts := f.own(model.ContainerListFilter{Status: model.ContainerFilterAttention})
	opts.Limit, opts.Offset = 2, 2
	page := f.list(t, opts)
	require.Equal(t, []string{"worker"}, names(page))
	require.Equal(t, 3, page.Total)

	opts.Offset = 4
	past := f.list(t, opts)
	require.Empty(t, past.Containers)
	require.Equal(t, 3, past.Total, "a page past the end still says how many match")
}
