package privatechat

// ExecutionGuard holds an exact current carrier lease while a transaction that
// may emit a native side effect commits. It must not perform network I/O.
type ExecutionGuard func(func() error) error

func (p *Processor) withSurfaceExecutionGuard(surface string, guard ExecutionGuard, fn func() error) error {
	if !p.app.Service().PrivateHasOnlineAttachedCarrier(surface) {
		return fn()
	}
	if guard == nil {
		return ErrDeferred
	}
	return guard(fn)
}
