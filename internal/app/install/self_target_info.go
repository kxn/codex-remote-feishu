package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

const (
	currentDaemonConfigEnv         = "CODEX_REMOTE_CONFIG"
	currentDaemonRuntimeIDEnv      = "CODEX_REMOTE_INSTANCE_ID"
	currentDaemonRuntimeSourceEnv  = "CODEX_REMOTE_INSTANCE_SOURCE"
	currentDaemonRuntimeManagedEnv = "CODEX_REMOTE_INSTANCE_MANAGED"
	currentDaemonRuntimeLifeEnv    = "CODEX_REMOTE_LIFETIME"
)

type CurrentDaemonTargetInfo struct {
	ResolverSource        string                         `json:"resolverSource,omitempty"`
	RuntimeInstanceID     string                         `json:"runtimeInstanceId,omitempty"`
	RuntimeInstanceSource string                         `json:"runtimeInstanceSource,omitempty"`
	RuntimeManaged        bool                           `json:"runtimeManaged,omitempty"`
	RuntimeLifetime       string                         `json:"runtimeLifetime,omitempty"`
	InstanceID            string                         `json:"instanceId"`
	BaseDir               string                         `json:"baseDir"`
	ConfigPath            string                         `json:"configPath"`
	ConfigExists          bool                           `json:"configExists"`
	StatePath             string                         `json:"statePath"`
	StateExists           bool                           `json:"stateExists"`
	ServiceName           string                         `json:"serviceName"`
	ServiceUnitPath       string                         `json:"serviceUnitPath"`
	LogPath               string                         `json:"logPath"`
	RawLogPath            string                         `json:"rawLogPath"`
	PIDPath               string                         `json:"pidPath"`
	LocalUpgradeArtifact  string                         `json:"localUpgradeArtifactPath"`
	CurrentVersion        string                         `json:"currentVersion,omitempty"`
	CurrentBinaryPath     string                         `json:"currentBinaryPath,omitempty"`
	PendingUpgradePhase   string                         `json:"pendingUpgradePhase,omitempty"`
	Relay                 RepoInstallTargetRelayEndpoint `json:"relay"`
	Admin                 RepoInstallTargetEndpoint      `json:"admin"`
	Tool                  RepoInstallTargetEndpoint      `json:"tool"`
	ExternalAccess        RepoInstallTargetEndpoint      `json:"externalAccess"`
	Pprof                 RepoInstallTargetPprofEndpoint `json:"pprof"`
}

