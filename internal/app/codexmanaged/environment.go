package codexmanaged

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func envValue(env []string, names ...string) string {
	for _, name := range names {
		for _, entry := range env {
			key, value, ok := strings.Cut(entry, "=")
			if ok && key == name && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

// exec.LookPath reads the process PATH before cmd.Env can be assigned. Resolve
// from the supplied environment first, so restored child PATH is respected too.
func executableFromEnv(name string, env []string) (string, error) {
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
		return name, nil
	}
	path := envValue(env, "PATH", "Path", "path")
	extensions := []string{""}
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		ext := envValue(env, "PATHEXT", "Pathext")
		if ext == "" {
			ext = ".COM;.EXE;.BAT;.CMD"
		}
		extensions = append(extensions, strings.Split(strings.ToLower(ext), ";")...)
	}
	for _, directory := range filepath.SplitList(path) {
		if directory == "" {
			directory = "."
		}
		for _, extension := range extensions {
			candidate := filepath.Join(directory, name+extension)
			info, err := os.Stat(candidate)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
				continue
			}
			return filepath.Abs(candidate)
		}
	}
	return "", fmt.Errorf("resolve %s in supplied PATH: %w", name, exec.ErrNotFound)
}
