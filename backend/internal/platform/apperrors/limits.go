package apperrors

import "errors"

// Limit names one limit a request can exceed: a user quota, a host capacity or
// a count limit. The names are the wire vocabulary: they cross gRPC as a
// status detail and reach the client as the `limit` field of an error
// response, so a name is never changed or reused.
type Limit string

const (
	LimitUserMemory Limit = "user_memory"
	LimitUserCPU    Limit = "user_cpu"
	LimitUserDisk   Limit = "user_disk"
	LimitHostMemory Limit = "host_memory"
	LimitHostCPU    Limit = "host_cpu"
	LimitDiskFloor  Limit = "disk_floor"

	LimitContainersPerUser        Limit = "containers_per_user"
	LimitVolumesPerUser           Limit = "volumes_per_user"
	LimitQueuedContainerCreates   Limit = "queued_container_creates"
	LimitQueuedBuilds             Limit = "queued_builds"
	LimitQueuedComposeDeployments Limit = "queued_compose_deployments"
	LimitStagedSourceBytes        Limit = "staged_source_bytes"
)

// limitRejection is what exceeding a limit answers: the error kind, which
// decides the client code and transport status, and a fixed message that
// names the limit and no value.
type limitRejection struct {
	limit   Limit
	kind    error
	message string
}

// limitRejections lists every limit once, in the order Limits returns.
var limitRejections = []limitRejection{
	{LimitUserMemory, ErrQuotaExceeded, "user memory quota exceeded"},
	{LimitUserCPU, ErrQuotaExceeded, "user CPU quota exceeded"},
	{LimitUserDisk, ErrQuotaExceeded, "user disk quota exceeded"},
	{LimitHostMemory, ErrHostExhausted, "host memory is fully reserved"},
	{LimitHostCPU, ErrHostExhausted, "host CPU is fully reserved"},
	{LimitDiskFloor, ErrHostExhausted, "host is low on free disk"},
	{LimitContainersPerUser, ErrLimitExceeded, "maximum number of containers reached"},
	{LimitVolumesPerUser, ErrLimitExceeded, "maximum number of volumes reached"},
	{LimitQueuedContainerCreates, ErrLimitExceeded, "maximum number of queued container creates reached"},
	{LimitQueuedBuilds, ErrLimitExceeded, "maximum number of queued builds reached"},
	{LimitQueuedComposeDeployments, ErrLimitExceeded, "maximum number of queued compose deployments reached"},
	{LimitStagedSourceBytes, ErrQuotaExceeded, "staged source quota exceeded"},
}

// Limits returns every limit name.
func Limits() []Limit {
	limits := make([]Limit, 0, len(limitRejections))
	for _, rejection := range limitRejections {
		limits = append(limits, rejection.limit)
	}
	return limits
}

// Known reports whether l is a limit name of the vocabulary.
func (l Limit) Known() bool {
	_, ok := l.rejection()
	return ok
}

func (l Limit) rejection() (limitRejection, bool) {
	for _, rejection := range limitRejections {
		if rejection.limit == l {
			return rejection, true
		}
	}
	return limitRejection{}, false
}

// Exceeded is the error of a request rejected by limit: the limit's kind, its
// fixed safe message and its name, which LimitOf reads back. A name outside
// the vocabulary names nothing and gives the generic exhausted kind.
func Exceeded(limit Limit) error {
	rejection, ok := limit.rejection()
	if !ok {
		return ErrResourceExhausted
	}
	return &Error{kind: rejection.kind, message: rejection.message, limit: limit}
}

// LimitOf returns the limit err was rejected by, or "" when it names none. It
// reads the same application error SafeMessage does: the outermost one in
// err's chain, so the message and the limit of a response always agree.
func LimitOf(err error) Limit {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.limit
	}
	return ""
}
