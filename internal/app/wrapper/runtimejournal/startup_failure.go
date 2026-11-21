package runtimejournal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// StartupFailure is recorded only before the wrapper opens its command
// channel and after all known native bootstrap processes have been reaped.
type StartupFailure struct {
	Owner              agentproto.ExecutionBinding       `json:"owner"`
	CarrierIncarnation string                            `json:"carrier_incarnation"`
	Process            agentproto.RuntimeProcessIdentity `json:"process"`
	Scopes             []hostinventory.Snapshot          `json:"native_scopes,omitempty"`
	Stage              string                            `json:"stage"`
	ObservedAt         time.Time                         `json:"observed_at"`
}

func startupFailurePath(snapshot agentproto.RuntimeLaunchSnapshot) string {
	return filepath.Join(filepath.Dir(snapshot.JournalPath), strconv.FormatInt(snapshot.Owner.SessionGeneration, 10), "startup-failure.json")
}

func ReadStartupFailure(snapshot agentproto.RuntimeLaunchSnapshot) (StartupFailure, error) {
	var receipt StartupFailure
	data, err := os.ReadFile(startupFailurePath(snapshot))
	if err != nil {
		return receipt, err
	}
	if err = json.Unmarshal(data, &receipt); err != nil {
		return receipt, err
	}
	return receipt, receipt.validate(snapshot)
}

func WriteStartupFailure(snapshot agentproto.RuntimeLaunchSnapshot, receipt StartupFailure) error {
	if err := receipt.validate(snapshot); err != nil {
		return err
	}
	path := startupFailurePath(snapshot)
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".startup-failure-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (r StartupFailure) validate(snapshot agentproto.RuntimeLaunchSnapshot) error {
	if r.Owner != snapshot.Owner || r.CarrierIncarnation != snapshot.CarrierIncarnation || r.Process.HostID != snapshot.HostID || r.Process.PID <= 0 || r.Process.StartIdentity == "" || r.Process.Platform == "" || r.ObservedAt.IsZero() {
		return fmt.Errorf("startup failure carrier identity mismatch")
	}
	switch r.Stage {
	case "wrapper_config", "work_state_init", "ensure_relay", "lifetime_setup", "native_bootstrap", "native_exit_before_ready":
	default:
		return fmt.Errorf("unknown startup failure stage")
	}
	for _, scope := range r.Scopes {
		if !scope.Complete || scope.RootAlive || !scope.WaitObserved || len(scope.ProcessIDs) != 0 || scope.Platform != r.Process.Platform || scope.RootIdentity == "" {
			return fmt.Errorf("startup failure has unverified native processes")
		}
	}
	return nil
}
