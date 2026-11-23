package worktransport

// PreSendError proves the relay transport was never called. A registered
// command alone is not a write receipt; only this local boundary permits retry.
type PreSendError struct{ Err error }

func (e *PreSendError) Error() string           { return e.Err.Error() }
func (e *PreSendError) Unwrap() error           { return e.Err }
func (*PreSendError) NativeCommandUnsent() bool { return true }
