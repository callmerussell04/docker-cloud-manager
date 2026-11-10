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
	// A lowered memory limit, or a CPU quota returned to standard, is not
	// raised again for this many passes.
	boostCooldownPasses = 6
)

// Fixed rules of CPU boost. They are not settings.
const (
	// A workload using more than this share of a CPU quota is close to it.
	// Only workloads close to their standard quota are inspected: they are
	// either candidates or already boosted.
	cpuBoostNearQuotaPercent = 85
	// Quotas are raised while host CPU load is below cpuBoostIdleHostPercent
	// and returned to standard while it is above cpuBoostBusyHostPercent.
	cpuBoostIdleHostPercent = 60
	cpuBoostBusyHostPercent = 85
)

// BorrowedMemoryStopReason is the last error of a container stopped while it
// used memory above its standard limit. It names no other user's usage.
const BorrowedMemoryStopReason = "stopped by the platform: the host ran short of memory, so the memory this container had borrowed above its standard limit was reclaimed"

// boostWorkload is one measured running workload: a user container, or a
// build container when build is set.
type boostWorkload struct {
	container model.Container
	build     *model.BuildContainer
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

	// cpuStandardQuota is the CPU quota create and start apply and
	// cpuCeiling the most CPU boost may raise it to, both per cpuPeriod.
	cpuStandardQuota int64
	cpuCeiling       int64
	cpuPeriod        int64
	// cpuUsed is the CPU the workload used since the previous pass, in
	// cores. cpuMeasured is unset on its first sample.
	cpuUsed     float64
	cpuMeasured bool
	// cpuCoolingDown is set while a recently lowered CPU quota may not grow.
	cpuCoolingDown bool
	// cpuQuota is its CPU quota now, per cpuQuotaPeriod, which differs from
	// cpuPeriod if the period setting changed since it started. Both are
	// read only for workloads close to their standard quota.
	cpuQuota       int64
	cpuQuotaPeriod int64
}

func (w boostWorkload) ownerID() uuid.UUID {
	if w.build != nil {
		return w.build.OwnerID
	}
	return w.container.OwnerID
}

func (w boostWorkload) dockerID() string {
	if w.build != nil {
		return w.build.DockerID
	}
	return w.container.DockerID
}

// poolUse is what the workload takes from the user memory pool, which is
// also what stopping it gives back. The pool already holds back a build's
// slot, so a build takes only what it uses above its reservation.
func (w boostWorkload) poolUse() int64 {
	if w.build != nil {
		return max(w.workingSet-w.reservation, 0)
	}
	return w.workingSet
}

// borrowed is the part of the current limit above the standard limit.
func (w boostWorkload) borrowed() int64 { return max(w.limit-w.standardLimit, 0) }

func (w boostWorkload) nearLimit() bool {
	return w.workingSet*100 > w.limit*boostNearLimitPercent
}

func (w boostWorkload) growCandidate() bool {
	return w.nearLimit() && w.limit < w.ceiling && !w.coolingDown
}

// memoryBoostInput is what one pass measured and the settings it runs under.
type memoryBoostInput struct {
	// pool is the memory user workloads may use together.
	pool      int64
	workloads []boostWorkload
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
		free -= w.poolUse()
	}
	return free
}

// allowedLimit is the highest memory limit the settings allow the workload
// now: its boost ceiling, or its standard limit while boost is off.
func (in memoryBoostInput) allowedLimit(w boostWorkload) int64 {
	if !in.boostEnabled {
		return w.standardLimit
	}
	return max(w.ceiling, w.standardLimit)
}

// growPossible reports whether the pass may grant anything, so the caller
// looks up quotas only when they can matter. Nothing is granted in a pass
// that lowers a limit.
func (in memoryBoostInput) growPossible() bool {
	return in.boostEnabled && in.free() > in.boostMinFree && len(shrinks(in.workloads, in.allowedLimit)) == 0
}

// growCandidateOwners are the owners whose quota the grow step reads.
func (in memoryBoostInput) growCandidateOwners() []uuid.UUID {
	var owners []uuid.UUID
	if !in.growPossible() {
		return nil
	}
	for _, w := range in.workloads {
		if w.growCandidate() && !slices.Contains(owners, w.ownerID()) {
			owners = append(owners, w.ownerID())
		}
	}
	return owners
}

// memoryLimitChange sets a workload's memory limit to limit.
type memoryLimitChange struct {
	workload boostWorkload
	limit    int64
}

