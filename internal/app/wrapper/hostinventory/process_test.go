package hostinventory

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestProcessIdentityHelper(t *testing.T) {
	if os.Getenv("HOST_IDENTITY_HELPER") == "1" {
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
}
func TestProcessExitRequiresActualIdentityAndKernelExit(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessIdentityHelper$")
	cmd.Env = append(os.Environ(), "HOST_IDENTITY_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	identity, err := CaptureProcess(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if WaitProcessExit(ctx, identity) == nil {
		t.Fatal("live process treated as exited")
	}
	wrong := identity
	wrong.StartIdentity = "different original process"
	if err = WaitProcessExit(context.Background(), wrong); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err = WaitProcessExit(ctx2, identity); err != nil {
		t.Fatal(err)
	}
}
