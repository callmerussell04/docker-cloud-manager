package model

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ContainerStatusClass is what a container's status means to the containers
// list. The values are the list's failing-first order: failing, busy,
// running, created, exited.
type ContainerStatusClass int

const (
	ContainerClassFailing ContainerStatusClass = iota
	ContainerClassBusy
	ContainerClassRunning
	ContainerClassCreated
	ContainerClassExited
)

// ContainerStatusClasses are every class in the list's failing-first order.
var ContainerStatusClasses = []ContainerStatusClass{
	ContainerClassFailing,
	ContainerClassBusy,
	ContainerClassRunning,
	ContainerClassCreated,
	ContainerClassExited,
}

// containerClassStatuses lists the statuses of every class but busy, which
// holds every other status, including one this list does not know.
var containerClassStatuses = map[ContainerStatusClass][]string{
	ContainerClassFailing: {ContainerStatusError, ContainerStatusMissing},
	ContainerClassRunning: {ContainerStatusRunning},
	ContainerClassCreated: {ContainerStatusCreated},
	ContainerClassExited:  {ContainerStatusExited},
}

// ContainerClassStatuses is the statuses of a class; it is nil for busy,
// which is every status no other class lists.
func ContainerClassStatuses(class ContainerStatusClass) []string {
	return containerClassStatuses[class]
}

// ContainerStatusFilter is the containers list's status filter.
type ContainerStatusFilter string

const (
	ContainerFilterAll       ContainerStatusFilter = "all"
	ContainerFilterRunning   ContainerStatusFilter = "running"
	ContainerFilterStopped   ContainerStatusFilter = "stopped"
	ContainerFilterAttention ContainerStatusFilter = "attention"
)

var containerFilterClasses = map[ContainerStatusFilter][]ContainerStatusClass{
	ContainerFilterAll:       ContainerStatusClasses,
	ContainerFilterRunning:   {ContainerClassRunning},
	ContainerFilterStopped:   {ContainerClassCreated, ContainerClassExited},
	ContainerFilterAttention: {ContainerClassFailing, ContainerClassBusy},
}

// Classes is the classes a filter shows: Running the running containers,
// Stopped those created or exited, Attention those failing or busy.
func (f ContainerStatusFilter) Classes() []ContainerStatusClass {
	return containerFilterClasses[f]
}

// Shows reports whether a filter shows containers of a class.
func (f ContainerStatusFilter) Shows(class ContainerStatusClass) bool {
	for _, c := range f.Classes() {
		if c == class {
			return true
		}
	}
	return false
}

// ContainerFilterCounts is how many containers each status filter shows.
type ContainerFilterCounts struct {
	All       int
	Running   int
	Stopped   int
	Attention int
}

// Add counts n containers of a class under every filter that shows it.
func (c *ContainerFilterCounts) Add(class ContainerStatusClass, n int) {
	c.All += n
	switch {
	case ContainerFilterRunning.Shows(class):
		c.Running += n
	case ContainerFilterStopped.Shows(class):
		c.Stopped += n
	case ContainerFilterAttention.Shows(class):
		c.Attention += n
	}
}

// Shown is how many containers a filter shows; an unknown filter shows all.
func (c ContainerFilterCounts) Shown(f ContainerStatusFilter) int {
	switch f {
	case ContainerFilterRunning:
		return c.Running
	case ContainerFilterStopped:
		return c.Stopped
	case ContainerFilterAttention:
		return c.Attention
	default:
		return c.All
	}
}

// ContainerSortKey is a containers list column.
type ContainerSortKey string

const (
	ContainerSortName    ContainerSortKey = "name"
	ContainerSortStatus  ContainerSortKey = "status"
	ContainerSortAddress ContainerSortKey = "address"
	ContainerSortMemory  ContainerSortKey = "memory"
	ContainerSortCreated ContainerSortKey = "created"
)

// SortOrder is the direction of a sorted column.
type SortOrder string

const (
	SortAscending  SortOrder = "asc"
	SortDescending SortOrder = "desc"
)

// ContainerSearchMaxLength is the longest search, in characters, after
// trimming.
const ContainerSearchMaxLength = 100

var (
	errUnknownContainerFilter = errors.New("status must be one of: all, running, stopped, attention")
	errUnknownContainerSort   = errors.New("sort must be one of: name, status, address, memory, created")
	errUnknownSortOrder       = errors.New("order must be asc or desc")
	errContainerSearchTooLong = errors.New("search must be at most 100 characters")
)

// ContainerListFilter is how the containers list is narrowed and ordered.
// Its zero value is every container, failing first, with no search.
type ContainerListFilter struct {
	Status ContainerStatusFilter
	Sort   ContainerSortKey
	Order  SortOrder
	// Search finds a container by a case-insensitive substring of its name
	// or image tag, taken literally.
	Search string
}

// Normalize fills in the defaults (all, status, ascending), trims the
// search, and refuses an unknown filter, sort key or order and a search
// over ContainerSearchMaxLength. Its errors never repeat the value.
func (f ContainerListFilter) Normalize() (ContainerListFilter, error) {
	if f.Status == "" {
		f.Status = ContainerFilterAll
	}
	if f.Sort == "" {
		f.Sort = ContainerSortStatus
	}
	if f.Order == "" {
		f.Order = SortAscending
	}
	f.Search = strings.TrimSpace(f.Search)
	if _, ok := containerFilterClasses[f.Status]; !ok {
		return ContainerListFilter{}, errUnknownContainerFilter
	}
	switch f.Sort {
	case ContainerSortName, ContainerSortStatus, ContainerSortAddress, ContainerSortMemory, ContainerSortCreated:
	default:
		return ContainerListFilter{}, errUnknownContainerSort
	}
	if f.Order != SortAscending && f.Order != SortDescending {
		return ContainerListFilter{}, errUnknownSortOrder
	}
	if utf8.RuneCountInString(f.Search) > ContainerSearchMaxLength {
		return ContainerListFilter{}, errContainerSearchTooLong
	}
	return f, nil
}

// ContainerListQuery is what a caller asks of the containers list; whose
// containers it covers comes from the request's access scope.
type ContainerListQuery struct {
	ProjectID *uuid.UUID
	Limit     int
	Offset    int
	ContainerListFilter
}

// ContainerListOptions is a containers list read from the repository: the
// owner and project, the page, and the filter, sort and search. The repository
// expects a normalized filter.
type ContainerListOptions struct {
	ListOptions
	ContainerListFilter
}

// ContainerPage is one page of the containers list. Total is the number of
// containers matching every filter, status included; Counts covers the owner,
// the project and the search, but not the status filter.
type ContainerPage struct {
	Containers []Container
	Total      int
	Counts     ContainerFilterCounts
}
