package service

import (
	"cmp"
	"slices"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/google/uuid"
)

// Fixed rules of memory boost. They are not settings.
const (
	// A workload whose working set is above this share of its current limit
	// is close to it and may be given more.
	boostNearLimitPercent = 85
	// boostStepPercent of the standard limit is granted per pass.
	boostStepPercent = 50
	// A grant smaller than this is not worth a Docker update.
	boostMinGrantBytes = 16 << 20
	// A lowered limit keeps the larger of these above the working set.
	shrinkMarginPercent  = 10
	shrinkMinMarginBytes = 64 << 20
	// A shrink that lowers a limit by less than this share is skipped.
	shrinkMinChangePercent = 5
	// A lowered limit is not raised again for this many passes.
	boostCooldownPasses = 6
)

// BorrowedMemoryStopReason is the last error of a container stopped while it
// used memory above its standard limit. It names no other user's usage.
const BorrowedMemoryStopReason = "stopped by the platform: the host ran short of memory, so the memory this container had borrowed above its standard limit was reclaimed"

// memoryWorkload is one measured running workload.
type memoryWorkload struct {
	container model.Container
	// reservation is what admission promised; standardLimit is the limit
	// create and start apply; ceiling is the most a boost may raise it to.
	reservation   int64
	standardLimit int64
	ceiling       int64
	workingSet    int64
	// limit is the memory limit the workload runs with now.
	limit int64
	// coolingDown is set while a recently lowered limit may not grow.
	coolingDown bool
}

// borrowed is the part of the current limit above the standard limit.
func (w memoryWorkload) borrowed() int64 { return max(w.limit-w.standardLimit, 0) }

func (w memoryWorkload) nearLimit() bool {
	return w.workingSet*100 > w.limit*boostNearLimitPercent
}

func (w memoryWorkload) growCandidate() bool {
	return w.nearLimit() && w.limit < w.ceiling && !w.coolingDown
}

// memoryBoostInput is what one pass measured and the settings it runs under.
type memoryBoostInput struct {
	// pool is the memory user workloads may use together.
	pool      int64
	workloads []memoryWorkload
	// minFree is the free memory below which workloads are stopped.
	minFree int64
	// boostMinFree is the free memory a grant must leave and below which
	// boosted limits are lowered. It is only read when boostEnabled is set.
	boostMinFree int64
	boostEnabled bool
	// quotas is the memory quota of every owner with a grow candidate.
	quotas map[uuid.UUID]int64
}

func (in memoryBoostInput) free() int64 {
	free := in.pool
	for _, w := range in.workloads {
		free -= w.workingSet
	}
	return free
}

// growPossible reports whether the pass may grant anything, so the caller
// looks up quotas only when they can matter.
func (in memoryBoostInput) growPossible() bool {
	return in.boostEnabled && in.free() > in.boostMinFree
}

// growCandidateOwners are the owners whose quota the grow step reads.
func (in memoryBoostInput) growCandidateOwners() []uuid.UUID {
	var owners []uuid.UUID
	if !in.growPossible() {
		return nil
	}
	for _, w := range in.workloads {
		if w.growCandidate() && !slices.Contains(owners, w.container.OwnerID) {
			owners = append(owners, w.container.OwnerID)
		}
	}
	return owners
}

// memoryLimitChange sets a workload's memory limit to limit.
type memoryLimitChange struct {
	workload memoryWorkload
	limit    int64
}

// memoryStop is a workload to stop, with the reason it records.
type memoryStop struct {
	workload memoryWorkload
	reason   string
}

// memoryBoostPlan is what one pass does. Stops are in order and cover
// shortfall when each is applied; the caller stops them until it is covered,
// passing over any it cannot stop. A workload that is stopped is not also
// given a limit change.
type memoryBoostPlan struct {
	free      int64
	shortfall int64
	stops     []memoryStop
	changes   []memoryLimitChange
}

