package daemon

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type adminInstancesResponse struct {
	Instances []adminInstanceSummary `json:"instances"`
}

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

type adminInstanceCreateRequest struct {
	WorkspaceRoot string `json:"workspaceRoot"`
	DisplayName   string `json:"displayName,omitempty"`
}

func (a *App) handleAdminInstancesList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, adminInstancesResponse{Instances: a.adminInstancesSnapshot()})
}

func (a *App) handleAdminInstanceCreate(w http.ResponseWriter, r *http.Request) {
	_ = r
	writeAPIError(w, http.StatusGone, apiError{
		Code:    "managed_instance_admin_removed",
		Message: "managed background instances are no longer created from web admin",
	})
}

func (a *App) handleAdminInstanceDelete(w http.ResponseWriter, r *http.Request) {
	_ = r
	writeAPIError(w, http.StatusGone, apiError{
		Code:    "managed_instance_admin_removed",
		Message: "managed background instances are no longer deleted from web admin",
	})
}

func (a *App) adminInstancesSnapshot() []adminInstanceSummary {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.syncManagedHeadlessLocked(time.Now().UTC())

	summaries := make([]adminInstanceSummary, 0, len(a.service.Instances()))
	for _, inst := range a.service.Instances() {
		if inst == nil || strings.EqualFold(strings.TrimSpace(inst.Source), "headless") {
			continue
		}
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
			if isManagedHeadlessInstance(inst) {
				summary.Status = managedHeadlessStatusBusy
			}
		}
		if managed := a.managedHeadless[inst.InstanceID]; managed != nil {
			overlayManagedSummary(&summary, managed)
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		leftRank := adminInstanceStatusRank(summaries[i].Status)
		rightRank := adminInstanceStatusRank(summaries[j].Status)
		if leftRank == rightRank {
			return summaries[i].InstanceID < summaries[j].InstanceID
		}
		return leftRank < rightRank
	})
	return summaries
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
	a.managedHeadless[instanceID] = &managedHeadlessProcess{
		InstanceID:    instanceID,
		PID:           pid,
		RequestedAt:   requestedAt,
		StartedAt:     requestedAt,
		WorkspaceRoot: normalizedRoot,
		DisplayName:   displayName,
		Status:        managedHeadlessStatusStarting,
	}
	a.mu.Unlock()
	return adminInstanceSummary{
		InstanceID:    instanceID,
		DisplayName:   displayName,
		WorkspaceRoot: normalizedRoot,
		Source:        "headless",
		Managed:       true,
		PID:           pid,
		Status:        managedHeadlessStatusStarting,
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
		Args:       cfg.LaunchArgs,
	}
}

func overlayManagedSummary(summary *adminInstanceSummary, managed *managedHeadlessProcess) {
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
	if summary.Status == managedHeadlessStatusBusy || summary.Status == managedHeadlessStatusIdle {
		summary.Online = true
	}
	if summary.Status == managedHeadlessStatusOffline || summary.Status == managedHeadlessStatusStarting || summary.Status == "stopped" || summary.Status == "deleted" {
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
			if isManagedHeadlessInstance(inst) {
				summary.Status = managedHeadlessStatusBusy
			}
		}
		if managed := a.managedHeadless[instanceID]; managed != nil {
			overlayManagedSummary(&summary, managed)
		}
		return summary, true
	}
	if managed := a.managedHeadless[instanceID]; managed != nil {
		summary := adminInstanceSummary{
			InstanceID:    instanceID,
			DisplayName:   managed.DisplayName,
			WorkspaceRoot: managed.WorkspaceRoot,
			Source:        "headless",
			Managed:       true,
			PID:           managed.PID,
			Status:        firstNonEmpty(strings.TrimSpace(managed.Status), managedHeadlessStatusStarting),
		}
		overlayManagedSummary(&summary, managed)
		return summary, true
	}
	return adminInstanceSummary{}, false
}

func (a *App) managedInstancePIDLocked(instanceID string) int {
	if managed := a.managedHeadless[instanceID]; managed != nil && managed.PID > 0 {
		return managed.PID
	}
	if inst := a.service.Instance(instanceID); inst != nil && inst.Managed && strings.EqualFold(strings.TrimSpace(inst.Source), "headless") {
		return inst.PID
	}
	return 0
}

func (a *App) noteManagedHeadlessDisconnectedLocked(instanceID string) {
	if managed := a.managedHeadless[instanceID]; managed != nil {
		managed.Status = managedHeadlessStatusOffline
		managed.RefreshInFlight = false
		managed.RefreshCommandID = ""
	}
}

func adminInstanceStatusRank(status string) int {
	switch strings.TrimSpace(status) {
	case managedHeadlessStatusBusy:
		return 0
	case "online":
		return 1
	case managedHeadlessStatusIdle:
		return 2
	case managedHeadlessStatusStarting:
		return 3
	case managedHeadlessStatusOffline:
		return 4
	default:
		return 5
	}
}
