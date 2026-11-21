package workingress

type PrivateReplyRoutingError struct{ Reason string }

func (e *PrivateReplyRoutingError) Error() string { return e.Reason }
