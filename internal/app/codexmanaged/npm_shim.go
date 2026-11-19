package codexmanaged

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func configuredCodexBinary(name string, env []string) (string, error) {
	resolved, err := executableFromEnv(name, env)
	if err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" || !strings.EqualFold(filepath.Ext(resolved), ".cmd") {
		return resolved, nil
	}
	triple, platform, executable := platformPackage()
	return npmShimNative(resolved, triple, platform, executable)
}

// npm emits shell launchers on Windows, which exec.Cmd cannot execute directly.
// Resolve only the named Codex npm shim from a verified official package layout;
// never interpret launcher contents or execute an arbitrary shell script.
func npmShimNative(shim, triple, platform, executable string) (string, error) {
	if !strings.EqualFold(filepath.Base(shim), "codex.cmd") {
		return "", fmt.Errorf("configured Codex shell launcher is not a supported npm shim")
	}
	prefix := filepath.Dir(shim)
	if strings.EqualFold(filepath.Base(prefix), ".bin") && strings.EqualFold(filepath.Base(filepath.Dir(prefix)), "node_modules") {
		prefix = filepath.Dir(filepath.Dir(prefix))
	}
	metadata := filepath.Join(prefix, "node_modules", "@openai", "codex", "package.json")
	file, err := os.Open(metadata)
	if err != nil {
		return "", fmt.Errorf("resolve configured Codex npm package metadata: %w", err)
	}
	defer file.Close()
	var pkg struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(file, 64*1024)).Decode(&pkg); err != nil {
		return "", fmt.Errorf("configured Codex npm package metadata is invalid")
	}
	if pkg.Name != "@openai/codex" {
		return "", fmt.Errorf("configured Codex npm shim does not belong to @openai/codex")
	}
	return installedBinaryForPlatform(prefix, triple, platform, executable)
}
