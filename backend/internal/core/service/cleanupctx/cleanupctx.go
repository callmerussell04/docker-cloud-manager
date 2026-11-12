// Package cleanupctx gives Core services the context their cleanup runs under.
package cleanupctx

import (
	"context"
	"time"
)

// timeout bounds one cleanup.
const timeout = 30 * time.Second

// Detached returns a context for cleanup that must outlive the request: it
// keeps ctx's values, such as scope and request ID, ignores ctx's cancellation
// and deadline, and ends after 30 seconds.
func Detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), timeout)
}
