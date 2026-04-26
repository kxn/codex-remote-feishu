package daemon

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const workspaceSurfaceContextDir = ".codex-remote"
const workspaceSurfaceContextFile = "surface-context.json"

type workspaceSurfaceContextPayload struct {
	SurfaceSessionID string    `json:"surface_session_id"`
	GatewayID        string    `json:"gateway_id,omitempty"`
	ChatID           string    `json:"chat_id,omitempty"`
	ActorUserID      string    `json:"actor_user_id,omitempty"`
	WorkspaceKey     string    `json:"workspace_key,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (a *App) syncWorkspaceSurfaceContextFilesLocked() {
	desired := map[string]workspaceSurfaceContextPayload{}
	for _, surface := range a.service.Surfaces() {
		if surface == nil || state.NormalizeProductMode(surface.ProductMode) != state.ProductModeNormal {
			continue
		}
		inst := a.service.Instance(strings.TrimSpace(surface.AttachedInstanceID))
		if inst == nil {
			continue
		}
		workspaceRoot := state.NormalizeWorkspaceKey(inst.WorkspaceRoot)
		if workspaceRoot == "" {
			continue
		}
		if _, exists := desired[workspaceRoot]; exists {
			log.Printf("workspace surface context conflict: workspace=%s existing=%s ignored=%s", workspaceRoot, desired[workspaceRoot].SurfaceSessionID, surface.SurfaceSessionID)
			continue
		}
		desired[workspaceRoot] = workspaceSurfaceContextPayload{
			SurfaceSessionID: strings.TrimSpace(surface.SurfaceSessionID),
			GatewayID:        strings.TrimSpace(surface.GatewayID),
			ChatID:           strings.TrimSpace(surface.ChatID),
			ActorUserID:      strings.TrimSpace(surface.ActorUserID),
			WorkspaceKey:     state.ResolveWorkspaceKey(inst.WorkspaceKey, workspaceRoot),
			UpdatedAt:        time.Now().UTC(),
		}
	}

	for workspaceRoot, payload := range desired {
		if err := writeWorkspaceSurfaceContext(workspaceRoot, payload); err != nil {
			log.Printf("write workspace surface context failed: workspace=%s err=%v", workspaceRoot, err)
			continue
		}
		if err := ensureWorkspaceContextGitExclude(workspaceRoot); err != nil {
			log.Printf("ensure workspace context git exclude failed: workspace=%s err=%v", workspaceRoot, err)
		}
	}

	for workspaceRoot := range a.surfaceResumeRuntime.workspaceContextRoots {
		if _, ok := desired[workspaceRoot]; ok {
			continue
		}
		if err := removeWorkspaceSurfaceContext(workspaceRoot); err != nil {
			log.Printf("remove workspace surface context failed: workspace=%s err=%v", workspaceRoot, err)
		}
	}

	a.surfaceResumeRuntime.workspaceContextRoots = map[string]string{}
	for workspaceRoot, payload := range desired {
		a.surfaceResumeRuntime.workspaceContextRoots[workspaceRoot] = payload.SurfaceSessionID
	}
}

func (a *App) clearWorkspaceSurfaceContextFilesLocked() {
	for workspaceRoot := range a.surfaceResumeRuntime.workspaceContextRoots {
		if err := removeWorkspaceSurfaceContext(workspaceRoot); err != nil {
			log.Printf("remove workspace surface context during shutdown failed: workspace=%s err=%v", workspaceRoot, err)
		}
	}
	a.surfaceResumeRuntime.workspaceContextRoots = map[string]string{}
}

func workspaceSurfaceContextPath(workspaceRoot string) string {
	return filepath.Join(strings.TrimSpace(workspaceRoot), workspaceSurfaceContextDir, workspaceSurfaceContextFile)
}

func writeWorkspaceSurfaceContext(workspaceRoot string, payload workspaceSurfaceContextPayload) error {
	return writeJSONFileAtomic(workspaceSurfaceContextPath(workspaceRoot), payload, 0o600)
}

func removeWorkspaceSurfaceContext(workspaceRoot string) error {
	path := workspaceSurfaceContextPath(workspaceRoot)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func ensureWorkspaceContextGitExclude(workspaceRoot string) error {
	info, err := gitmeta.LocateWorkspace(workspaceRoot)
	if err != nil || !info.InRepo() {
		return err
	}
	rel, err := filepath.Rel(info.RepoRoot, workspaceRoot)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	pattern := "/" + workspaceSurfaceContextDir + "/"
	if rel != "." && rel != "" {
		pattern = "/" + rel + "/" + workspaceSurfaceContextDir + "/"
	}
	excludePath := filepath.Join(info.GitDir, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return err
	}
	if gitmeta.FileHasExactTrimmedLine(excludePath, pattern) {
		return nil
	}
	f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(pattern + "\n")
	return err
}

func readWorkspaceSurfaceContext(path string) (workspaceSurfaceContextPayload, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return workspaceSurfaceContextPayload{}, err
	}
	var payload workspaceSurfaceContextPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return workspaceSurfaceContextPayload{}, err
	}
	return payload, nil
}
