package privatechat

import "context"

// beginProjection is called after acquiring the processor lock and any outer
// reference gate. Cancellation before this boundary prevents all mutation;
// once begun, local reduction and its checkpoint share one lifetime, while
// callers retain their original context for external I/O and delivery waits.
func beginProjection(ctx context.Context) (context.Context, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return context.WithoutCancel(ctx), nil
}
