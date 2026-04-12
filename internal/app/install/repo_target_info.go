package install

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

type RepoInstallTargetOptions struct {
	InstanceID      string
	BaseDir         string
	FallbackBaseDir string
	GOOS            string
	RequireBinding  bool
}

type RepoInstallTargetEndpoint struct {
	ListenHost string `json:"listenHost,omitempty"`
	ListenPort int    `json:"listenPort,omitempty"`
	URL        string `json:"url,omitempty"`
}

type RepoInstallTargetRelayEndpoint struct {
	RepoInstallTargetEndpoint
	ServerURL string `json:"serverURL,omitempty"`
}

type RepoInstallTargetPprofEndpoint struct {
	RepoInstallTargetEndpoint
	Enabled bool `json:"enabled"`
}

type RepoInstallTargetInfo struct {
	RepoRoot                 string                         `json:"repoRoot,omitempty"`
	BindingPath              string                         `json:"bindingPath,omitempty"`
	BindingSource            string                         `json:"bindingSource,omitempty"`
	InstanceID               string                         `json:"instanceId"`
	BaseDir                  string                         `json:"baseDir"`
	InstallBinDir            string                         `json:"installBinDir"`
	ConfigPath               string                         `json:"configPath"`
	ConfigExists             bool                           `json:"configExists"`
	StatePath                string                         `json:"statePath"`
	StateExists              bool                           `json:"stateExists"`
	ServiceName              string                         `json:"serviceName"`
	ServiceUnitPath          string                         `json:"serviceUnitPath"`
	LogPath                  string                         `json:"logPath"`
	RawLogPath               string                         `json:"rawLogPath"`
	PIDPath                  string                         `json:"pidPath"`
	LocalUpgradeArtifactPath string                         `json:"localUpgradeArtifactPath"`
	CurrentVersion           string                         `json:"currentVersion,omitempty"`
	CurrentBinaryPath        string                         `json:"currentBinaryPath,omitempty"`
	PendingUpgradePhase      string                         `json:"pendingUpgradePhase,omitempty"`
	Relay                    RepoInstallTargetRelayEndpoint `json:"relay"`
	Admin                    RepoInstallTargetEndpoint      `json:"admin"`
	Tool                     RepoInstallTargetEndpoint      `json:"tool"`
	ExternalAccess           RepoInstallTargetEndpoint      `json:"externalAccess"`
	Pprof                    RepoInstallTargetPprofEndpoint `json:"pprof"`
}

func ResolveRepoInstallTargetInfo(opts RepoInstallTargetOptions) (RepoInstallTargetInfo, error) {
	goos := strings.TrimSpace(opts.GOOS)
	if goos == "" {
		goos = runtime.GOOS
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return RepoInstallTargetInfo{}, err
	}
	explicitInstance := strings.TrimSpace(opts.InstanceID)
	explicitBaseDir := strings.TrimSpace(opts.BaseDir)

	bindingSource := "explicit"
	if explicitInstance == "" && explicitBaseDir == "" {
		if strings.TrimSpace(repoRoot) == "" && opts.RequireBinding {
			return RepoInstallTargetInfo{}, fmt.Errorf("repo install target requires a repository context or explicit -instance/-base-dir")
		}
		if _, source, found, err := readRepoInstallBindingWithSource(repoRoot); err != nil {
			return RepoInstallTargetInfo{}, err
		} else if found {
			bindingSource = string(source)
		} else if opts.RequireBinding {
			return RepoInstallTargetInfo{}, fmt.Errorf("repo install target is not bound for %s; write .codex-remote/install-target.json or pass -instance/-base-dir explicitly", repoRoot)
		} else if strings.TrimSpace(repoRoot) != "" {
			bindingSource = "detected"
		} else {
			bindingSource = "platform_default"
		}
	}

	selection, err := resolveInstallInstanceSelection(explicitInstance, explicitBaseDir, opts.FallbackBaseDir, goos)
	if err != nil {
		return RepoInstallTargetInfo{}, err
	}

	state := InstallState{
		InstanceID: selection.InstanceID,
		BaseDir:    selection.BaseDir,
		ConfigPath: selection.ConfigPath,
		StatePath:  selection.StatePath,
	}
	paths := RuntimePathsForState(state)
	info := RepoInstallTargetInfo{
		RepoRoot:                 selection.RepoRoot,
		BindingPath:              repoInstallBindingPath(selection.RepoRoot),
		BindingSource:            bindingSource,
		InstanceID:               selection.InstanceID,
		BaseDir:                  selection.BaseDir,
		InstallBinDir:            selection.InstallBinDir,
		ConfigPath:               selection.ConfigPath,
		StatePath:                selection.StatePath,
		ServiceName:              selection.ServiceName,
		ServiceUnitPath:          selection.ServiceUnitPath,
		LogPath:                  paths.DaemonLogFile,
		RawLogPath:               paths.DaemonRawLogFile,
		PIDPath:                  paths.PIDFile,
		LocalUpgradeArtifactPath: LocalUpgradeArtifactPath(state),
	}

	loadedState, stateExists, err := loadRepoInstallTargetState(selection.StatePath)
	if err != nil {
		return RepoInstallTargetInfo{}, err
	}
	info.StateExists = stateExists
	if stateExists {
		if strings.TrimSpace(loadedState.ConfigPath) != "" {
			info.ConfigPath = loadedState.ConfigPath
			state.ConfigPath = loadedState.ConfigPath
		}
		info.CurrentVersion = strings.TrimSpace(loadedState.CurrentVersion)
		info.CurrentBinaryPath = firstNonEmpty(strings.TrimSpace(loadedState.CurrentBinaryPath), strings.TrimSpace(loadedState.InstalledBinary))
		if loadedState.PendingUpgrade != nil {
			info.PendingUpgradePhase = string(loadedState.PendingUpgrade.Phase)
		}
		info.LocalUpgradeArtifactPath = LocalUpgradeArtifactPath(loadedState)
	}

	cfg, configExists, err := loadRepoInstallTargetConfig(state.ConfigPath, selection.InstanceID)
	if err != nil {
		return RepoInstallTargetInfo{}, err
	}
	info.ConfigPath = state.ConfigPath
	info.ConfigExists = configExists
	applyRepoInstallTargetConfig(&info, cfg)
	return info, nil
}

