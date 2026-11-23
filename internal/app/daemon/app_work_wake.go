package daemon

// Wakeups only accelerate committed work. Durable polling remains the recovery
// path, and each lane retains a single consumer so callbacks cannot double-send.
func (r *teamRuntime) laneWake(name string) chan struct{} {
	r.laneMu.Lock()
	defer r.laneMu.Unlock()
	if r.laneWakes == nil {
		r.laneWakes = make(map[string]chan struct{})
	}
	if r.laneWakes[name] == nil {
		r.laneWakes[name] = make(chan struct{}, 1)
	}
	return r.laneWakes[name]
}

func (r *teamRuntime) wakeLane(name string) {
	select {
	case r.laneWake(name) <- struct{}{}:
	default:
	}
}
