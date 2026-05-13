package desktopsession

import (
	"context"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/browseropen"
)

var (
	resolveTargetFunc                  = ResolveTarget
	ensureDaemonReadyFromStatePathFunc = install.EnsureDaemonReadyFromStatePath
)

type ResolveOptions struct {
	StatePath           string
	InstanceID          string
	BaseDir             string
	PreferCurrentDaemon bool
}

type EnsureOptions struct {
	Resolve ResolveOptions
	Version string
}

type Target struct {
	ResolverSource   string
	InstanceID       string
	BaseDir          string
	ConfigPath       string
	StatePath        string
	SessionStatePath string
	AdminURL         string
	LogPath          string
}

func ResolveTarget(opts ResolveOptions) (Target, error) {
	if statePath := strings.TrimSpace(opts.StatePath); statePath != "" {
		return resolveExplicitStatePathTarget(statePath)
	}
	preferCurrent := opts.PreferCurrentDaemon || (strings.TrimSpace(opts.InstanceID) == "" && strings.TrimSpace(opts.BaseDir) == "")
	if preferCurrent {
		if info, err := install.ResolveCurrentDaemonTargetInfo(); err == nil {
			return targetFromCurrentDaemonInfo(info), nil
		}
	}
	defaults, err := install.DetectPlatformDefaults()
	if err != nil {
		return Target{}, err
	}
	info, err := install.ResolveRepoInstallTargetInfo(install.RepoInstallTargetOptions{
		InstanceID:      strings.TrimSpace(opts.InstanceID),
		BaseDir:         strings.TrimSpace(opts.BaseDir),
		FallbackBaseDir: defaults.BaseDir,
		GOOS:            defaults.GOOS,
	})
	if err != nil {
		return Target{}, err
	}
	return targetFromRepoInstallTargetInfo(info), nil
}

func QueryStatus(ctx context.Context, opts ResolveOptions) (Status, error) {
	target, err := resolveTargetFunc(opts)
	if err != nil {
		return Status{}, err
	}
	status := Status{
		State:      StateNone,
		InstanceID: target.InstanceID,
		AdminURL:   target.AdminURL,
	}
	if snapshot, ok, err := ReadStatusFile(target.SessionStatePath); err != nil {
		return Status{}, err
	} else if ok {
		status = mergeStatus(status, snapshot)
	}
	if strings.TrimSpace(target.AdminURL) != "" {
		live, err := Client{BaseURL: target.AdminURL}.Status(ctx)
		if err == nil {
			status = mergeStatus(status, live)
		}
	}
	if status.State == "" {
		status.State = StateNone
	}
	return status, nil
}

func EnsureBackend(ctx context.Context, opts EnsureOptions) (Status, error) {
	target, err := resolveTargetFunc(opts.Resolve)
	if err != nil {
		return Status{}, err
	}
	ready, err := ensureDaemonReadyFromStatePathFunc(ctx, target.StatePath, opts.Version)
	status := Status{
		State:         StateBackendOnly,
		InstanceID:    target.InstanceID,
		AdminURL:      ready.AdminURL,
		SetupURL:      ready.SetupURL,
		SetupRequired: ready.SetupRequired,
	}
	if snapshot, ok, readErr := ReadStatusFile(target.SessionStatePath); readErr == nil && ok {
		status = mergeStatus(status, snapshot)
	}
	if strings.TrimSpace(status.AdminURL) != "" {
		client := Client{BaseURL: status.AdminURL}
		if live, liveErr := client.Status(ctx); liveErr == nil {
			status = mergeStatus(status, live)
		}
	}
	if status.State == "" {
		status.State = StateBackendOnly
	}
	return status, err
}

func OpenAdmin(ctx context.Context, opts EnsureOptions, env map[string]string) (Status, error) {
	status, err := EnsureBackend(ctx, opts)
	if err != nil {
		return status, err
	}
	openURL := strings.TrimSpace(status.AdminURL)
	if status.SetupRequired && strings.TrimSpace(status.SetupURL) != "" {
		openURL = strings.TrimSpace(status.SetupURL)
	}
	if openURL == "" {
		return status, fmt.Errorf("desktop session admin url is empty")
	}
	if err := browseropen.Open(openURL, env); err != nil {
		return status, err
	}
	return status, nil
}