func loadRepoInstallTargetState(path string) (InstallState, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return InstallState{}, false, nil
	}
	state, err := LoadState(path)
	if err != nil {
		if os.IsNotExist(err) {
			return InstallState{}, false, nil
		}
		return InstallState{}, false, err
	}
	return state, true, nil
}

func loadRepoInstallTargetConfig(path, instanceID string) (config.AppConfig, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return defaultRepoInstallTargetConfig(instanceID), false, nil
	}
	loaded, err := config.LoadAppConfigAtPath(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultRepoInstallTargetConfig(instanceID), false, nil
		}
		return config.AppConfig{}, false, err
	}
	return loaded.Config, true, nil
}

func defaultRepoInstallTargetConfig(instanceID string) config.AppConfig {
	cfg := config.DefaultAppConfig()
	ports := instanceDefaultPorts(instanceID)
	cfg.Relay.ListenPort = ports.Relay
	cfg.Relay.ServerURL = relayServerURLForPort(ports.Relay)
	cfg.Admin.ListenPort = ports.Admin
	cfg.Tool.ListenPort = ports.Tool
	cfg.ExternalAccess.ListenPort = ports.ExternalAccess
	if cfg.Debug.Pprof == nil {
		cfg.Debug.Pprof = &config.PprofSettings{}
	}
	cfg.Debug.Pprof.ListenHost = firstNonEmpty(cfg.Debug.Pprof.ListenHost, "127.0.0.1")
	cfg.Debug.Pprof.ListenPort = ports.Pprof
	applyBuildFlavorDebugDefaults(&cfg)
	return cfg
}

func applyRepoInstallTargetConfig(info *RepoInstallTargetInfo, cfg config.AppConfig) {
	if info == nil {
		return
	}
	info.Relay = RepoInstallTargetRelayEndpoint{
		RepoInstallTargetEndpoint: RepoInstallTargetEndpoint{
			ListenHost: normalizeRepoTargetListenHost(cfg.Relay.ListenHost),
			ListenPort: cfg.Relay.ListenPort,
			URL:        repoTargetWSURL(normalizeRepoTargetListenHost(cfg.Relay.ListenHost), cfg.Relay.ListenPort),
		},
		ServerURL: strings.TrimSpace(cfg.Relay.ServerURL),
	}
	info.Admin = RepoInstallTargetEndpoint{
		ListenHost: normalizeRepoTargetListenHost(cfg.Admin.ListenHost),
		ListenPort: cfg.Admin.ListenPort,
		URL:        repoTargetHTTPURL(normalizeRepoTargetListenHost(cfg.Admin.ListenHost), cfg.Admin.ListenPort, ""),
	}
	info.Tool = RepoInstallTargetEndpoint{
		ListenHost: normalizeRepoTargetListenHost(cfg.Tool.ListenHost),
		ListenPort: cfg.Tool.ListenPort,
		URL:        repoTargetHTTPURL(normalizeRepoTargetListenHost(cfg.Tool.ListenHost), cfg.Tool.ListenPort, ""),
	}
	info.ExternalAccess = RepoInstallTargetEndpoint{
		ListenHost: normalizeRepoTargetListenHost(cfg.ExternalAccess.ListenHost),
		ListenPort: cfg.ExternalAccess.ListenPort,
		URL:        repoTargetHTTPURL(normalizeRepoTargetListenHost(cfg.ExternalAccess.ListenHost), cfg.ExternalAccess.ListenPort, ""),
	}

	pprof := config.PprofSettings{}
	if cfg.Debug.Pprof != nil {
		pprof = *cfg.Debug.Pprof
	}
	pprofHost := normalizeRepoTargetListenHost(pprof.ListenHost)
	info.Pprof = RepoInstallTargetPprofEndpoint{
		RepoInstallTargetEndpoint: RepoInstallTargetEndpoint{
			ListenHost: pprofHost,
			ListenPort: pprof.ListenPort,
			URL:        repoTargetHTTPURL(pprofHost, pprof.ListenPort, "/debug/pprof/"),
		},
		Enabled: cfg.Debug.Pprof != nil && cfg.Debug.Pprof.Enabled,
	}
}

func normalizeRepoTargetListenHost(value string) string {
	return firstNonEmpty(strings.TrimSpace(value), "127.0.0.1")
}

func repoTargetHTTPURL(host string, port int, suffix string) string {
	if port <= 0 {
		return ""
	}
	return fmt.Sprintf("http://%s:%d%s", host, port, suffix)
}

func repoTargetWSURL(host string, port int) string {
	if port <= 0 {
		return ""
	}
	return fmt.Sprintf("ws://%s:%d/ws/agent", host, port)
}
