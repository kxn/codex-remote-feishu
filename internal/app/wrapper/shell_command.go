package wrapper

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type localShellKind string

const (
	localShellPOSIX      localShellKind = "posix"
	localShellPowerShell localShellKind = "powershell"
	localShellCMD        localShellKind = "cmd"
)

const shellCommandPayloadTTL = 24 * time.Hour

type shellCommandRuntime struct {
	stateDir  string
	runtimeID string

	mu             sync.Mutex
	detected       bool
	detectionKind  localShellKind
	detectionPath  string
	detectionError error
	negativeUntil  time.Time
	payloads       map[string][]*shellCommandPayload
}

type shellCommandPayload struct {
	path     string
	threadID string
	turnID   string
	itemID   string
}

func newShellCommandRuntime(stateDir, runtimeID string) *shellCommandRuntime {
	runtime := &shellCommandRuntime{
		stateDir:  strings.TrimSpace(stateDir),
		runtimeID: safeShellRuntimeID(runtimeID),
		payloads:  map[string][]*shellCommandPayload{},
	}
	if runtime.runtimeID == "" {
		runtime.runtimeID = "unknown"
	}
	_ = cleanupExpiredShellCommandPayloads(runtime.stateDir, time.Now())
	return runtime
}

func (r *shellCommandRuntime) prepare(command agentproto.Command) (agentproto.Command, func(bool), error) {
	if r == nil {
		return agentproto.Command{}, nil, errors.New("shell command runtime is unavailable")
	}
	if strings.TrimSpace(command.ShellCommand.Payload) == "" {
		return agentproto.Command{}, nil, errors.New("shell command payload is empty")
	}
	kind, _, err := r.detect()
	if err != nil {
		return agentproto.Command{}, nil, err
	}
	payloadPath, err := r.writePayload(command.ShellCommand.Payload)
	if err != nil {
		return agentproto.Command{}, nil, err
	}
	r.trackPayload(&shellCommandPayload{
		path:     payloadPath,
		threadID: strings.TrimSpace(command.Target.ThreadID),
		turnID:   strings.TrimSpace(command.Target.TurnID),
	})
	cleanup := func(accepted bool) {
		// A successful thread/shellCommand response only acknowledges that the
		// UserShell item was accepted. observeEvents removes the payload after
		// that item completes; failures can remove it immediately.
		if !accepted {
			r.removePayload(payloadPath)
		}
	}
	shellCommand, err := fixedShellReadCommand(kind, payloadPath)
	if err != nil {
		cleanup(false)
		return agentproto.Command{}, nil, err
	}
	command.ShellCommand.Command = shellCommand
	return command, cleanup, nil
}

func (r *shellCommandRuntime) trackPayload(payload *shellCommandPayload) {
	if r == nil || payload == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.payloads[shellPayloadKey(payload.threadID, payload.turnID)] = append(
		r.payloads[shellPayloadKey(payload.threadID, payload.turnID)], payload,
	)
}

func (r *shellCommandRuntime) removePayload(path string) {
	if r == nil || strings.TrimSpace(path) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removePayloadLocked(path)
}

func (r *shellCommandRuntime) removePayloadLocked(path string) {
	for key, payloads := range r.payloads {
		remaining := payloads[:0]
		for _, payload := range payloads {
			if payload == nil || payload.path == path {
				_ = os.Remove(path)
				continue
			}
			remaining = append(remaining, payload)
		}
		if len(remaining) == 0 {
			delete(r.payloads, key)
		} else {
			r.payloads[key] = remaining
		}
	}
}

func (r *shellCommandRuntime) observeEvents(events []agentproto.Event) {
	if r == nil || len(events) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range events {
		if !isUserShellEvent(event) {
			continue
		}
		key := shellPayloadKey(event.ThreadID, event.TurnID)
		payloads := r.payloads[key]
		if len(payloads) == 0 {
			continue
		}
		if event.Kind == agentproto.EventItemStarted {
			for _, payload := range payloads {
				if payload != nil && payload.itemID == "" {
					payload.itemID = strings.TrimSpace(event.ItemID)
					break
				}
			}
			continue
		}
		if event.Kind != agentproto.EventItemCompleted {
			continue
		}
		for _, payload := range payloads {
			if payload == nil || (payload.itemID != "" && payload.itemID != strings.TrimSpace(event.ItemID)) {
				continue
			}
			r.removePayloadLocked(payload.path)
			break
		}
	}
}

func shellPayloadKey(threadID, turnID string) string {
	return strings.TrimSpace(threadID) + "\x00" + strings.TrimSpace(turnID)
}

func isUserShellEvent(event agentproto.Event) bool {
	if event.ItemKind != "command_execution" {
		return false
	}
	source, _ := event.Metadata["source"].(string)
	source = strings.ToLower(strings.TrimSpace(source))
	source = strings.ReplaceAll(source, "_", "")
	source = strings.ReplaceAll(source, "-", "")
	return source == "usershell"
}

func (r *shellCommandRuntime) detect() (localShellKind, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.detected {
		return r.detectionKind, r.detectionPath, r.detectionError
	}
	if !r.negativeUntil.IsZero() && time.Now().Before(r.negativeUntil) {
		return "", "", r.detectionError
	}
	kind, path, ok := detectLocalShell(os.Environ(), runtime.GOOS)
	if !ok {
		r.detectionError = errors.New("no trusted local shell detected")
		r.negativeUntil = time.Now().Add(10 * time.Second)
		return "", "", r.detectionError
	}
	r.detected = true
	r.detectionKind = kind
	r.detectionPath = path
	r.detectionError = nil
	return kind, path, nil
}

