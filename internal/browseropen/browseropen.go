package browseropen

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

var ErrUnavailable = errors.New("browser opener unavailable")

func Open(url string, env map[string]string) error {
	command := Command(env)
	if len(command) == 0 {
		return ErrUnavailable
	}
	cmd := execlaunch.Command(command[0], append(command[1:], url)...)
	return cmd.Start()
}

func Command(env map[string]string) []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"open"}
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler"}
	default:
		if strings.TrimSpace(env["DISPLAY"]) == "" && strings.TrimSpace(env["WAYLAND_DISPLAY"]) == "" {
			return nil
		}
		if path, err := exec.LookPath("xdg-open"); err == nil {
			return []string{path}
		}
		return nil
	}
}
