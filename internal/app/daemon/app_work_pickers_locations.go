package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) queueTeamResourcePicker(ctx context.Context, source teamHumanSource) (string, error) {
	return a.queueTeamResourcePickerPage(ctx, source, 0)
}
func (a *App) queueTeamResourcePickerPage(ctx context.Context, source teamHumanSource, page int) (string, error) {
	return a.queueTeamResourcePickerNotice(ctx, source, page, "")
}

func (a *App) queueTeamResourcePickerNotice(ctx context.Context, source teamHumanSource, page int, notice string) (string, error) {
	if page < 0 {
		return "", fmt.Errorf("无效页码")
	}
	if err := a.registerTeamLocationCandidates(ctx); err != nil {
		return "", err
	}
	var locations []work.ExecutionLocation
	var preference workstore.ExecutionPreference
	var target string
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		target, e = a.teamPickerTarget(tx, source)
		if e != nil {
			return e
		}
		preference, e = tx.GetExecutionPreference(target)
		if e == workstore.ErrNotFound {
			preference = workstore.ExecutionPreference{ScopeID: target}
		} else if e != nil {
			return e
		}
		locations, e = tx.ListLocations()
		return e
	})
	if err != nil {
		return "", err
	}
	if target != source.ScopeID {
		if err = a.verifyWorkMembership(ctx, source.PrincipalID, target, source.Tenant, source.SenderIDs, source.Gateway); err != nil {
			return "", err
		}
	}
	panel, err := a.teamResourcePickerPanel(source, target, page, preference, locations)
	if err != nil {
		return "", err
	}
	if notice != "" {
		panel.Paragraphs = append([]string{notice}, panel.Paragraphs...)
	}
	if source.NavigationRevision > 0 && source.BootstrapSourceMessageID == "" {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "返回工作区与会话", Action: "private.workspace"})
	}
	return a.queueTeamPicker(ctx, source, "execution.location-picker", target, max(1, preference.Revision), panel)
}

