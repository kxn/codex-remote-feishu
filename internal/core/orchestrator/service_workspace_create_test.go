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

func TestWorkspacePickerPathsForGOOSWindowsUsesVolumeRootAsInitialWhenWorkspaceEmpty(t *testing.T) {
	root, initial := workspacePickerPathsForGOOS("windows", "", `E:\Users\demo`)
	if root != "E:/" || initial != "E:/" {
		t.Fatalf("workspacePickerPathsForGOOS(windows, empty) = (%q, %q), want (%q, %q)", root, initial, "E:/", "E:/")
	}
}

func TestWorkspacePickerPathsForGOOSUnixUsesFilesystemRootAsInitialWhenWorkspaceEmpty(t *testing.T) {
	root, initial := workspacePickerPathsForGOOS("linux", "", "")
	if root != "/" || initial != "/" {
		t.Fatalf("workspacePickerPathsForGOOS(linux, empty) = (%q, %q), want (%q, %q)", root, initial, "/", "/")
	}
}
