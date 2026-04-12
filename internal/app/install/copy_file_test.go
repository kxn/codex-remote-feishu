package install

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestCopyFileReplacesRunningExecutableAtomically(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires linux executable replacement semantics")
	}

	targetSource := "/bin/sleep"
	replacementSource := "/bin/echo"
	if _, err := os.Stat(targetSource); err != nil {
		t.Skipf("missing %s: %v", targetSource, err)
	}
	if _, err := os.Stat(replacementSource); err != nil {
		t.Skipf("missing %s: %v", replacementSource, err)
	}

	dir := t.TempDir()
	targetPath := filepath.Join(dir, "codex-remote")
	replacementPath := filepath.Join(dir, "replacement")
	if err := copyTestBinary(targetSource, targetPath); err != nil {
		t.Fatalf("copyTestBinary target: %v", err)
	}
	if err := copyTestBinary(replacementSource, replacementPath); err != nil {
		t.Fatalf("copyTestBinary replacement: %v", err)
	}

	cmd := exec.Command(targetPath, "2")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start target executable: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	time.Sleep(120 * time.Millisecond)

	if err := copyFile(replacementPath, targetPath); err != nil {
		t.Fatalf("copyFile while executable running: %v", err)
	}

	want, err := os.ReadFile(replacementPath)
	if err != nil {
		t.Fatalf("read replacement: %v", err)
	}
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("target binary mismatch after replacement")
	}
}

func copyTestBinary(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	info, err := source.Stat()
	if err != nil {
		return err
	}
	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer target.Close()
	if _, err := io.Copy(target, source); err != nil {
		return err
	}
	return target.Chmod(info.Mode().Perm())
}
