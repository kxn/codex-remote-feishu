package codexupgrade

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper"
)

func Inspect(ctx context.Context, opts InspectOptions) Installation {
	info := Installation{
		ConfiguredBinary: strings.TrimSpace(opts.ConfiguredBinary),
		NPMCommand:       firstNonEmpty(strings.TrimSpace(opts.NPMCommand), defaultNPMCommand),
		PackageName:      defaultPackageName,
		SourceKind:       SourceUnknown,
	}
	if info.ConfiguredBinary == "" {
		info.Problem = "missing configured codex binary"
		return info
	}

	effective, err := wrapper.ResolveNormalCodexBinaryPreview(info.ConfiguredBinary)
	if err != nil {
		info.Problem = err.Error()
		info.EffectiveBinary = info.ConfiguredBinary
	} else {
		info.EffectiveBinary = strings.TrimSpace(effective)
	}
	if resolved, err := resolveExecutablePath(info.EffectiveBinary); err == nil {
		info.ResolvedBinary = resolved
	} else if info.Problem == "" {
		info.Problem = err.Error()
	}

	if looksLikeBundle(info.EffectiveBinary) || looksLikeBundle(info.ResolvedBinary) {
		info.SourceKind = SourceVSCodeBundle
	}

	info.NPMPrefix = loadNPMPrefix(ctx, info.NPMCommand)
	info.NPMRoot = loadNPMRoot(ctx, info.NPMCommand)
	info.PackageRoot, info.PackageVersion, info.PackageBinPath = loadGlobalPackageMetadata(info.NPMRoot, info.PackageName)
	info.GlobalBinPath = globalBinPathForPrefix(info.NPMPrefix)
	info.GlobalBinScript = canonicalPath(filepath.Join(info.PackageRoot, "bin", "codex.js"))

	if info.SourceKind == SourceVSCodeBundle {
		return info
	}
	if matchesGlobalNPMInstallation(info) {
		info.SourceKind = SourceStandaloneNPM
		return info
	}
	if info.ResolvedBinary != "" || info.EffectiveBinary != "" {
		info.SourceKind = SourceStandaloneOther
	}
	return info
}

func loadNPMPrefix(ctx context.Context, npmCommand string) string {
	out, err := runCommand(ctx, npmCommand, "config", "get", "prefix", "--json")
	if err != nil {
		return ""
	}
	return cleanCommandOutput(out)
}

func loadNPMRoot(ctx context.Context, npmCommand string) string {
	out, err := runCommand(ctx, npmCommand, "root", "-g")
	if err != nil {
		return ""
	}
	return cleanCommandOutput(out)
}

func loadGlobalPackageMetadata(npmRoot, packageName string) (string, string, string) {
	npmRoot = strings.TrimSpace(npmRoot)
	packageName = strings.TrimSpace(packageName)
	if npmRoot == "" || packageName == "" {
		return "", "", ""
	}
	packageRoot := filepath.Join(npmRoot, filepath.FromSlash(packageName))
	raw, err := os.ReadFile(filepath.Join(packageRoot, "package.json"))
	if err != nil {
		return "", "", ""
	}
	var pkg struct {
		Name    string          `json:"name"`
		Version string          `json:"version"`
		Bin     json.RawMessage `json:"bin"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return "", "", ""
	}
	if strings.TrimSpace(pkg.Name) != packageName {
		return "", "", ""
	}
	return canonicalPath(packageRoot), strings.TrimSpace(pkg.Version), resolvePackageBinPath(packageRoot, pkg.Bin)
}

func resolvePackageBinPath(packageRoot string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return canonicalPath(filepath.Join(packageRoot, strings.TrimSpace(single)))
	}
	var bins map[string]string
	if err := json.Unmarshal(raw, &bins); err != nil {
		return ""
	}
	if value := strings.TrimSpace(bins["codex"]); value != "" {
		return canonicalPath(filepath.Join(packageRoot, value))
	}
	for _, value := range bins {
		if strings.TrimSpace(value) == "" {
			continue
		}
		return canonicalPath(filepath.Join(packageRoot, value))
	}
	return ""
}

func matchesGlobalNPMInstallation(info Installation) bool {
	if info.PackageRoot == "" || info.PackageVersion == "" {
		return false
	}
	for _, candidate := range []string{
		info.ResolvedBinary,
		info.EffectiveBinary,
		info.PackageBinPath,
		info.GlobalBinScript,
		info.GlobalBinPath,
	} {
		if samePath(candidate, info.PackageBinPath) || samePath(candidate, info.GlobalBinScript) || samePath(candidate, info.GlobalBinPath) {
			return true
		}
		if pathWithinRoot(candidate, info.PackageRoot) {
			return true
		}
	}
	return false
}

func globalBinPathForPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return ""
	}
	switch runtime.GOOS {
	case "windows":
		return canonicalPath(filepath.Join(prefix, "codex.cmd"))
	default:
		return canonicalPath(filepath.Join(prefix, "bin", "codex"))
	}
}

func looksLikeBundle(path string) bool {
	return wrapper.LooksLikeVSCodeBundleCodexPath(path)
}

func resolveExecutablePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", exec.ErrNotFound
	}
	resolved, err := exec.LookPath(value)
	if err != nil {
		return "", err
	}
	return canonicalPath(resolved), nil
}

func canonicalPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil && strings.TrimSpace(resolved) != "" {
		path = resolved
	}
	return filepath.Clean(path)
}

func samePath(left, right string) bool {
	left = canonicalPath(left)
	right = canonicalPath(right)
	if left == "" || right == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func pathWithinRoot(path, root string) bool {
	path = canonicalPath(path)
	root = canonicalPath(root)
	if path == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