// memoryStop is a workload to stop, with the reason it records.
type memoryStop struct {
	workload boostWorkload
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

// planMemoryBoost decides one pass: stop workloads when the pool runs short,
// lower boosted limits toward standard when it runs low, lower limits above
// what the settings now allow (a lowered boost ceiling, or boost turned off)
// at any time, and otherwise raise the limits of workloads close to them. It
// is the only place a running workload's memory limit is lowered, and it
// never lowers one below its working set plus a margin or below its standard
// limit.
func planMemoryBoost(in memoryBoostInput) memoryBoostPlan {
	plan := memoryBoostPlan{free: in.free()}
	plan.shortfall = in.minFree - plan.free
	if plan.shortfall > 0 {
		plan.stops = stopOrder(in)
	}
	if in.boostEnabled && plan.free < in.boostMinFree {
		plan.changes = shrinks(in.workloads, func(w boostWorkload) int64 { return w.standardLimit })
		return plan
	}
	plan.changes = shrinks(in.workloads, in.allowedLimit)
	if len(plan.changes) == 0 && in.growPossible() {
		plan.changes = grows(in, plan.free-in.boostMinFree)
	}
	return plan
}

// stopOrder lists the workloads that may be stopped, first those using
// borrowed memory, most first, then those over their reservation, largest
// excess first. Without boost nothing is counted as borrowed, so a workload
// still above its standard limit from before boost was turned off is
// listed as over its reservation. A workload at or below its reservation is
// never listed.
func stopOrder(in memoryBoostInput) []memoryStop {
	var borrowing, over []boostWorkload
	for _, w := range in.workloads {
		switch {
		case in.boostEnabled && w.workingSet > w.standardLimit:
			borrowing = append(borrowing, w)
		case w.workingSet > w.reservation:
			over = append(over, w)
		}
	}
	slices.SortStableFunc(borrowing, func(a, b boostWorkload) int {
		return cmp.Compare(b.workingSet-b.standardLimit, a.workingSet-a.standardLimit)
	})
	slices.SortStableFunc(over, func(a, b boostWorkload) int {
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

// shrinks lowers every limit above floor, which is never below the standard
// limit, toward it: to what the workload uses plus a margin, never below
// floor, skipping changes too small to matter.
func shrinks(workloads []boostWorkload, floor func(boostWorkload) int64) []memoryLimitChange {
	var changes []memoryLimitChange
	for _, w := range workloads {
		lowest := max(floor(w), w.standardLimit)
		if w.limit <= lowest {
			continue
		}
		margin := max(w.workingSet*shrinkMarginPercent/100, shrinkMinMarginBytes)
		target := max(lowest, w.workingSet+margin)
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
		borrowedByOwner[w.ownerID()] += w.borrowed()
	}
	type request struct {
		workload boostWorkload
		bytes    int64
	}
	var requests []request
	var requested, reserved int64
	for _, w := range in.workloads {
		if !w.growCandidate() {
			continue
		}
		step := w.standardLimit * boostStepPercent / 100
		headroom := in.quotas[w.ownerID()] - borrowedByOwner[w.ownerID()]
		bytes := min(step, w.ceiling-w.limit, headroom)
		if bytes <= 0 {
			continue
		}
		borrowedByOwner[w.ownerID()] += bytes
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

// nearCPUQuota reports whether the workload used more than
// cpuBoostNearQuotaPercent of quota since the previous pass.
func (w boostWorkload) nearCPUQuota(quota int64) bool {
	if !w.cpuMeasured || w.cpuPeriod <= 0 {
		return false
	}
	return w.cpuUsed*100 > float64(quota)/float64(w.cpuPeriod)*cpuBoostNearQuotaPercent
}

// cpuBoostActs reports whether host CPU load is low enough to raise quotas
// or high enough to take them back. In between nothing changes.
func cpuBoostActs(hostLoadPercent float64) bool {
	return hostLoadPercent < cpuBoostIdleHostPercent || hostLoadPercent > cpuBoostBusyHostPercent
}

// cpuQuotaChange sets a workload's CPU quota, and its CPU period when
// period is not zero.
type cpuQuotaChange struct {
	workload boostWorkload
	quota    int64
	period   int64
}

// planCPUBoost decides one pass of CPU boost for inspected workloads: on a
// busy host every raised quota returns to standard; on an idle host a
// workload close to its quota gets one standard quota more, within its
// ceiling, unless its quota was lowered within the cooldown, so a workload
// whose own use makes the host busy does not swing every pass. Lowering a
// quota only throttles, so it needs no usage check, and a quota set under
// another CPU period is compared in cores and returned with today's period;
// only a quota under today's period is raised. A workload without a quota of
// its own is left alone.
func planCPUBoost(hostLoadPercent float64, workloads []boostWorkload) []cpuQuotaChange {
	var changes []cpuQuotaChange
	for _, w := range workloads {
		samePeriod := w.cpuQuotaPeriod == w.cpuPeriod
		switch {
		case w.cpuQuota <= 0 || w.cpuQuotaPeriod <= 0:
		case hostLoadPercent > cpuBoostBusyHostPercent && w.cpuQuota*w.cpuPeriod > w.cpuStandardQuota*w.cpuQuotaPeriod:
			change := cpuQuotaChange{workload: w, quota: w.cpuStandardQuota}
			if !samePeriod {
				change.period = w.cpuPeriod
			}
			changes = append(changes, change)
		case hostLoadPercent < cpuBoostIdleHostPercent && samePeriod && !w.cpuCoolingDown && w.cpuQuota < w.cpuCeiling && w.nearCPUQuota(w.cpuQuota):
			changes = append(changes, cpuQuotaChange{workload: w, quota: min(w.cpuQuota+w.cpuStandardQuota, w.cpuCeiling)})
		}
	}
	return changes
}