func QuitSession(ctx context.Context, opts ResolveOptions) error {
	target, err := resolveTargetFunc(opts)
	if err != nil {
		return err
	}
	if strings.TrimSpace(target.AdminURL) == "" {
		return fmt.Errorf("desktop session admin url is empty")
	}
	return Client{BaseURL: target.AdminURL}.Quit(ctx)
}

func resolveExplicitStatePathTarget(statePath string) (Target, error) {
	info, err := install.ResolveRepoInstallTargetInfoFromStatePath(statePath)
	if err != nil {
		return Target{}, err
	}
	return targetFromRepoInstallTargetInfo(info), nil
}

func targetFromCurrentDaemonInfo(info install.CurrentDaemonTargetInfo) Target {
	state := install.InstallState{
		InstanceID: info.InstanceID,
		BaseDir:    info.BaseDir,
		ConfigPath: info.ConfigPath,
		StatePath:  info.StatePath,
	}
	return Target{
		ResolverSource:   firstNonEmpty(strings.TrimSpace(info.ResolverSource), "runtime_env"),
		InstanceID:       strings.TrimSpace(info.InstanceID),
		BaseDir:          strings.TrimSpace(info.BaseDir),
		ConfigPath:       strings.TrimSpace(info.ConfigPath),
		StatePath:        strings.TrimSpace(info.StatePath),
		SessionStatePath: StateFilePath(install.RuntimePathsForState(state)),
		AdminURL:         strings.TrimSpace(info.Admin.URL),
		LogPath:          strings.TrimSpace(info.LogPath),
	}
}

func targetFromRepoInstallTargetInfo(info install.RepoInstallTargetInfo) Target {
	state := install.InstallState{
		InstanceID: info.InstanceID,
		BaseDir:    info.BaseDir,
		ConfigPath: info.ConfigPath,
		StatePath:  info.StatePath,
	}
	return Target{
		ResolverSource:   firstNonEmpty(strings.TrimSpace(info.BindingSource), "repo_target"),
		InstanceID:       strings.TrimSpace(info.InstanceID),
		BaseDir:          strings.TrimSpace(info.BaseDir),
		ConfigPath:       strings.TrimSpace(info.ConfigPath),
		StatePath:        strings.TrimSpace(info.StatePath),
		SessionStatePath: StateFilePath(install.RuntimePathsForState(state)),
		AdminURL:         strings.TrimSpace(info.Admin.URL),
		LogPath:          strings.TrimSpace(info.LogPath),
	}
}

func mergeStatus(base, incoming Status) Status {
	base.State = mergeState(base.State, incoming.State)
	if !incoming.UpdatedAt.IsZero() {
		base.UpdatedAt = incoming.UpdatedAt
	}
	if incoming.BackendPID > 0 {
		base.BackendPID = incoming.BackendPID
	}
	if strings.TrimSpace(incoming.InstanceID) != "" {
		base.InstanceID = strings.TrimSpace(incoming.InstanceID)
	}
	if strings.TrimSpace(incoming.AdminURL) != "" {
		base.AdminURL = strings.TrimSpace(incoming.AdminURL)
	}
	if strings.TrimSpace(incoming.SetupURL) != "" {
		base.SetupURL = strings.TrimSpace(incoming.SetupURL)
	}
	base.SetupRequired = incoming.SetupRequired
	return base
}

func mergeState(current, incoming State) State {
	if incoming == "" {
		return current
	}
	if current == "" {
		return incoming
	}
	if stateRank(incoming) >= stateRank(current) {
		return incoming
	}
	return current
}

func stateRank(state State) int {
	switch state {
	case StateNone:
		return 0
	case StateBackendOnly:
		return 1
	case StateHealthy:
		return 2
	case StateQuitting:
		return 3
	default:
		return 1
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
