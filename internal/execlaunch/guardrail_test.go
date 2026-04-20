package execlaunch

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestProductionCodeUsesExecLaunchHelper(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	fset := token.NewFileSet()
	violations := make([]string, 0)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor":
				return filepath.SkipDir
			default:
				return nil
			}
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		if !strings.HasPrefix(rel, "cmd/") && !strings.HasPrefix(rel, "internal/") && !strings.HasPrefix(rel, "testkit/") {
			return nil
		}
		if strings.HasPrefix(rel, "internal/execlaunch/") {
			return nil
		}

		fileNode, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		aliases := map[string]bool{}
		dotImport := false
		for _, imp := range fileNode.Imports {
			if strings.Trim(imp.Path.Value, "\"") != "os/exec" {
				continue
			}
			switch {
			case imp.Name == nil:
				aliases["exec"] = true
			case imp.Name.Name == ".":
				dotImport = true
			case imp.Name.Name == "_":
			default:
				aliases[imp.Name.Name] = true
			}
		}
		if len(aliases) == 0 && !dotImport {
			return nil
		}

		ast.Inspect(fileNode, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				ident, ok := fun.X.(*ast.Ident)
				if !ok || !aliases[ident.Name] {
					return true
				}
				if fun.Sel.Name != "Command" && fun.Sel.Name != "CommandContext" {
					return true
				}
				pos := fset.Position(fun.Pos())
				violations = append(violations, fmt.Sprintf("%s:%d:%d uses %s.%s", rel, pos.Line, pos.Column, ident.Name, fun.Sel.Name))
			case *ast.Ident:
				if !dotImport {
					return true
				}
				if fun.Name != "Command" && fun.Name != "CommandContext" {
					return true
				}
				pos := fset.Position(fun.Pos())
				violations = append(violations, fmt.Sprintf("%s:%d:%d uses dot-imported %s", rel, pos.Line, pos.Column, fun.Name))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan repository: %v", err)
	}

	if len(violations) == 0 {
		return
	}
	sort.Strings(violations)
	t.Fatalf("production files must use internal/execlaunch instead of direct os/exec launches:\n%s", strings.Join(violations, "\n"))
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
