package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	var req adminInstanceCreateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode managed instance payload",
			Details: err.Error(),
		})
		return
	}
	summary, err := a.createManagedHeadlessInstance(strings.TrimSpace(req.WorkspaceRoot), strings.TrimSpace(req.DisplayName))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "managed_instance_create_failed",
			Message: "failed to create managed headless instance",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		Instance adminInstanceSummary `json:"instance"`
	}{Instance: summary})
}

func (a *App) handleAdminInstanceDelete(w http.ResponseWriter, r *http.Request) {
	instanceID := strings.TrimSpace(r.PathValue("id"))
	if instanceID == "" {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_instance_id",
			Message: "instance id is required",
		})
		return
	}

	a.mu.Lock()
	summary, ok := a.adminManagedInstanceSummaryLocked(instanceID)
	if !ok {
		a.mu.Unlock()
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "managed_instance_not_found",
			Message: "managed instance not found",
			Details: instanceID,
		})
		return
	}
	if summary.Source != "headless" || !summary.Managed {
		a.mu.Unlock()
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "managed_instance_delete_forbidden",
			Message: "only managed headless instances can be deleted from web admin",
			Details: instanceID,
		})
		return
	}
	pid := a.managedInstancePIDLocked(instanceID)
	if pid == 0 {
		a.mu.Unlock()
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "managed_instance_pid_unknown",
			Message: "managed headless instance has no known pid",
			Details: instanceID,
		})
		return
	}
	killGrace := a.headlessRuntime.KillGrace
	a.mu.Unlock()

	if err := a.stopProcess(pid, killGrace); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "managed_instance_delete_failed",
			Message: "failed to stop managed headless instance",
			Details: err.Error(),
		})
		return
	}

	a.mu.Lock()
	delete(a.managedHeadless, instanceID)
	a.service.RemoveInstance(instanceID)
	a.mu.Unlock()

	logDelete := struct {
		InstanceID string `json:"instanceId"`
		PID        int    `json:"pid"`
	}{InstanceID: instanceID, PID: pid}
	if payload, err := json.Marshal(logDelete); err == nil {
		log.Printf("admin managed instance deleted: %s", string(payload))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) adminInstancesSnapshot() []adminInstanceSummary {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.syncManagedHeadlessLocked(time.Now().UTC())

	summaries := make([]adminInstanceSummary, 0, len(a.service.Instances())+len(a.managedHeadless))
	seen := map[string]bool{}
	for _, inst := range a.service.Instances() {
		if inst == nil {
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
		seen[inst.InstanceID] = true
	}
	for instanceID, managed := range a.managedHeadless {
		if seen[instanceID] || managed == nil {
			continue
		}
		summary := adminInstanceSummary{
			InstanceID:    instanceID,
			DisplayName:   managed.DisplayName,
			WorkspaceRoot: managed.WorkspaceRoot,
			Source:        "headless",
			Managed:       true,
			PID:           managed.PID,
			Status:        firstNonEmpty(strings.TrimSpace(managed.Status), "starting"),
		}
		overlayManagedSummary(&summary, managed)
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
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspaceRoot must be a directory")
	}
	return absRoot, nil
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