func (a *App) teamResourcePickerPanel(source teamHumanSource, target string, page int, preference workstore.ExecutionPreference, locations []work.ExecutionLocation) (workui.Panel, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return workui.Panel{}, err
	}
	hostID := teamID("host", hostname+":"+a.headlessRuntime.Paths.StateDir)
	visible := []work.ExecutionLocation{}
	for _, location := range locations {
		if location.IsolatedContextID == "" && location.HostID == hostID {
			visible = append(visible, location)
		}
	}
	sort.Slice(visible, func(i, j int) bool { return visible[i].Cwd < visible[j].Cwd })
	start := page * teamPickerPageSize
	if start > len(visible) {
		return workui.Panel{}, workstore.ErrConflict
	}
	end := min(start+teamPickerPageSize, len(visible))
	panel := workui.Panel{Title: "选择执行目录", Paragraphs: []string{"点击目录会将它加入可用目录并设为默认。已经选择的其他目录会保留；当前执行会先完成或停止，再使用新目录。"}}
	if source.BootstrapSourceMessageID != "" {
		panel.Paragraphs = []string{"请项目发起人选择目录。选择后，负责人会继续处理原来的需求。"}
	}
	values := func(id string, generation int64) map[string]any {
		return map[string]any{"target_scope_id": target, "location_id": id, "location_generation": strconv.FormatInt(generation, 10), "preference_revision": strconv.FormatInt(preference.Revision, 10), "navigation_revision": strconv.FormatInt(source.NavigationRevision, 10)}
	}
	for _, location := range visible[start:end] {
		label := location.Cwd
		if location.ID == preference.DefaultLocationID {
			label = "当前默认：" + label
		}
		panel.Buttons = append(panel.Buttons, workui.Button{Label: label, Action: "picker.location.select", Values: values(location.ID, location.Generation)})
		if slices.Contains(preference.SelectedLocationIDs, location.ID) {
			panel.Buttons = append(panel.Buttons, workui.Button{Label: "移除此目录", Action: "picker.location.remove", Values: values(location.ID, location.Generation)})
		}
	}
	if len(visible) == 0 {
		panel.Paragraphs = append(panel.Paragraphs, "还没有可用的目录记录。先在本机打开所需目录，之后重新打开此选择器；也可以继续使用独立临时工作区。")
	}
	panel.Buttons = append(panel.Buttons, workui.Button{Label: "清空目录选择，使用独立临时工作区", Action: "picker.location.scratch", Values: values("", 0)})
	if source.BootstrapSourceMessageID != "" {
		panel.Buttons[len(panel.Buttons)-1].Label = "使用独立临时工作区"
	}
	if page > 0 {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "上一页", Action: "picker.location.page", Values: map[string]any{"page": strconv.Itoa(page - 1), "navigation_revision": strconv.FormatInt(source.NavigationRevision, 10)}})
	}
	if end < len(visible) {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "下一页", Action: "picker.location.page", Values: map[string]any{"page": strconv.Itoa(page + 1), "navigation_revision": strconv.FormatInt(source.NavigationRevision, 10)}})
	}
	if source.BootstrapSourceMessageID != "" {
		for i := range panel.Buttons {
			panel.Buttons[i].Values["bootstrap_source_message_id"] = source.BootstrapSourceMessageID
		}
	}
	return panel, nil
}
func (a *App) teamPickerTarget(tx *workstore.Tx, source teamHumanSource) (string, error) {
	if err := a.validateTeamPickerSource(tx, source); err != nil {
		return "", err
	}
	scope, err := tx.GetScope(source.ScopeID)
	if err != nil {
		return "", err
	}
	if scope.Kind != work.ScopePersonal {
		return scope.ID, nil
	}
	nav, err := tx.GetPrivateNavigation(scope.ID)
	if err != nil {
		return "", err
	}
	if nav.Revision != source.NavigationRevision {
		return "", workstore.ErrConflict
	}
	front, err := tx.GetContext(nav.FrontContextID)
	if err != nil {
		return "", err
	}
	if front.ProjectViewScopeID != "" {
		return front.ProjectViewScopeID, nil
	}
	return scope.ID, nil
}
func (a *App) registerTeamLocationCandidates(ctx context.Context) error {
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	hostID := teamID("host", hostname+":"+a.headlessRuntime.Paths.StateDir)
	paths := map[string]bool{}
	if a.service != nil {
		for path := range a.service.RecentPersistedWorkspaces(100) {
			paths[path] = true
		}
		for _, instance := range a.service.Instances() {
			if instance != nil && instance.WorkspaceRoot != "" {
				paths[instance.WorkspaceRoot] = true
			}
		}
	}
	candidates := []string{}
	for path := range paths {
		if !filepath.IsAbs(path) {
			continue
		}
		canonical, e := filepath.EvalSymlinks(filepath.Clean(path))
		if e != nil {
			continue
		}
		info, e := os.Stat(canonical)
		if e != nil || !info.IsDir() {
			continue
		}
		candidates = append(candidates, canonical)
	}
	sort.Strings(candidates)
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		for _, path := range slices.Compact(candidates) {
			id := teamID("location", hostID+":"+path)
			if _, e := tx.GetLocation(id); e == nil {
				continue
			} else if e != workstore.ErrNotFound {
				return e
			}
			if e := tx.PutLocation(work.ExecutionLocation{ID: id, HostID: hostID, Cwd: path, Generation: 1, Revision: 1}, 0); e != nil {
				return e
			}
		}
		return nil
	})
}
func (a *App) applyTeamLocationChoice(ctx context.Context, source teamHumanSource, view workstore.UIView, action string, values map[string]any) (string, error) {
	target, _ := values["target_scope_id"].(string)
	if target != source.ScopeID {
		if err := a.verifyWorkMembership(ctx, source.PrincipalID, target, source.Tenant, source.SenderIDs, source.Gateway); err != nil {
			return "", err
		}
	}
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		actual, e := a.teamPickerTarget(tx, source)
		if e != nil {
			return e
		}
		if actual != target || view.TargetID != target {
			return workstore.ErrScopeDenied
		}

		selectionBody, e := json.Marshal(map[string]any{"human_interaction_id": source.Authority.InteractionID, "view_id": view.ID, "action": action, "values": values})
		if e != nil {
			return e
		}
		receipt := work.NewContent(teamID("location-selection", source.Key), target, "application/json", selectionBody, time.Now().UTC())
		if previous, e := tx.GetContent(target, receipt.ID); e == nil {
			if previous.SHA256 != receipt.SHA256 {
				return workstore.ErrIdempotencyConflict
			}
			return nil
		} else if e != workstore.ErrNotFound {
			return e
		}
		revisionText, _ := values["preference_revision"].(string)
		expected, e := strconv.ParseInt(revisionText, 10, 64)
		if e != nil {
			return e
		}
		preference, e := tx.GetExecutionPreference(target)
		if e == workstore.ErrNotFound {
			preference = workstore.ExecutionPreference{ScopeID: target}
		} else if e != nil {
			return e
		}
		if preference.Revision != expected {
			return workstore.ErrConflict
		}
		id, _ := values["location_id"].(string)
		switch action {
		case "picker.location.scratch":
			preference.SelectedLocationIDs = nil
			preference.DefaultLocationID = ""
		case "picker.location.select", "picker.location.remove":
			location, e := tx.GetLocation(id)
			if e != nil {
				return e
			}
			generation, _ := values["location_generation"].(string)
			if location.IsolatedContextID != "" || strconv.FormatInt(location.Generation, 10) != generation {
				return workstore.ErrConflict
			}
			info, e := os.Stat(location.Cwd)
			if e != nil {
				return e
			}
			if !info.IsDir() {
				return fmt.Errorf("所选目录已不可用")
			}
			if action == "picker.location.select" {
				if !slices.Contains(preference.SelectedLocationIDs, id) {
					preference.SelectedLocationIDs = append(preference.SelectedLocationIDs, id)
				}
				preference.DefaultLocationID = id
			} else {
				if preference.DefaultLocationID == id && len(preference.SelectedLocationIDs) > 1 {
					return fmt.Errorf("请先把另一个目录设为默认，再移除此目录")
				}
				preference.SelectedLocationIDs = slices.DeleteFunc(preference.SelectedLocationIDs, func(candidate string) bool { return candidate == id })
				if preference.DefaultLocationID == id {
					preference.DefaultLocationID = ""
				}
			}
		default:
			return fmt.Errorf("目录操作已不可用")
		}
		if e = tx.PutContent(receipt); e != nil {
			return e
		}
		preference.SelectedBy = work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}
		preference.SourceRef = receipt.ID
		preference.Revision++
		if e = tx.PutExecutionPreference(preference, expected); e != nil {
			return e
		}
		if source.BootstrapSourceMessageID != "" {
			return resumeBootstrapWithDirectory(tx, source, view, receipt.ID)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if source.BootstrapSourceMessageID != "" {
		return "bootstrap_directory_selected", nil
	}
	if action == "picker.location.select" {
		if receipt, resumed, e := a.resumeTeamPendingReview(ctx, source, view.ContextID); e != nil {
			return "", e
		} else if resumed {
			return receipt, nil
		}
	}
	notice := "已将所选目录设为默认。"
	switch action {
	case "picker.location.scratch":
		notice = "已清空默认目录。后续工作将使用独立临时工作区。"
	case "picker.location.remove":
		notice = "已移除所选目录。"
	}
	return a.queueTeamResourcePickerNotice(ctx, source, 0, notice)
}
