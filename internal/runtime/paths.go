package relayruntime

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct {
	ConfigDir        string
	ConfigFile       string
	DataDir          string
	LogsDir          string
	DaemonLogFile    string
	DaemonRawLogFile string
	StateDir         string
	ManagerLockFile  string
	DaemonLockFile   string
	PIDFile          string
	IdentityFile     string
	ToolServiceFile  string
}

func DefaultPaths() (Paths, error) {
	configHome, err := xdgBase("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return Paths{}, err
	}
	dataHome, err := xdgBase("XDG_DATA_HOME", filepath.Join(".local", "share"))
	if err != nil {
		return Paths{}, err
	}
	stateHome, err := xdgBase("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return Paths{}, err
	}

	configDir := filepath.Join(configHome, ProductName)
	dataDir := filepath.Join(dataHome, ProductName)
	logsDir := filepath.Join(dataDir, "logs")
	stateDir := filepath.Join(stateHome, ProductName)
	return Paths{
		ConfigDir:        configDir,
		ConfigFile:       filepath.Join(configDir, "config.json"),
		DataDir:          dataDir,
		LogsDir:          logsDir,
		DaemonLogFile:    filepath.Join(logsDir, "codex-remote-relayd.log"),
		DaemonRawLogFile: filepath.Join(logsDir, "codex-remote-relayd-raw.ndjson"),
		StateDir:         stateDir,
		ManagerLockFile:  filepath.Join(stateDir, "relay-manager.lock"),
		DaemonLockFile:   filepath.Join(stateDir, "relayd.lock"),
		PIDFile:          filepath.Join(stateDir, "codex-remote-relayd.pid"),
		IdentityFile:     filepath.Join(stateDir, "codex-remote-relayd.identity.json"),
		ToolServiceFile:  filepath.Join(stateDir, "codex-remote-tool-service.json"),
	}, nil
}

func xdgBase(envKey, fallbackSuffix string) (string, error) {
	if value := os.Getenv(envKey); value != "" {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallbackSuffix), nil
}

func WrapperRawLogFile(logsDir string, pid int) string {
	if pid <= 0 {
		return filepath.Join(logsDir, "codex-remote-wrapper-unknown-raw.ndjson")
	}
	return filepath.Join(logsDir, fmt.Sprintf("codex-remote-wrapper-%d-raw.ndjson", pid))
}