func ResolveCurrentDaemonTargetInfo() (CurrentDaemonTargetInfo, error) {
	configPath := filepath.Clean(strings.TrimSpace(os.Getenv(currentDaemonConfigEnv)))
	xdgConfigHome := filepath.Clean(strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")))
	xdgDataHome := filepath.Clean(strings.TrimSpace(os.Getenv("XDG_DATA_HOME")))

	if configPath == "." {
		configPath = ""
	}
	if xdgConfigHome == "." {
		xdgConfigHome = ""
	}
	if xdgDataHome == "." {
		xdgDataHome = ""
	}
	if configPath == "" && xdgConfigHome == "" && xdgDataHome == "" {
		return CurrentDaemonTargetInfo{}, fmt.Errorf("current daemon target requires CODEX_REMOTE_CONFIG or XDG runtime env; run from an active codex-remote daemon environment")
	}
	if configPath == "" && xdgConfigHome != "" {
		configPath = filepath.Join(xdgConfigHome, productName, "config.json")
	}

	statePath := ""
	if xdgDataHome != "" {
		statePath = filepath.Join(xdgDataHome, productName, "install-state.json")
	}
	if statePath == "" {
		if baseDir, instanceID, ok := inferBaseDirAndInstanceFromConfigPath(configPath); ok {
			statePath = defaultInstallStatePathForInstance(baseDir, instanceID)
		}
	}
	if statePath == "" {
		return CurrentDaemonTargetInfo{}, fmt.Errorf("unable to derive current daemon install-state path from runtime env")
	}

	baseDir := ""
	instanceID := ""
	if inferredBaseDir, inferredInstanceID, ok := inferBaseDirAndInstanceFromStatePath(statePath); ok {
		baseDir = inferredBaseDir
		instanceID = inferredInstanceID
	} else if inferredBaseDir, inferredInstanceID, ok := inferBaseDirAndInstanceFromConfigPath(configPath); ok {
		baseDir = inferredBaseDir
		instanceID = inferredInstanceID
	}
	if baseDir == "" {
		baseDir = inferBaseDir(configPath, statePath)
	}
	if instanceID == "" {
		instanceID = inferInstanceID(configPath, statePath)
	}
	if baseDir == "" || strings.TrimSpace(instanceID) == "" {
		return CurrentDaemonTargetInfo{}, fmt.Errorf("unable to infer current daemon base dir or instance id from runtime env")
	}

	stateValue := InstallState{
		InstanceID: instanceID,
		BaseDir:    baseDir,
		ConfigPath: configPath,
		StatePath:  statePath,
	}
	loadedState, stateExists, err := loadRepoInstallTargetState(statePath)
	if err != nil {
		return CurrentDaemonTargetInfo{}, err
	}
	if stateExists {
		stateValue = loadedState
		stateValue.StatePath = firstNonEmpty(strings.TrimSpace(stateValue.StatePath), statePath)
		stateValue.ConfigPath = firstNonEmpty(strings.TrimSpace(stateValue.ConfigPath), configPath)
		stateValue.BaseDir = firstNonEmpty(strings.TrimSpace(stateValue.BaseDir), baseDir)
		stateValue.InstanceID = firstNonEmpty(strings.TrimSpace(stateValue.InstanceID), instanceID)
		baseDir = stateValue.BaseDir
		instanceID = stateValue.InstanceID
		configPath = stateValue.ConfigPath
		statePath = stateValue.StatePath
	}
	if configPath == "" {
		configPath = defaultConfigPathForInstance(baseDir, instanceID)
		stateValue.ConfigPath = configPath
	}
	if statePath == "" {
		statePath = defaultInstallStatePathForInstance(baseDir, instanceID)
		stateValue.StatePath = statePath
	}

	paths := RuntimePathsForState(stateValue)
	info := CurrentDaemonTargetInfo{
		ResolverSource:        "runtime_env",
		RuntimeInstanceID:     strings.TrimSpace(os.Getenv(currentDaemonRuntimeIDEnv)),
		RuntimeInstanceSource: strings.TrimSpace(os.Getenv(currentDaemonRuntimeSourceEnv)),
		RuntimeManaged:        envFlagEnabled(currentDaemonRuntimeManagedEnv),
		RuntimeLifetime:       strings.TrimSpace(os.Getenv(currentDaemonRuntimeLifeEnv)),
		InstanceID:            instanceID,
		BaseDir:               baseDir,
		ConfigPath:            configPath,
		StatePath:             statePath,
		StateExists:           stateExists,
		ServiceName:           serviceNameForInstallInstance(serviceRuntimeGOOS, instanceID),
		ServiceUnitPath:       serviceUnitPathForInstallInstance(serviceRuntimeGOOS, baseDir, instanceID),
		LogPath:               paths.DaemonLogFile,
		RawLogPath:            paths.DaemonRawLogFile,
		PIDPath:               paths.PIDFile,
		LocalUpgradeArtifact:  LocalUpgradeArtifactPath(stateValue),
	}
	if stateExists {
		info.CurrentVersion = strings.TrimSpace(loadedState.CurrentVersion)
		info.CurrentBinaryPath = firstNonEmpty(strings.TrimSpace(loadedState.CurrentBinaryPath), strings.TrimSpace(loadedState.InstalledBinary))
		if loadedState.PendingUpgrade != nil {
			info.PendingUpgradePhase = strings.TrimSpace(loadedState.PendingUpgrade.Phase)
		}
	}

	cfg, configExists, err := loadRepoInstallTargetConfig(configPath, instanceID)
	if err != nil {
		return CurrentDaemonTargetInfo{}, err
	}
	info.ConfigExists = configExists
	applyCurrentDaemonTargetConfig(&info, cfg)
	return info, nil
}

func applyCurrentDaemonTargetConfig(info *CurrentDaemonTargetInfo, cfg config.AppConfig) {
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

func envFlagEnabled(key string) bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
