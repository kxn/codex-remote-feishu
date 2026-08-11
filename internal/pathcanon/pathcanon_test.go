package pathcanon

import "testing"

func TestStripExtendedPrefix(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"native drive", `\\?\C:\repo`, `C:\repo`},
		{"slash drive", `//?/C:/repo`, `C:/repo`},
		{"native UNC", `\\?\UNC\server\share`, `\\server\share`},
		{"slash UNC", `//?/UNC/server/share`, `//server/share`},
		{"plain drive", `C:\repo`, `C:\repo`},
		{"plain slash drive", `C:/repo`, `C:/repo`},
		{"plain UNC", `\\server\share`, `\\server\share`},
		{"unix path", `/home/user/repo`, `/home/user/repo`},
		{"no prefix", `repo`, `repo`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripExtendedPrefix(tc.input); got != tc.want {
				t.Fatalf("stripExtendedPrefix(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestWorkspaceKeyForGOOSWindows(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"extended native drive", `\\?\C:\repo`, `C:/repo`},
		{"extended slash drive", `//?/C:/repo`, `C:/repo`},
		{"plain native drive", `C:\repo`, `C:/repo`},
		{"plain slash drive", `C:/repo`, `C:/repo`},
		{"drive with trailing slash", `C:/repo/`, `C:/repo`},
		{"drive with dot segments", `C:\repo\work\..\src`, `C:/repo/src`},
		{"drive with dot", `C:\repo\.`, `C:/repo`},
		{"drive root only", `C:\`, `C:`},
		{"extended UNC native", `\\?\UNC\server\share\repo`, `//server/share/repo`},
		{"extended UNC slash", `//?/UNC/server/share/repo`, `//server/share/repo`},
		{"plain UNC native", `\\server\share\repo`, `//server/share/repo`},
		{"plain UNC slash", `//server/share/repo`, `//server/share/repo`},
		{"UNC root", `\\server\share`, `//server/share`},
		{"mixed separators", `C:\repo/sub`, `C:/repo/sub`},
		{"relative path", `work\..\src`, `src`},
		{"leading dotdot", `..\repo`, `../repo`},
		{"unix absolute", `/home/user/repo`, `/home/user/repo`},
		{"empty", `  `, ``},
		{"dot", `.`, ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WorkspaceKeyForGOOS("windows", tc.input); got != tc.want {
				t.Fatalf("WorkspaceKeyForGOOS(windows, %q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestWorkspaceKeyForGOOSUnix(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"unix absolute", `/home/user/repo`, `/home/user/repo`},
		{"unix clean", `/home/user/work/../repo`, `/home/user/repo`},
		{"extended drive still stripped", `//?/C:/repo`, `C:/repo`},
		{"extended UNC still stripped", `\\?\UNC\server\share`, `//server/share`},
		{"windows drive form", `C:\repo`, `C:/repo`},
		{"relative", `work/../src`, `src`},
		{"empty", ` `, ``},
		{"dot", `.`, ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WorkspaceKeyForGOOS("linux", tc.input); got != tc.want {
				t.Fatalf("WorkspaceKeyForGOOS(linux, %q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNativeForGOOSWindows(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"extended native drive", `\\?\C:\repo`, `C:\repo`},
		{"extended slash drive", `//?/C:/repo`, `C:\repo`},
		{"plain slash drive", `C:/repo`, `C:\repo`},
		{"extended UNC", `\\?\UNC\server\share\repo`, `\\server\share\repo`},
		{"slash UNC", `//?/UNC/server/share/repo`, `\\server\share\repo`},
		{"plain UNC", `//server/share/repo`, `\\server\share\repo`},
		{"mixed separators", `C:/repo/sub`, `C:\repo\sub`},
		{"dot segments", `C:\repo\work\..\src`, `C:\repo\src`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NativeForGOOS("windows", tc.input); got != tc.want {
				t.Fatalf("NativeForGOOS(windows, %q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCompareKeyForGOOS(t *testing.T) {
	cases := []struct {
		name, goos, input, want string
	}{
		{"windows drive case", "windows", `C:\Repo`, `c:/repo`},
		{"windows slash drive case", "windows", `//?/C:/Repo`, `c:/repo`},
		{"windows UNC case", "windows", `\\Server\Share\Repo`, `//server/share/repo`},
		{"unix stays case-sensitive", "linux", `/Home/Repo`, `/Home/Repo`},
		{"linux windows-like drive lowered", "linux", `C:\Repo`, `c:/repo`},
		{"linux windows-like UNC lowered", "linux", `//Server/Share`, `//server/share`},
		{"empty", "windows", ` `, ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompareKeyForGOOS(tc.goos, tc.input); got != tc.want {
				t.Fatalf("CompareKeyForGOOS(%s, %q) = %q, want %q", tc.goos, tc.input, got, tc.want)
			}
		})
	}
}

func TestCompareKeyConsistency(t *testing.T) {
	// Extended and plain forms of the same Windows path must compare equal.
	pairs := [][2]string{
		{`\\?\C:\repo`, `C:\repo`},
		{`//?/C:/repo`, `C:/repo`},
		{`//?/C:/Repo`, `c:/repo`},
		{`\\?\UNC\server\share\repo`, `\\server\share\repo`},
		{`//?/UNC/server/share/repo`, `//server/share/repo`},
	}
	for _, pair := range pairs {
		if CompareKeyForGOOS("windows", pair[0]) != CompareKeyForGOOS("windows", pair[1]) {
			t.Fatalf("CompareKey mismatch: %q vs %q", pair[0], pair[1])
		}
	}
}

func TestIsWindowsLikePath(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{`C:\repo`, true},
		{`c:/repo`, true},
		{`\\server\share`, true},
		{`//server/share`, true},
		{`\\?\C:\repo`, true},
		{`//?/C:/repo`, true},
		{`/home/user`, false},
		{`repo`, false},
	}
	for _, tc := range cases {
		if got := IsWindowsLikePath(tc.input); got != tc.want {
			t.Fatalf("IsWindowsLikePath(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestContainmentForGOOS(t *testing.T) {
	cases := []struct {
		name, goos, root, target string
		want                     bool
	}{
		{"drive descendant", "windows", `C:\repo`, `C:\repo\pkg`, true},
		{"drive self", "windows", `C:\repo`, `C:\repo`, true},
		{"drive sibling", "windows", `C:\repo`, `C:\repo2`, false},
		{"drive outside", "windows", `C:\repo`, `D:\repo`, false},
		{"case-insensitive drive", "windows", `C:\Repo`, `c:/repo/pkg`, true},
		{"extended drive", "windows", `\\?\C:\repo`, `//?/C:/repo/pkg`, true},
		{"UNC containment", "windows", `\\server\share`, `\\server\share\repo`, true},
		{"extended UNC", "windows", `\\?\UNC\server\share`, `//?/UNC/server/share/repo`, true},
		{"UNC outside", "windows", `\\server\share`, `\\other\share\repo`, false},
		{"unix descendant", "linux", `/home/u/repo`, `/home/u/repo/pkg`, true},
		{"unix sibling", "linux", `/home/u/repo`, `/home/u/repo2`, false},
		{"empty root", "windows", ``, `C:\repo`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ContainmentForGOOS(tc.goos, tc.root, tc.target); got != tc.want {
				t.Fatalf("ContainmentForGOOS(%s, %q, %q) = %v, want %v", tc.goos, tc.root, tc.target, got, tc.want)
			}
		})
	}
}
