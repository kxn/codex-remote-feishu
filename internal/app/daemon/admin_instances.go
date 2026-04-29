package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	headlessruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/headlessruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type adminInstanceSummary struct {
	InstanceID             string     `json:"instanceId"`
	DisplayName            string     `json:"displayName,omitempty"`
	WorkspaceRoot          string     `json:"workspaceRoot,omitempty"`
	Source                 string     `json:"source,omitempty"`
	Managed                bool       `json:"managed"`
	Online                 bool       `json:"online"`
	PID                    int        `json:"pid,omitempty"`
	Status                 string     `json:"status"`
	RequestedAt            *time.Time `json:"requestedAt,omitempty"`
	StartedAt              *time.Time `json:"startedAt,omitempty"`
	IdleSince              *time.Time `json:"idleSince,omitempty"`
	LastHelloAt            *time.Time `json:"lastHelloAt,omitempty"`
	LastRefreshRequestedAt *time.Time `json:"lastRefreshRequestedAt,omitempty"`
	LastRefreshCompletedAt *time.Time `json:"lastRefreshCompletedAt,omitempty"`
	RefreshInFlight        bool       `json:"refreshInFlight"`
	LastError              string     `json:"lastError,omitempty"`
}

func (a *App) createManagedHeadlessInstance(workspaceRoot, displayName string) (adminInstanceSummary, error) {
	normalizedRoot, err := normalizeWorkspaceRoot(workspaceRoot)
	if err != nil {
		return adminInstanceSummary{}, err
	}
	cfg := a.headlessRuntime
	if strings.TrimSpace(cfg.BinaryPath) == "" {
		return adminInstanceSummary{}, fmt.Errorf("headless binary is not configured")
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = filepath.Base(filepath.Clean(normalizedRoot))
	}
	if displayName == "." || displayName == string(filepath.Separator) || displayName == "" {
		displayName = "headless"
	}
	instanceID := fmt.Sprintf("inst-headless-admin-%d", time.Now().UTC().UnixNano())
	env := append([]string{}, cfg.BaseEnv...)
	env = append(env,
		"CODEX_REMOTE_INSTANCE_ID="+instanceID,
		"CODEX_REMOTE_INSTANCE_SOURCE=headless",
		"CODEX_REMOTE_INSTANCE_MANAGED=1",
		"CODEX_REMOTE_LIFETIME=daemon-owned",
		"CODEX_REMOTE_INSTANCE_DISPLAY_NAME="+displayName,
	)
	pid, err := a.startHeadless(controlToHeadlessLaunch(cfg, env, normalizedRoot, instanceID))
	if err != nil {
		return adminInstanceSummary{}, err
	}

	requestedAt := time.Now().UTC()
	a.mu.Lock()
	a.managedHeadlessRuntime.Processes[instanceID] = &headlessruntime.Process{
		InstanceID:    instanceID,
		PID:           pid,
		RequestedAt:   requestedAt,
		StartedAt:     requestedAt,
		WorkspaceRoot: normalizedRoot,
		DisplayName:   displayName,
		Status:        headlessruntime.StatusStarting,
	}
	a.mu.Unlock()
	return adminInstanceSummary{
		InstanceID:    instanceID,
		DisplayName:   displayName,
		WorkspaceRoot: normalizedRoot,
		Source:        "headless",
		Managed:       true,
		PID:           pid,
		Status:        headlessruntime.StatusStarting,
		RequestedAt:   &requestedAt,
		StartedAt:     &requestedAt,
	}, nil
}

func normalizeWorkspaceRoot(workspaceRoot string) (string, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return "", fmt.Errorf("workspaceRoot is required")
	}
	if strings.HasPrefix(workspaceRoot, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		switch workspaceRoot {
		case "~":
			workspaceRoot = home
		case "~" + string(filepath.Separator):
			workspaceRoot = home
		default:
			prefix := "~" + string(filepath.Separator)
			if strings.HasPrefix(workspaceRoot, prefix) {
				workspaceRoot = filepath.Join(home, strings.TrimPrefix(workspaceRoot, prefix))
			}
		}
	}
	normalizedRoot, err := state.ResolveWorkspaceRootOnHost(workspaceRoot)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(normalizedRoot)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspaceRoot must be a directory")
	}
	return normalizedRoot, nil
}

func controlToHeadlessLaunch(cfg HeadlessRuntimeConfig, env []string, workDir, instanceID string) relayruntime.HeadlessLaunchOptions {
	return relayruntime.HeadlessLaunchOptions{
		BinaryPath: cfg.BinaryPath,
		ConfigPath: cfg.ConfigPath,
		Env:        env,
		Paths:      cfg.Paths,
		WorkDir:    workDir,
		InstanceID: instanceID,
		LaunchMode: relayruntime.HeadlessLaunchModeAppServer,
		Args:       cfg.LaunchArgs,
	}
}

