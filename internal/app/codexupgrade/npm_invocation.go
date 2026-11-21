package codexupgrade

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func npmInvocation(name string, args []string) (string, []string, error) {
	if runtime.GOOS != "windows" {
		return name, args, nil
	}
	resolved, err := exec.LookPath(name)
	if err != nil {
		return "", nil, err
	}
	return windowsNPMInvocation(resolved, args)
}

// npm's .cmd is a launcher, never interpreted as a shell command. Use the
// corresponding installed node/npm-cli.js pair for both inspection and install.
func windowsNPMInvocation(resolved string, args []string) (string, []string, error) {
	if !strings.EqualFold(filepath.Ext(resolved), ".cmd") {
		return resolved, args, nil
	}
	if !strings.EqualFold(filepath.Base(resolved), "npm.cmd") {
		return "", nil, fmt.Errorf("only the npm launcher is supported for tracked maintenance")
	}
	directory := filepath.Dir(resolved)
	script := filepath.Join(directory, "node_modules", "npm", "bin", "npm-cli.js")
	node := filepath.Join(directory, "node.exe")
	for _, path := range []string{script, node} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("native npm installation is missing node/npm-cli.js")
		}
	}
	return node, append([]string{script}, args...), nil
}
