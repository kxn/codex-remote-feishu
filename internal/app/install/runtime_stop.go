package install

import (
	"context"
	"fmt"
	"os"
	"time"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type stopInstallStateOptions struct {
	StopDelay    time.Duration
	StopGrace    time.Duration
	PollInterval time.Duration
}

type runtimeControlHooks struct {
	Sleep            func(time.Duration)
	ReadPID          func(string) (int, error)
	TerminateProcess func(int, time.Duration) error
	RemoveFile       func(string) error
}

func defaultRuntimeControlHooks() runtimeControlHooks {
	return runtimeControlHooks{
		Sleep:            time.Sleep,
		ReadPID:          relayruntime.ReadPID,
		TerminateProcess: relayruntime.TerminateProcess,
		RemoveFile:       os.Remove,
	}
}

func stopInstallStateProcess(ctx context.Context, stateValue InstallState, paths relayruntime.Paths, opts stopInstallStateOptions, hooks runtimeControlHooks) error {
	if hooks.Sleep == nil {
		hooks.Sleep = time.Sleep
	}
	if hooks.ReadPID == nil {
		hooks.ReadPID = relayruntime.ReadPID
	}
	if hooks.TerminateProcess == nil {
		hooks.TerminateProcess = relayruntime.TerminateProcess
	}
	if hooks.RemoveFile == nil {
		hooks.RemoveFile = os.Remove
	}
	if opts.StopDelay > 0 {
		hooks.Sleep(opts.StopDelay)
	}
	if isManagedServiceManager(stateValue) {
		driver, ok := managedServiceDriverForManager(effectiveServiceManager(stateValue))
		if !ok {
			return fmt.Errorf("unsupported managed service manager %q", effectiveServiceManager(stateValue))
		}
		return driver.StopAndWait(ctx, stateValue, opts.StopGrace, opts.PollInterval)
	}

	pid, err := hooks.ReadPID(paths.PIDFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if pid <= 0 {
		return nil
	}
	if err := hooks.TerminateProcess(pid, opts.StopGrace); err != nil {
		return err
	}
	_ = hooks.RemoveFile(paths.PIDFile)
	_ = hooks.RemoveFile(paths.IdentityFile)
	return nil
}
