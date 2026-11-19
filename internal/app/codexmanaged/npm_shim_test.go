package codexmanaged

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNPMShimResolvesNativeAcrossWindowsLayouts(t *testing.T) {
	for _, architecture := range []struct{ name, triple, platform string }{
		{"amd64", "x86_64-pc-windows-msvc", "codex-win32-x64"},
		{"arm64", "aarch64-pc-windows-msvc", "codex-win32-arm64"},
	} {
		for _, local := range []bool{false, true} {
			for _, layout := range []string{"hoisted-bin", "nested-bin", "bundled-codex"} {
				t.Run(architecture.name+"/"+map[bool]string{false: "global", true: "local"}[local]+"/"+layout, func(t *testing.T) {
					prefix := t.TempDir()
					modules := filepath.Join(prefix, "node_modules")
					pkg := filepath.Join(modules, "@openai", "codex")
					shim := filepath.Join(prefix, "codex.cmd")
					if local {
						shim = filepath.Join(modules, ".bin", "codex.cmd")
					}
					fixtureFile(t, shim, "this arbitrary script must not execute")
					fixtureFile(t, filepath.Join(pkg, "package.json"), `{"name":"@openai/codex"}`)
					vendor := filepath.Join(modules, "@openai", architecture.platform, "vendor")
					directory := "bin"
					if layout == "nested-bin" {
						vendor = filepath.Join(pkg, "node_modules", "@openai", architecture.platform, "vendor")
					}
					if layout == "bundled-codex" {
						vendor, directory = filepath.Join(pkg, "vendor"), "codex"
					}
					binary := filepath.Join(vendor, architecture.triple, directory, "codex.exe")
					fixtureFile(t, binary, "0.159.3")
					resolved, err := npmShimNative(shim, architecture.triple, architecture.platform, "codex.exe")
					if err != nil || resolved != binary {
						t.Fatalf("resolved=%s want=%s err=%v", resolved, binary, err)
					}
					// The actual candidate is the verified native binary, preserving its
					// version selection rather than returning the .cmd launcher.
					selected, err := ensure(context.Background(), resolved, t.TempDir(), fakeOperations("0.159.3"))
					if err != nil || selected != binary {
						t.Fatalf("selected=%s err=%v", selected, err)
					}
				})
			}
		}
	}
}

func TestNPMShimRequiresOfficialMetadataAndKnownNative(t *testing.T) {
	for _, tc := range []struct {
		name, shim, metadata string
		native               bool
	}{
		{"other package", "codex.cmd", `{"name":"other/codex"}`, true},
		{"invalid metadata", "codex.cmd", `not-json`, true},
		{"missing native", "codex.cmd", `{"name":"@openai/codex"}`, false},
		{"arbitrary shell", "custom.cmd", `{"name":"@openai/codex"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefix := t.TempDir()
			shim := filepath.Join(prefix, tc.shim)
			fixtureFile(t, shim, "arbitrary-shell-script")
			pkg := filepath.Join(prefix, "node_modules", "@openai", "codex")
			fixtureFile(t, filepath.Join(pkg, "package.json"), tc.metadata)
			if tc.native {
				fixtureFile(t, filepath.Join(pkg, "vendor", "x86_64-pc-windows-msvc", "bin", "codex.exe"), "0.159.3")
			}
			if _, err := npmShimNative(shim, "x86_64-pc-windows-msvc", "codex-win32-x64", "codex.exe"); err == nil {
				t.Fatal("accepted unsafe or incomplete npm shim")
			}
		})
	}
}

func fixtureFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o700); err != nil {
		t.Fatal(err)
	}
}
