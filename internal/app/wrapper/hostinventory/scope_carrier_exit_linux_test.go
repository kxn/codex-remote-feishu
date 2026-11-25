//go:build linux

package hostinventory

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestScopeRootExitUsesFrozenCarrierBootIdentity(t *testing.T) {
	child := exec.Command("sleep", "30")
	Prepare(child)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	scope, err := Bind(child)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	carrier, err := CaptureProcess(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err = scope.ObserveRootExit(context.Background()); err == nil {
		t.Fatal("bare legacy Linux scope falsely proved exit")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if err = scope.ObserveRootExitWithCarrier(ctx, carrier); err == nil {
		t.Fatal("live native root treated as exited")
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	if err = scope.ObserveRootExitWithCarrier(context.Background(), carrier); err != nil {
		t.Fatal(err)
	}
	state := scope.Inspect()
	if !state.Complete || state.RootAlive || len(state.ProcessIDs) != 0 {
		t.Fatalf("scope not proven stopped: %+v", state)
	}
}
