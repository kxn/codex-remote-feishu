package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	targetRoots = []string{
		"internal/adapter/feishu",
		"internal/app/daemon",
	}
	rawHTTPAllowlist = map[string]string{
		"internal/adapter/feishu/longconn.go": "long connection endpoint handshake is transport-level plumbing, not ordinary brokered OpenAPI traffic",
	}
)

type violation struct {
	file   string
	line   int
	kind   string
	detail string
	allow  string
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to resolve working directory: %v\n", err)
		os.Exit(1)
	}
	files, err := collectTargetFiles(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to collect target files: %v\n", err)
		os.Exit(1)
	}
	var violations []violation
	for _, rel := range files {
		violations = append(violations, scanFile(root, rel)...)
	}
	if len(violations) == 0 {
		return
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].file != violations[j].file {
			return violations[i].file < violations[j].file
		}
		if violations[i].line != violations[j].line {
			return violations[i].line < violations[j].line
		}
		return violations[i].kind < violations[j].kind
	})
	fmt.Fprintln(os.Stderr, "feishu broker guardrail failed:")
	for _, item := range violations {
		fmt.Fprintf(os.Stderr, "- %s:%d: %s (%s)", item.file, item.line, item.kind, item.detail)
		if strings.TrimSpace(item.allow) != "" {
			fmt.Fprintf(os.Stderr, "; allowed only in %s", item.allow)
		}
		fmt.Fprintln(os.Stderr)
	}
	fmt.Fprintln(os.Stderr, "Route ordinary Feishu SDK/OpenAPI traffic through feishu.DoSDK broker closures; raw HTTP needs an explicit transport-level allowlist entry.")
	os.Exit(1)
}

func collectTargetFiles(root string) ([]string, error) {
	var files []string
	for _, relRoot := range targetRoots {
		absRoot := filepath.Join(root, filepath.FromSlash(relRoot))
		err := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func scanFile(root, rel string) []violation {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution)
	if err != nil {
		return []violation{{file: rel, line: 1, kind: "parse_error", detail: err.Error()}}
	}
	var stack []ast.Node
	var out []violation
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return false
		}
		stack = append(stack, n)
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		line := fset.Position(call.Pos()).Line
		if isFeishuSDKCall(call) && !insideBrokerClosure(stack, "DoSDK") {
			out = append(out, violation{
				file:   rel,
				line:   line,
				kind:   "direct_feishu_sdk_call",
				detail: selectorChainString(call.Fun),
				allow:  "a feishu.DoSDK callback",
			})
		}
		if isFeishuRawSDKMethodCall(call) && !insideBrokerClosure(stack, "DoSDK") {
			out = append(out, violation{
				file:   rel,
				line:   line,
				kind:   "direct_feishu_sdk_raw_call",
				detail: selectorChainString(call.Fun),
				allow:  "a feishu.DoSDK callback with an explicit no-generated-service reason",
			})
		}
		if isFeishuRawHTTPRequest(call) {
			if reason := strings.TrimSpace(rawHTTPAllowlist[rel]); reason == "" {
				out = append(out, violation{
					file:   rel,
					line:   line,
					kind:   "direct_feishu_http_request",
					detail: selectorChainString(call.Fun),
					allow:  "rawHTTPAllowlist with a transport-level reason",
				})
			}
		}
		return true
	})
	return out
}

func insideBrokerClosure(stack []ast.Node, brokerFn string) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		lit, ok := stack[i].(*ast.FuncLit)
		if !ok || i == 0 {
			continue
		}
		call, ok := stack[i-1].(*ast.CallExpr)
		if !ok || !funcLitIsCallArg(lit, call) {
			continue
		}
		if calleeLeafName(call.Fun) == brokerFn {
			return true
		}
	}
	return false
}

func funcLitIsCallArg(lit *ast.FuncLit, call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if arg == lit {
			return true
		}
	}
	return false
}

func calleeLeafName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return value.Sel.Name
	default:
		return ""
	}
}

func selectorChain(expr ast.Expr) []string {
	switch value := expr.(type) {
	case *ast.Ident:
		return []string{value.Name}
	case *ast.SelectorExpr:
		parts := selectorChain(value.X)
		return append(parts, value.Sel.Name)
	default:
		return nil
	}
}

func selectorChainString(expr ast.Expr) string {
	return strings.Join(selectorChain(expr), ".")
}

func isFeishuSDKCall(call *ast.CallExpr) bool {
	parts := selectorChain(call.Fun)
	if len(parts) < 4 {
		return false
	}
	for i := 1; i+1 < len(parts); i++ {
		switch {
		case parts[i] == "Im" && (parts[i+1] == "V1" || parts[i+1] == "V2"):
			return true
		case parts[i] == "Application" && (parts[i+1] == "V6" || parts[i+1] == "V7"):
			return true
		case parts[i] == "Event" && parts[i+1] == "V1":
			return true
		case parts[i] == "Drive" && parts[i+1] == "V1":
			return true
		case parts[i] == "Bitable" && parts[i+1] == "V1":
			return true
		}
	}
	return false
}

func isFeishuRawSDKMethodCall(call *ast.CallExpr) bool {
	parts := selectorChain(call.Fun)
	if len(parts) != 2 {
		return false
	}
	if parts[0] != "sdkClient" && parts[0] != "client" {
		return false
	}
	switch parts[1] {
	case "Get", "Post", "Patch", "Put", "Delete":
		return true
	default:
		return false
	}
}

func isFeishuRawHTTPRequest(call *ast.CallExpr) bool {
	parts := selectorChain(call.Fun)
	if len(parts) < 2 {
		return false
	}
	last := selectorChainString(call.Fun)
	if last != "http.NewRequestWithContext" && last != "http.NewRequest" {
		return false
	}
	urlIndex := 2
	if calleeLeafName(call.Fun) == "NewRequest" {
		urlIndex = 1
	}
	if len(call.Args) <= urlIndex {
		return false
	}
	fragments := strings.Join(stringFragments(call.Args[urlIndex]), "")
	if fragments == "" {
		return false
	}
	return strings.Contains(fragments, "/open-apis/") || strings.Contains(fragments, "/oauth/v1/app/registration") || strings.Contains(fragments, "feishu.cn")
}

func stringFragments(expr ast.Expr) []string {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return nil
		}
		return []string{strings.Trim(value.Value, "`")}
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return nil
		}
		return append(stringFragments(value.X), stringFragments(value.Y)...)
	case *ast.ParenExpr:
		return stringFragments(value.X)
	default:
		return nil
	}
}
