package privatestaging

import "context"

type contextKey struct{}

func WithStore(ctx context.Context, store *Store) context.Context {
	return context.WithValue(ctx, contextKey{}, store)
}
func FromContext(ctx context.Context) *Store {
	value, _ := ctx.Value(contextKey{}).(*Store)
	return value
}
