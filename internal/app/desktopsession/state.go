package desktopsession

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

const stateFileName = "desktop-session.json"

type State string

const (
	StateNone        State = "none"
	StateBackendOnly State = "backend_only"
	StateHealthy     State = "healthy"
	StateQuitting    State = "quitting"
)

type Status struct {
	State         State     `json:"state"`
	UpdatedAt     time.Time `json:"updatedAt,omitempty"`
	BackendPID    int       `json:"backendPid,omitempty"`
	InstanceID    string    `json:"instanceId,omitempty"`
	AdminURL      string    `json:"adminURL,omitempty"`
	SetupURL      string    `json:"setupURL,omitempty"`
	SetupRequired bool      `json:"setupRequired,omitempty"`
}

func StateFilePath(paths relayruntime.Paths) string {
	return filepath.Join(paths.StateDir, stateFileName)
}

func ReadStatusFile(path string) (Status, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Status{}, false, nil
	}
	path = filepath.Clean(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Status{}, false, nil
		}
		return Status{}, false, err
	}
	var status Status
	if err := json.Unmarshal(raw, &status); err != nil {
		return Status{}, false, err
	}
	if status.State == "" {
		status.State = StateNone
	}
	return status, true, nil
}

func WriteStatusFile(path string, status Status) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	path = filepath.Clean(path)
	if status.State == "" {
		status.State = StateNone
	}
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func RemoveStatusFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	path = filepath.Clean(path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