func (r *shellCommandRuntime) invalidateDetection() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.detected = false
	r.detectionKind = ""
	r.detectionPath = ""
	r.detectionError = nil
	r.negativeUntil = time.Time{}
}

func (r *shellCommandRuntime) cleanupCurrentRuntime() {
	if r == nil || strings.TrimSpace(r.stateDir) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.payloads = map[string][]*shellCommandPayload{}
	root := filepath.Join(r.stateDir, "applause-shell")
	if err := validateShellDirectory(root); err != nil {
		return
	}
	runtimeDir := filepath.Join(root, "runtime-"+r.runtimeID)
	info, err := os.Lstat(runtimeDir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return
	}
	_ = os.RemoveAll(runtimeDir)
}

func (r *shellCommandRuntime) writePayload(payload string) (string, error) {
	root := filepath.Join(r.stateDir, "applause-shell")
	runtimeDir := filepath.Join(root, "runtime-"+r.runtimeID)
	if strings.TrimSpace(r.stateDir) == "" {
		return "", errors.New("shell command state directory is empty")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create shell command root: %w", err)
	}
	if err := validateShellDirectory(root); err != nil {
		return "", fmt.Errorf("validate shell command root: %w", err)
	}
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return "", fmt.Errorf("create shell command runtime directory: %w", err)
	}
	if err := validateShellDirectory(runtimeDir); err != nil {
		return "", fmt.Errorf("validate shell command runtime directory: %w", err)
	}
	file, err := os.CreateTemp(runtimeDir, "payload-*.json")
	if err != nil {
		return "", fmt.Errorf("create shell command payload: %w", err)
	}
	path := file.Name()
	defer func() { _ = file.Close() }()
	if err := file.Chmod(0o600); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("protect shell command payload: %w", err)
	}
	if _, err := file.WriteString(payload); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("write shell command payload: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("sync shell command payload: %w", err)
	}
	return path, nil
}

func detectLocalShell(env []string, goos string) (localShellKind, string, bool) {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = strings.TrimSpace(value)
		}
	}
	candidates := []string{values["CODEX_REMOTE_SHELL"], values["SHELL"]}
	if strings.EqualFold(goos, "windows") {
		for _, candidate := range candidates {
			if kind, path, ok := classifyShellCandidate(candidate); ok {
				return kind, path, true
			}
		}
		if values["PSModulePath"] != "" {
			if path := firstAvailableShell("pwsh", "powershell"); path != "" {
				return localShellPowerShell, path, true
			}
		}
		if kind, path, ok := classifyShellCandidate(values["ComSpec"]); ok {
			return kind, path, true
		}
		if kind, path, ok := classifyShellCandidate(values["COMSPEC"]); ok {
			return kind, path, true
		}
		return localShellCMD, "cmd.exe", true
	}
	for _, candidate := range candidates {
		if kind, path, ok := classifyShellCandidate(candidate); ok {
			return kind, path, true
		}
	}
	for _, candidate := range []string{"bash", "zsh", "sh"} {
		if path := firstAvailableShell(candidate); path != "" {
			return localShellPOSIX, path, true
		}
	}
	return "", "", false
}

func classifyShellCandidate(candidate string) (localShellKind, string, bool) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "", "", false
	}
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(candidate, "\\", "/")))
	switch base {
	case "pwsh", "pwsh.exe", "powershell", "powershell.exe":
		return localShellPowerShell, candidate, true
	case "cmd", "cmd.exe":
		return localShellCMD, candidate, true
	case "bash", "bash.exe", "zsh", "zsh.exe", "sh", "sh.exe", "dash", "ash":
		return localShellPOSIX, candidate, true
	default:
		return "", "", false
	}
}

func firstAvailableShell(names ...string) string {
	for _, name := range names {
		if path, err := lookPath(name); err == nil && path != "" {
			return path
		}
	}
	return ""
}

var lookPath = func(name string) (string, error) {
	return exec.LookPath(name)
}

func fixedShellReadCommand(kind localShellKind, path string) (string, error) {
	switch kind {
	case localShellPOSIX:
		return "cat " + quotePOSIX(path), nil
	case localShellPowerShell:
		return "Get-Content -Raw -Encoding UTF8 -LiteralPath " + quotePowerShell(path), nil
	case localShellCMD:
		return "type " + quoteCMD(path), nil
	default:
		return "", errors.New("unsupported local shell")
	}
}

func quotePOSIX(path string) string { return "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'" }

func quotePowerShell(path string) string { return "'" + strings.ReplaceAll(path, "'", "''") + "'" }

func quoteCMD(path string) string {
	return `"` + strings.ReplaceAll(path, `"`, `""`) + `"`
}

func safeShellRuntimeID(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func cleanupExpiredShellCommandPayloads(stateDir string, now time.Time) error {
	root := filepath.Join(strings.TrimSpace(stateDir), "applause-shell")
	if strings.TrimSpace(stateDir) == "" {
		return nil
	}
	if err := validateShellDirectory(root); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "runtime-") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, infoErr := os.Lstat(path)
		if infoErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		if infoErr != nil || now.Sub(info.ModTime()) < shellCommandPayloadTTL {
			continue
		}
		_ = os.RemoveAll(path)
	}
	return nil
}

func validateShellDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("shell command directory is a symlink: %s", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("shell command path is not a directory: %s", path)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("protect shell command directory: %w", err)
	}
	return nil
}
