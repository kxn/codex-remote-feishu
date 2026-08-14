package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestResolveWorkspaceKey(t *testing.T) {
	want := testutil.WorkspacePath("data", "dl", "droid")
	input := " " + testutil.WorkspacePath("data", "dl", "work", "..", "droid") + "/ "
	if got := ResolveWorkspaceKey("", input); got != want {
		t.Fatalf("ResolveWorkspaceKey() = %q, want %q", got, want)
	}
	if got := ResolveWorkspaceKey("   "); got != "" {
		t.Fatalf("ResolveWorkspaceKey() = %q, want empty", got)
	}
}

func TestWorkspaceShortName(t *testing.T) {
	if got := WorkspaceShortName(testutil.WorkspacePath("data", "dl", "work", "..", "droid") + "/"); got != "droid" {
		t.Fatalf("WorkspaceShortName() = %q, want %q", got, "droid")
	}
	root, wantRoot := "/", "/"
	if runtime.GOOS == "windows" {
		root, wantRoot = `C:\`, "C:"
	}
	if got := WorkspaceShortName(root); got != wantRoot {
		t.Fatalf("WorkspaceShortName(root) = %q, want %q", got, wantRoot)
	}
}

func TestResolveWorkspaceRootOnHostResolvesSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	resolved, err := ResolveWorkspaceRootOnHost(filepath.Join(link, ".", ""))
	if err != nil {
		t.Fatalf("ResolveWorkspaceRootOnHost() error = %v", err)
	}
	if !testutil.SamePath(resolved, target) {
		t.Fatalf("ResolveWorkspaceRootOnHost() = %q, want %q", resolved, target)
	}
}

func TestResolveHeadlessResumeWorkspaceKeyKeepsWorkspaceRootForSubdirectoryCWD(t *testing.T) {
	got := ResolveHeadlessResumeWorkspaceKey(
		testutil.WorkspacePath("data", "dl", "repo"),
		testutil.WorkspacePath("data", "dl", "repo", "pkg"),
	)
	want := testutil.WorkspacePath("data", "dl", "repo")
	if got != want {
		t.Fatalf("ResolveHeadlessResumeWorkspaceKey() = %q, want %q", got, want)
	}
}

func TestResolveHeadlessResumeWorkspaceKeyUsesCWDWhenOutsideWorkspace(t *testing.T) {
	got := ResolveHeadlessResumeWorkspaceKey(
		testutil.WorkspacePath("data", "dl", "repo"),
		testutil.WorkspacePath("data", "dl", "other"),
	)
	want := testutil.WorkspacePath("data", "dl", "other")
	if got != want {
		t.Fatalf("ResolveHeadlessResumeWorkspaceKey() = %q, want %q", got, want)
	}
}

func TestResolveWorkspaceClaimKeyForGOOSWindowsKeepsSlashRootWorkspaceKeysLogical(t *testing.T) {
	if got := ResolveWorkspaceClaimKeyForGOOS("windows", "/data/dl/demo"); got != "/data/dl/demo" {
		t.Fatalf("ResolveWorkspaceClaimKeyForGOOS(windows) = %q, want /data/dl/demo", got)
	}
}

func TestShouldResolveWorkspacePathOnHostWindowsResolvesRelativeDotPaths(t *testing.T) {
	if !ShouldResolveWorkspacePathOnHost("windows", "./demo") {
		t.Fatal("expected relative dot path to resolve on host")
	}
}

func TestShouldResolveWorkspacePathOnHostWindowsResolvesVolumePaths(t *testing.T) {
	if !ShouldResolveWorkspacePathOnHost("windows", `D:\data\dl\demo`) {
		t.Fatal("expected volume path to resolve on host")
	}
}

func TestNormalizeWorkspaceKeyStripsWindowsExtendedPrefix(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"extended native drive", `\\?\C:\repo`, `C:/repo`},
		{"extended slash drive", `//?/C:/repo`, `C:/repo`},
		{"extended UNC native", `\\?\UNC\server\share\repo`, `//server/share/repo`},
		{"extended UNC slash", `//?/UNC/server/share/repo`, `//server/share/repo`},
		{"plain drive", `C:\repo`, `C:/repo`},
		{"plain UNC", `\\server\share\repo`, `//server/share/repo`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeWorkspaceKey(tc.input); got != tc.want {
				t.Fatalf("NormalizeWorkspaceKey(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestShouldResolveWorkspacePathOnHostWindowsRecognizesExtendedPrefixes(t *testing.T) {
	cases := []string{
		`\\?\C:\repo`,
		`//?/C:/repo`,
		`\\?\UNC\server\share`,
		`//?/UNC/server/share`,
	}
	for _, input := range cases {
		if !ShouldResolveWorkspacePathOnHost("windows", input) {
			t.Fatalf("expected extended-length path to resolve on host: %q", input)
		}
	}
}
