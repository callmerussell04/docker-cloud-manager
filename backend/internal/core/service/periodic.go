package service

import (
	"context"
	"time"
)

// runPeriodically calls tick once per interval until ctx is canceled, waiting
// before the first call. The interval is read before each wait so runtime
// config changes apply, and is floored at one second. Cancellation is observed
// between ticks; a tick in progress receives the canceled context.
func runPeriodically(ctx context.Context, intervalSeconds func() int64, tick func(context.Context)) {
	for {
		timer := time.NewTimer(max(time.Duration(intervalSeconds())*time.Second, time.Second))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			tick(ctx)
		}
	}
}
