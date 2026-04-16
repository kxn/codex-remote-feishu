package orchestrator

import "testing"

func TestWorkspaceCreatePickerRootForGOOSWindowsUsesInitialPathVolume(t *testing.T) {
	got := workspaceCreatePickerRootForGOOS("windows", `E:\temp\demo`)
	if got != "E:/" {
		t.Fatalf("workspaceCreatePickerRootForGOOS(windows) = %q, want %q", got, "E:/")
	}
}

func TestWorkspaceCreatePickerRootForGOOSUnixUsesFilesystemRoot(t *testing.T) {
	got := workspaceCreatePickerRootForGOOS("linux", "/tmp/demo")
	if got != "/" {
		t.Fatalf("workspaceCreatePickerRootForGOOS(linux) = %q, want /", got)
	}
}

func TestShouldResolveWorkspacePathOnHostWindowsKeepsSlashRootWorkspaceKeysLogical(t *testing.T) {
	if shouldResolveWorkspacePathOnHost("windows", "/data/dl/demo") {
		t.Fatal("expected slash-root workspace key to stay logical on windows")
	}
}

func TestShouldResolveWorkspacePathOnHostWindowsResolvesRelativeDotPaths(t *testing.T) {
	if !shouldResolveWorkspacePathOnHost("windows", "./demo") {
		t.Fatal("expected relative dot path to resolve on host")
	}
}

func TestShouldResolveWorkspacePathOnHostWindowsResolvesVolumePaths(t *testing.T) {
	if !shouldResolveWorkspacePathOnHost("windows", `D:\data\dl\demo`) {
		t.Fatal("expected volume path to resolve on host")
	}
}
