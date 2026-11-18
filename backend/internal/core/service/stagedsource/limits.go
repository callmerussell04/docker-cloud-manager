package stagedsource

import (
	"io"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
)

// CheckHostDiskFloor rejects a write of projectedWriteBytes that would leave
// less than HostMinFreeDiskBytes free on the host disk.
func (s *Staging) CheckHostDiskFloor(projectedWriteBytes int64) error {
	return CheckHostDiskFloor(s.diskMetrics, s.hostDiskPath, s.cfg.Get().HostMinFreeDiskBytes, projectedWriteBytes)
}

// CheckHostDiskFloor is the projected host disk floor check for staging and
// container admission. Volume create and builds meet the floor through the
// admission ledger instead.
// It returns the disk floor rejection, a host-exhausted error, when the free
// bytes on path, less projectedWriteBytes, fall below minFreeBytes. Without
// metrics or a positive floor it checks nothing; an empty path means "/" and a
// negative write counts as none.
func CheckHostDiskFloor(metrics HostDiskMetrics, path string, minFreeBytes, projectedWriteBytes int64) error {
	if metrics == nil || minFreeBytes <= 0 {
		return nil
	}
	if path == "" {
		path = "/"
	}
	if projectedWriteBytes < 0 {
		projectedWriteBytes = 0
	}
	stats, err := metrics.GetDiskUsage(path)
	if err != nil {
		return err
	}
	if stats.FreeBytes-projectedWriteBytes < minFreeBytes {
		return apperrors.Exceeded(apperrors.LimitDiskFloor)
	}
	return nil
}

// CappedReader returns a reader of r that fails with ErrBadRequest once r holds
// more than maxBytes. Callers map that error to their own size-limit message.
func CappedReader(r io.Reader, maxBytes int64) io.Reader {
	return &cappedReader{r: r, remaining: maxBytes}
}

type cappedReader struct {
	r         io.Reader
	remaining int64
}

func (r *cappedReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		var one [1]byte
		n, err := r.r.Read(one[:])
		if n > 0 {
			return 0, apperrors.ErrBadRequest
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	return n, err
}