func overlayManagedSummary(summary *adminInstanceSummary, managed *headlessruntime.Process) {
	if summary == nil || managed == nil {
		return
	}
	if managed.PID > 0 {
		summary.PID = managed.PID
	}
	if strings.TrimSpace(managed.WorkspaceRoot) != "" {
		summary.WorkspaceRoot = managed.WorkspaceRoot
	}
	if strings.TrimSpace(managed.DisplayName) != "" {
		summary.DisplayName = managed.DisplayName
	}
	if strings.TrimSpace(managed.Status) != "" {
		summary.Status = managed.Status
	}
	if managed.RequestedAt.After(time.Time{}) {
		value := managed.RequestedAt
		summary.RequestedAt = &value
	}
	if managed.StartedAt.After(time.Time{}) {
		value := managed.StartedAt
		summary.StartedAt = &value
	}
	if managed.IdleSince.After(time.Time{}) {
		value := managed.IdleSince
		summary.IdleSince = &value
	}
	if managed.LastHelloAt.After(time.Time{}) {
		value := managed.LastHelloAt
		summary.LastHelloAt = &value
	}
	if managed.LastRefreshRequestedAt.After(time.Time{}) {
		value := managed.LastRefreshRequestedAt
		summary.LastRefreshRequestedAt = &value
	}
	if managed.LastRefreshCompletedAt.After(time.Time{}) {
		value := managed.LastRefreshCompletedAt
		summary.LastRefreshCompletedAt = &value
	}
	summary.RefreshInFlight = managed.RefreshInFlight
	if strings.TrimSpace(managed.LastError) != "" {
		summary.LastError = managed.LastError
	}
	if summary.Status == headlessruntime.StatusBusy || summary.Status == headlessruntime.StatusIdle {
		summary.Online = true
	}
	if summary.Status == headlessruntime.StatusOffline || summary.Status == headlessruntime.StatusStarting || summary.Status == headlessruntime.StatusStopping || summary.Status == "stopped" || summary.Status == "deleted" {
		summary.Online = false
	}
}

func (a *App) adminManagedInstanceSummaryLocked(instanceID string) (adminInstanceSummary, bool) {
	a.syncManagedHeadlessLocked(time.Now().UTC())
	inst := a.service.Instance(instanceID)
	if inst != nil {
		summary := adminInstanceSummary{
			InstanceID:    inst.InstanceID,
			DisplayName:   inst.DisplayName,
			WorkspaceRoot: inst.WorkspaceRoot,
			Source:        inst.Source,
			Managed:       inst.Managed,
			Online:        inst.Online,
			PID:           inst.PID,
			Status:        "offline",
		}
		if inst.Online {
			summary.Status = "online"
			if headlessruntime.IsManagedInstance(inst) {
				summary.Status = headlessruntime.StatusBusy
			}
		}
		if managed := a.managedHeadlessRuntime.Processes[instanceID]; managed != nil {
			overlayManagedSummary(&summary, managed)
		}
		return summary, true
	}
	if managed := a.managedHeadlessRuntime.Processes[instanceID]; managed != nil {
		summary := adminInstanceSummary{
			InstanceID:    instanceID,
			DisplayName:   managed.DisplayName,
			WorkspaceRoot: managed.WorkspaceRoot,
			Source:        "headless",
			Managed:       true,
			PID:           managed.PID,
			Status:        firstNonEmpty(strings.TrimSpace(managed.Status), headlessruntime.StatusStarting),
		}
		overlayManagedSummary(&summary, managed)
		return summary, true
	}
	return adminInstanceSummary{}, false
}

func (a *App) managedInstancePIDLocked(instanceID string) int {
	if managed := a.managedHeadlessRuntime.Processes[instanceID]; managed != nil && managed.PID > 0 {
		return managed.PID
	}
	if inst := a.service.Instance(instanceID); inst != nil && inst.Managed && strings.EqualFold(strings.TrimSpace(inst.Source), "headless") {
		return inst.PID
	}
	return 0
}

func (a *App) noteManagedHeadlessDisconnectedLocked(instanceID string) {
	if managed := a.managedHeadlessRuntime.Processes[instanceID]; managed != nil {
		managed.Status = headlessruntime.StatusOffline
		managed.RefreshInFlight = false
		managed.RefreshCommandID = ""
	}
}
