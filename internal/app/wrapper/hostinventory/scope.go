package hostinventory

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
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
	// Linux scope snapshots historically contain only /proc start ticks, not
	// the boot-qualified identity expected by WaitProcessExit. Comparing the
	// two would incorrectly classify a live process as exited.
	if runtime.GOOS == "linux" {
		return fmt.Errorf("Linux scope exit requires frozen carrier boot identity")
	}
	if err := WaitProcessExit(ctx, ProcessIdentity{Platform: s.Inspect().Platform, PID: s.pid, StartIdentity: s.identity}); err != nil {
		return err
	}
	s.exitVerified.Store(true)
	return nil
}

// ObserveRootExitWithCarrier reconstructs the Linux boot component omitted
// from historical scope snapshots from the exact wrapper identity. The scope
// still requires a separate complete, empty group/job inspection afterward.
func (s *Scope) ObserveRootExitWithCarrier(ctx context.Context, carrier ProcessIdentity) error {
	if s == nil || carrier.Platform != runtime.GOOS || carrier.StartIdentity == "" {
		return fmt.Errorf("carrier identity required")
	}
	identity := s.identity
	if runtime.GOOS == "linux" {
		boot, _, ok := strings.Cut(carrier.StartIdentity, ":")
		if !ok || boot == "" {
			return fmt.Errorf("carrier boot identity required")
		}
		identity = boot + ":" + identity
	}
	if err := WaitProcessExit(ctx, ProcessIdentity{Platform: carrier.Platform, PID: s.pid, StartIdentity: identity}); err != nil {
		return err
	}
	s.exitVerified.Store(true)
	return nil
}
