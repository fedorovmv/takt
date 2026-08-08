package application

import "context"

// detachedContext preserves request-scoped values while deliberately detaching
// a durable operation from caller cancellation. Detached work must continue
// after a CLI/RPC transport returns once its state has been committed.
func detachedContext(ctx context.Context) context.Context {
	if ctx == nil {
		return durableContext()
	}
	return context.WithoutCancel(ctx)
}

// durableContext is reserved for durable recovery/reconciliation work that has
// no live request context. Foreground use cases must propagate their caller ctx.
func durableContext() context.Context {
	return context.Background()
}
