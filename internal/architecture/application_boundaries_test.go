package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Follow production imports, including indirect local dependencies. Testing only
// the facade's imports would allow a new helper to quietly reintroduce the team
// task engine into private chat or either application into the common registry.
func TestApplicationDependencyBoundaries(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	const module = "github.com/kxn/codex-remote-feishu/"
	for _, start := range []string{"internal/app/privatechat", "internal/app/daemon/executionowner", "internal/app/ingressowner", "internal/core/platformingress", "internal/app/daemon/ingressmapper"} {
		t.Run(filepath.Base(start), func(t *testing.T) {
			seen := map[string]bool{}
			var visit func(string, []string)
			visit = func(pkg string, chain []string) {
				if seen[pkg] {
					return
				}
				seen[pkg] = true
				chain = append(append([]string{}, chain...), pkg)
				if forbidden(start, pkg) {
					t.Errorf("application ownership crossed through %s", strings.Join(chain, " -> "))
					return
				}
				entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(pkg)))
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					name := entry.Name()
					if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
						continue
					}
					file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, filepath.FromSlash(pkg), name), nil, parser.ImportsOnly)
					if err != nil {
						t.Fatal(err)
					}
					for _, spec := range file.Imports {
						path, err := strconv.Unquote(spec.Path.Value)
						if err != nil {
							t.Fatal(err)
						}
						if strings.HasPrefix(path, module+"internal/") {
							visit(strings.TrimPrefix(path, module), chain)
						}
					}
				}
			}
			visit(start, nil)
		})
	}
}

func forbidden(start, pkg string) bool {
	if pkg == "internal/app/daemon" || strings.HasPrefix(pkg, "internal/app/daemon/work") ||
		pkg == "internal/core/work" || pkg == "internal/core/workcmd" {
		return true
	}
	if start == "internal/app/daemon/executionowner" || start == "internal/app/ingressowner" || start == "internal/core/platformingress" || start == "internal/app/daemon/ingressmapper" {
		return strings.HasPrefix(pkg, "internal/app/privatechat") || pkg == "internal/core/orchestrator" ||
			pkg == "internal/core/control" || pkg == "internal/core/renderer" || strings.HasPrefix(pkg, "internal/adapter/feishu")
	}
	return false
}
