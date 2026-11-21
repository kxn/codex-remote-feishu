package hostinventory

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync/atomic"
)

type Snapshot struct {
	Platform         string `json:"platform"`
	GroupID          int    `json:"group_id,omitempty"`
	JobName          string `json:"job_name,omitempty"`
	RootPID          int    `json:"root_pid"`
	RootIdentity     string `json:"root_identity"`
	ProcessIDs       []int  `json:"process_ids"`
	Complete         bool   `json:"complete"`
	RootAlive        bool   `json:"root_alive"`
	ControlledStop   bool   `json:"controlled_stop"`
	RootExitVerified bool   `json:"root_exit_verified,omitempty"`
	WaitObserved     bool   `json:"wait_observed"`
	Reason           string `json:"reason,omitempty"`
}

type Scope struct {
	closed       atomic.Bool
	pid          int
	identity     string
	controlled   atomic.Bool
	waited       atomic.Bool
	exitVerified atomic.Bool
	platform     platformScope
}

type platformScope interface {
	Inspect(*Scope) Snapshot
	StopChildren(context.Context, *Scope) error
	Close() error
}

func Prepare(cmd *exec.Cmd)              { preparePlatform(cmd) }
func Bind(cmd *exec.Cmd) (*Scope, error) { return bindPlatform(cmd) }
func Restore(snapshot Snapshot) (*Scope, error) {
	scope, err := restorePlatform(snapshot)
	if err != nil {
		return nil, err
	}
	scope.controlled.Store(snapshot.ControlledStop)
	scope.waited.Store(snapshot.WaitObserved)
	scope.exitVerified.Store(snapshot.RootExitVerified)
	return scope, nil
}
func (s *Scope) MarkControlledStop() {
	if s != nil {
		s.controlled.Store(true)
	}
}
func (s *Scope) MarkWaitObserved() {
	if s != nil {
		s.waited.Store(true)
	}
}
func (s *Scope) Inspect() Snapshot {
	if s.closed.Load() {
		result := s.base()
		result.Reason = "native_scope_handle_closed"
		return result
	}
	return s.platform.Inspect(s)
}
func (s *Scope) StopChildren(ctx context.Context) error {
	if s.closed.Load() {
		return fmt.Errorf("native scope handle is closed")
	}
	return s.platform.StopChildren(ctx, s)
}
func (s *Scope) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	return s.platform.Close()
}
func (s *Scope) base() Snapshot {
	return Snapshot{RootExitVerified: s.exitVerified.Load(), RootPID: s.pid, RootIdentity: s.identity, ControlledStop: s.controlled.Load(), WaitObserved: s.waited.Load()}
}
func (s Snapshot) JSON() json.RawMessage { raw, _ := json.Marshal(s); return raw }

// ObserveRootExit uses the kernel identity proof after a daemon restart. It
// does not manufacture cmd.Wait or a requested stop. Complete group/job
// inspection is still required independently for descendants.
func (s *Scope) ObserveRootExit(ctx context.Context) error {
	if err := WaitProcessExit(ctx, ProcessIdentity{Platform: s.Inspect().Platform, PID: s.pid, StartIdentity: s.identity}); err != nil {
		return err
	}
	s.exitVerified.Store(true)
	return nil
}