// planMemoryBoost decides one pass: lower boosted limits when the pool runs
// low, stop workloads when it runs short, and otherwise raise the limits of
// workloads close to them. It is the only place a running workload's memory
// limit is lowered, and it never lowers one below its working set plus a
// margin or below its standard limit.
func planMemoryBoost(in memoryBoostInput) memoryBoostPlan {
	plan := memoryBoostPlan{free: in.free()}
	plan.shortfall = in.minFree - plan.free
	if plan.shortfall > 0 {
		plan.stops = stopOrder(in)
	}
	if !in.boostEnabled {
		return plan
	}
	switch {
	case plan.free < in.boostMinFree:
		plan.changes = shrinks(in.workloads)
	case in.growPossible():
		plan.changes = grows(in, plan.free-in.boostMinFree)
	}
	return plan
}

// stopOrder lists the workloads that may be stopped, first those using
// borrowed memory, most first, then those over their reservation, largest
// excess first. Without boost nothing is counted as borrowed. A workload at
// or below its reservation is never listed.
func stopOrder(in memoryBoostInput) []memoryStop {
	var borrowing, over []memoryWorkload
	for _, w := range in.workloads {
		switch {
		case in.boostEnabled && w.workingSet > w.standardLimit:
			borrowing = append(borrowing, w)
		case w.workingSet > w.reservation:
			over = append(over, w)
		}
	}
	slices.SortStableFunc(borrowing, func(a, b memoryWorkload) int {
		return cmp.Compare(b.workingSet-b.standardLimit, a.workingSet-a.standardLimit)
	})
	slices.SortStableFunc(over, func(a, b memoryWorkload) int {
		return cmp.Compare(b.workingSet-b.reservation, a.workingSet-a.reservation)
	})
	stops := make([]memoryStop, 0, len(borrowing)+len(over))
	for _, w := range borrowing {
		stops = append(stops, memoryStop{workload: w, reason: BorrowedMemoryStopReason})
	}
	for _, w := range over {
		stops = append(stops, memoryStop{workload: w, reason: MemoryPressureStopReason})
	}
	return stops
}

// shrinks lowers every boosted limit to what its workload uses plus a margin,
// never below the standard limit, skipping changes too small to matter.
func shrinks(workloads []memoryWorkload) []memoryLimitChange {
	var changes []memoryLimitChange
	for _, w := range workloads {
		if w.limit <= w.standardLimit {
			continue
		}
		margin := max(w.workingSet*shrinkMarginPercent/100, shrinkMinMarginBytes)
		target := max(w.standardLimit, w.workingSet+margin)
		if (w.limit-target)*100 < w.limit*shrinkMinChangePercent {
			continue
		}
		changes = append(changes, memoryLimitChange{workload: w, limit: target})
	}
	return changes
}

// grows raises the limits of workloads close to them by one step, within
// their ceiling, their owner's quota for borrowed memory and budget. When
// the requests exceed budget it is split in proportion to reservations.
func grows(in memoryBoostInput, budget int64) []memoryLimitChange {
	borrowedByOwner := map[uuid.UUID]int64{}
	for _, w := range in.workloads {
		borrowedByOwner[w.container.OwnerID] += w.borrowed()
	}
	type request struct {
		workload memoryWorkload
		bytes    int64
	}
	var requests []request
	var requested, reserved int64
	for _, w := range in.workloads {
		if !w.growCandidate() {
			continue
		}
		step := w.standardLimit * boostStepPercent / 100
		headroom := in.quotas[w.container.OwnerID] - borrowedByOwner[w.container.OwnerID]
		bytes := min(step, w.ceiling-w.limit, headroom)
		if bytes <= 0 {
			continue
		}
		borrowedByOwner[w.container.OwnerID] += bytes
		requests = append(requests, request{workload: w, bytes: bytes})
		requested += bytes
		reserved += w.reservation
	}
	var changes []memoryLimitChange
	for _, r := range requests {
		grant := r.bytes
		if requested > budget {
			// float64 keeps budget times reservation from overflowing.
			share := int64(0)
			if reserved > 0 {
				share = int64(float64(budget) * float64(r.workload.reservation) / float64(reserved))
			}
			grant = min(grant, share)
		}
		if grant < boostMinGrantBytes {
			continue
		}
		changes = append(changes, memoryLimitChange{workload: r.workload, limit: r.workload.limit + grant})
	}
	return changes
}
