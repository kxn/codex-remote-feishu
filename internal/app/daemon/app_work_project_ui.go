package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamProjectProjection struct {
	Project                                                      work.Project
	TeamMaterials                                                teamMaterialsProjection
	Members                                                      []work.ProjectMember
	MemberNames                                                  map[string]string
	TaskStates                                                   map[work.TaskState]int
	Decisions, Attention, Problems                               []string
	DirectorySelectable                                          bool
	NavigationTabMissing, NavigationPinMissing, NavigationLegacy bool
	NavigationGeneration                                         int64
	NavigationStatus                                             string
	HomeURL, HomeSync, HomeOperation, ArtifactsFingerprint       string
	Channel                                                      work.ProjectChannelBinding
	Binding                                                      work.RoleBotBinding
}

type teamMaterialsProjection struct {
	State, URL          string
	Allowed, NeedsCheck bool
}

func teamProjectFacts(tx *workstore.Tx, id string) (teamProjectProjection, error) {
	var v teamProjectProjection
	project, err := tx.GetProject(id)
	if err != nil {
		return v, err
	}
	v.Project = project
	team, err := tx.GetTeam(project.TeamID)
	if err != nil && !errors.Is(err, workstore.ErrNotFound) {
		return v, err
	}
	channels, err := tx.ListProjectChannelBindings(id)
	if err != nil {
		return v, err
	}
	for _, channel := range channels {
		if channel.Active && channel.Purpose == "main" {
			if v.Channel.ChatID != "" {
				return v, fmt.Errorf("ambiguous project destination")
			}
			v.Channel = channel
		}
	}
	if v.Channel.ChatID == "" {
		return v, fmt.Errorf("project destination unavailable")
	}
	if team.Materials != nil {
		allowed, err := tx.TeamMaterialsProjectAllowed(team, v.Channel)
		if err != nil {
			return v, err
		}
		v.TeamMaterials = teamMaterialsProjection{State: team.Materials.State, Allowed: allowed}
		if allowed {
			v.TeamMaterials.URL = team.Materials.URL
			v.TeamMaterials.NeedsCheck = team.Materials.LastError != ""
		}
	}
	v.Binding, err = tx.GetRoleBotBinding(project.LeaderRoleID)
	if err != nil {
		return v, err
	}
	v.Members, err = tx.ListProjectMembers(id)
	if err != nil {
		return v, err
	}
	v.MemberNames = map[string]string{}
	for _, member := range v.Members {
		role, err := tx.GetRole(member.RoleID)
		if err != nil {
			return v, err
		}
		v.MemberNames[member.RoleID] = role.Name
	}
	sort.Slice(v.Members, func(i, j int) bool { return v.Members[i].RoleID < v.Members[j].RoleID })
	tasks, err := tx.ListTasks(project.ScopeID)
	if err != nil {
		return v, err
	}
	v.TaskStates = map[work.TaskState]int{}
	for _, task := range tasks {
		v.TaskStates[task.State]++
	}
	decisions, err := tx.ListDecisionRequests(project.ScopeID)
	if err != nil {
		return v, err
	}
	for _, request := range decisions {
		if (request.State != work.DecisionOpen && request.State != work.DecisionUnknown) || request.Owner.Kind != work.ActorPrincipal {
			continue
		}
		title := "工作安排等待您确认"
		if request.Presentation != nil {
			title = request.Presentation.Title
		} else if request.TaskID != "" {
			task, err := tx.GetTask(request.TaskID)
			if err != nil {
				return v, err
			}
			title = task.Title + "：等待您确认"
		}
		v.Decisions = append(v.Decisions, title)
		v.Attention = append(v.Attention, fmt.Sprintf("%s:%d:%s", request.ID, request.RequestGeneration, request.State))
	}
	requests, err := tx.ListNativeRequests(project.ScopeID)
	if err != nil {
		return v, err
	}
	for _, request := range requests {
		if request.State != "open" && request.State != "response_pending" {
			continue
		}
		title := "执行请求等待您确认"
		if request.FormRef != "" {
			body, err := tx.GetContent(project.ScopeID, request.FormRef)
			if err != nil {
				return v, err
			}
			var form orchestrator.RequestFormState
			if err = json.Unmarshal(body.Data, &form); err != nil {
				return v, err
			}
			if form.Title != "" {
				title = form.Title
			}
		}
		v.Decisions = append(v.Decisions, title)
		v.Attention = append(v.Attention, fmt.Sprintf("%s:%d:%s", request.ID, request.Revision, request.State))
	}
	sort.Strings(v.Decisions)
	sort.Strings(v.Attention)
	problems, err := tx.ListDispatchProblems(project.ScopeID)
	if err != nil {
		return v, err
	}
	for _, problem := range problems {
		v.Problems = append(v.Problems, v.MemberNames[problem.RoleID]+"："+teamProjectProblem(problem.Reason))
	}
	sort.Strings(v.Problems)
	deliveries, err := tx.ListSentDeliveries(project.ScopeID, v.Channel.Tenant, v.Channel.ChatID, v.Channel.BindingGeneration)
	if err != nil {
		return v, err
	}
	var resources []work.Resource
	seenResources := map[string]bool{}
	for _, delivery := range deliveries {
		for _, ref := range delivery.References {
			if seenResources[ref.ResourceID] {
				continue
			}
			resource, e := tx.GetResource(project.ScopeID, ref.ResourceID)
			if e != nil {
				return v, e
			}
			resources = append(resources, resource)
			seenResources[ref.ResourceID] = true
		}
	}
	raw, _ := json.Marshal(struct {
		Resources  []work.Resource
		Deliveries []work.Delivery
	}{resources, deliveries})
	v.ArtifactsFingerprint = workui.ID(string(raw))
	home, err := tx.GetProjectHome(id)
	if err == nil {
		v.HomeURL = home.URL
		v.HomeSync = home.SyncState
		v.HomeOperation = home.OperationID
		v.NavigationTabMissing = home.NavigationTabMissing
		v.NavigationPinMissing = home.NavigationPinMissing
		v.NavigationLegacy = home.PinMessageID != "" && home.NavigationCardViewID == "" && !home.NavigationCardWanted
		v.NavigationGeneration = home.NavigationGeneration
		if home.TabID != "" && !v.NavigationTabMissing {
			v.NavigationStatus = "项目 Tab 已登记。"
		}
		if home.PinMessageID != "" && !v.NavigationPinMissing && !home.NavigationCardWanted {
			v.NavigationStatus += "导航消息已 Pin。"
		}
		if home.NavigationCardWanted {
			v.NavigationStatus += "导航卡 Pin 待核对，可先使用当前状态卡。"
		}
		if operation, e := tx.GetOperation(home.OperationID); e == nil {
			v.DirectorySelectable = operation.State != work.OperationConfirmed && project.State != work.ProjectArchiving && project.State != work.ProjectArchived
		} else {
			return v, e
		}
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return v, err
	}
	return v, nil
}
func teamProjectPanel(v teamProjectProjection) workui.Panel {
	states := map[work.ProjectState]string{work.ProjectProvisioning: "正在建立", work.ProjectActive: "进行中", work.ProjectDegraded: "部分工作待恢复", work.ProjectPausing: "正在暂停，等待执行退出", work.ProjectPaused: "已暂停", work.ProjectArchiving: "正在归档，等待执行和外部结果确认", work.ProjectArchived: "已归档"}
	panel := workui.Panel{Title: "项目 · " + v.Project.Name, Paragraphs: []string{"状态：" + states[v.Project.State], fmt.Sprintf("任务：待办%d · 进行%d · 等待%d · 核验%d · 已完成%d", v.TaskStates[work.TaskTodo], v.TaskStates[work.TaskActive], v.TaskStates[work.TaskWaiting], v.TaskStates[work.TaskChecking], v.TaskStates[work.TaskDone])}}
	labels := map[work.ProjectMemberState]string{work.ProjectMemberReady: "已加入", work.ProjectMemberInvited: "等待加入", work.ProjectMemberUnavailable: "连接待恢复"}
	members := []string{}
	ready := 0
	for _, member := range v.Members {
		if member.State == work.ProjectMemberRemoved {
			continue
		}
		if member.State == work.ProjectMemberReady {
			ready++
		}
		members = append(members, v.MemberNames[member.RoleID]+" · "+labels[member.State])
	}
	panel.Paragraphs = append(panel.Paragraphs, fmt.Sprintf("团队成员：%d位已加入，共%d位。", ready, len(members)))
	if len(members) <= 12 {
		panel.Paragraphs = append(panel.Paragraphs, strings.Join(members, "\n"))
	} else {
		panel.Paragraphs = append(panel.Paragraphs, "成员较多，此卡仅汇总加入情况。")
	}
	if v.HomeURL != "" {
		panel.Paragraphs = append(panel.Paragraphs, "固定资料入口："+v.HomeURL+"\n首页同步状态："+teamProjectHomeState(v.HomeSync))
	} else {
		panel.Paragraphs = append(panel.Paragraphs, "项目资料入口正在准备。")
	}
	if v.HomeSync == "unknown" || v.HomeSync == "conflict" {
		panel.Paragraphs = append(panel.Paragraphs, "首页需要核对：已保留原文和您的修改，负责人会核查后更新。")
	}
	if len(v.Decisions) > 0 {
		panel.Paragraphs = append(panel.Paragraphs, fmt.Sprintf("有%d项内容等待您确认，请查看对应的确认卡。", len(v.Decisions)))
		if len(v.Decisions) <= 8 {
			panel.Paragraphs = append(panel.Paragraphs, strings.Join(v.Decisions, "\n"))
		}
	}
	if len(v.Problems) > 0 {
		panel.Paragraphs = append(panel.Paragraphs, "上次检查发现以下情况，恢复结果请看对应工作卡：")
		if len(v.Problems) <= 8 {
			panel.Paragraphs = append(panel.Paragraphs, strings.Join(v.Problems, "\n"))
		} else {
			panel.Paragraphs = append(panel.Paragraphs, fmt.Sprintf("共有%d项工作需要核对执行状态。", len(v.Problems)))
		}
	}
	switch v.Project.State {
	case work.ProjectProvisioning:
		panel.Buttons = []workui.Button{{Label: "停止建立并保留资料", Action: "project.archive"}}
	case work.ProjectActive, work.ProjectDegraded:
		panel.Buttons = []workui.Button{{Label: "暂停项目", Action: "project.pause"}, {Label: "归档项目", Action: "project.archive"}}
	case work.ProjectPaused:
		panel.Buttons = []workui.Button{{Label: "恢复项目", Action: "project.resume"}, {Label: "归档项目", Action: "project.archive"}}
	case work.ProjectArchived:
		panel.Buttons = []workui.Button{{Label: "重新打开（保持暂停）", Action: "project.reopen"}}
	}
	switch {
	case v.TeamMaterials.State == "":
		panel.Paragraphs = append(panel.Paragraphs, "团队资料待连接。")
	case !v.TeamMaterials.Allowed:
		panel.Paragraphs = append(panel.Paragraphs, "团队资料尚未共享给此项目。")
	case v.TeamMaterials.State == "ready":
		panel.Paragraphs = append(panel.Paragraphs, "团队资料："+v.TeamMaterials.URL)
		if v.TeamMaterials.NeedsCheck {
			panel.Paragraphs = append(panel.Paragraphs, "团队资料的访问状态待核对。")
		}
	case v.TeamMaterials.State == "pending":
		panel.Paragraphs = append(panel.Paragraphs, "团队资料正在连接。")
	default:
		panel.Paragraphs = append(panel.Paragraphs, "团队资料连接待核对。")
	}
	panel.Buttons = append(panel.Buttons, workui.Button{Label: "查看团队资料", Action: "team.materials.open"})
	panel.Buttons = append(panel.Buttons, workui.Button{Label: "查看成果与历史", Action: "project.deliveries.open"})
	if v.DirectorySelectable {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "更换资料目录", Action: "project.directory.open"})
	}
	if v.NavigationStatus != "" {
		panel.Paragraphs = append(panel.Paragraphs, v.NavigationStatus)
	}
	if v.NavigationTabMissing || v.NavigationPinMissing {
		panel.Paragraphs = append(panel.Paragraphs, "已登记的固定入口已移除。项目首页和资料仍然保留。")
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "恢复固定入口", Action: "project.navigation.restore"})
	} else if v.NavigationLegacy {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "启用项目导航卡", Action: "project.navigation.restore"})
	}
	return panel
}

// Human-visible project summaries update one low-frequency status card.
// Internal execution events never become group progress messages.
func (a *App) runTeamProjectViews(ctx context.Context) error {
	if a.teamRuntime == nil {
		return nil
	}
	r := a.teamRuntime
	projects := []string{}
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, err := tx.ListScopes()
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			if scope.Kind == work.ScopeProject {
				projects = append(projects, scope.ProjectID)
			} else if scope.Kind == work.ScopePersonal {
				if err = a.queueTeamBusinessDecisions(tx, work.Project{ScopeID: scope.ID, LeaderRoleID: scope.RoleID}); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	var failures []error
	for _, id := range projects {
		var facts teamProjectProjection
		err := r.store.Within(ctx, func(tx *workstore.Tx) error {
			var err error
			facts, err = teamProjectFacts(tx, id)
			if err != nil {
				return err
			}
			if err = a.queueTeamBusinessDecisions(tx, facts.Project); err != nil {
				return err
			}
			attention := struct {
				TaskStates                                                   map[work.TaskState]int
				State                                                        work.ProjectState
				Revision                                                     int64
				Decisions, Problems                                          []string
				URL, HomeRecovery                                            string
				Binding                                                      work.RoleBotBinding
				Channel                                                      work.ProjectChannelBinding
				NavigationGeneration                                         int64
				NavigationStatus                                             string
				TeamMaterials                                                teamMaterialsProjection
				NavigationTabMissing, NavigationPinMissing, NavigationLegacy bool
			}{facts.TaskStates, facts.Project.State, facts.Project.Revision, facts.Attention, facts.Problems, facts.HomeURL, "", facts.Binding, facts.Channel, facts.NavigationGeneration, facts.NavigationStatus, facts.TeamMaterials, facts.NavigationTabMissing, facts.NavigationPinMissing, facts.NavigationLegacy}
			if facts.HomeSync == "unknown" || facts.HomeSync == "conflict" {
				attention.HomeRecovery = facts.HomeSync
			}
			raw, _ := json.Marshal(attention)
			previousID := ""
			var previousCreated time.Time
			views, err := tx.ListUIViews(facts.Project.ScopeID)
			if err != nil {
				return err
			}
			for _, previous := range views {
				if previous.SurfaceKey == "project.status:"+id && (previousID == "" || previous.CreatedAt.After(previousCreated)) {
					previousID, previousCreated = previous.ID, previous.CreatedAt
				}
			}
			var previousSnapshot []byte
			if previousID != "" {
				fingerprint, e := tx.GetContent(facts.Project.ScopeID, previousID+":snapshot")
				previousSnapshot = fingerprint.Data
				if e == nil && string(fingerprint.Data) == string(raw) {
					return nil
				}
				if e != nil && !errors.Is(e, workstore.ErrNotFound) {
					return e
				}
			}
			viewID := workui.ID("project.status", string(raw), previousID)
			view := workstore.UIView{ID: viewID, ScopeID: facts.Project.ScopeID, SurfaceKey: "project.status:" + id, Kind: "project.status", TargetID: id, TargetRevision: facts.Project.Revision, Binding: facts.Binding, Tenant: facts.Channel.Tenant, ChatID: facts.Channel.ChatID, ChannelGeneration: facts.Channel.BindingGeneration}
			urgent := projectSnapshotUrgent(previousSnapshot, facts)
			ready, err := workui.PrepareSnapshot(tx, &view, time.Now().UTC(), urgent)
			if err != nil || !ready {
				return err
			}
			if err = tx.PutContent(work.NewContent(viewID+":snapshot", facts.Project.ScopeID, "application/json", raw, time.Now().UTC())); err != nil {
				return err
			}
			if urgent {
				if err = tx.PutContent(work.NewContent(viewID+":snapshot-priority", facts.Project.ScopeID, "text/plain", []byte("urgent"), time.Now().UTC())); err != nil {
					return err
				}
			}
			return queueTeamProjectStatus(tx, view, facts)
		})
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if r.provision != nil && facts.HomeURL != "" && facts.HomeSync != "conflict" {
			if err = a.refreshTeamProjectHome(ctx, facts); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}
func (a *App) refreshTeamProjectHome(ctx context.Context, facts teamProjectProjection) error {
	r := a.teamRuntime
	facts.HomeSync = ""
	raw, _ := json.Marshal(facts)
	id := workui.ID("project.home.refresh", string(raw))
	if facts.Project.State == work.ProjectArchived {
		id = workui.ID("project.home.archive", facts.Project.ID, workRevision(facts.Project.Revision))
	}
	perform := false
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		budgetKey := "project.home:" + facts.Project.ID
		until, err := tx.PublicationBudgetUntil(budgetKey)
		if err != nil {
			return err
		}
		if until.After(time.Now()) {
			return nil
		}
		if err = tx.SetPublicationBudget(budgetKey, time.Now().Add(30*time.Second)); err != nil {
			return err
		}
		old, err := tx.GetOperation(id)
		if err == nil {
			if old.State == work.OperationConfirmed || time.Since(old.UpdatedAt) < 30*time.Second {
				return nil
			}
			perform = true
			return nil
		}
		if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		now := time.Now().UTC()
		content := work.NewContent(id+":facts", facts.Project.ScopeID, "application/json", raw, now)
		if err = tx.PutContent(content); err != nil {
			return err
		}
		op := work.Operation{ID: id, ScopeID: facts.Project.ScopeID, Actor: work.Actor{Kind: work.ActorSystem, ID: "project-status"}, Kind: "projection.home.refresh", TargetID: facts.Project.ID, TargetGeneration: facts.Channel.BindingGeneration, IdempotencyKey: id, RequestRef: content.ID, RequestHash: content.SHA256, State: work.OperationPrepared, Revision: 1, CreatedAt: now, UpdatedAt: now}
		if _, _, err = tx.PutOperation(op, 0); err != nil {
			return err
		}
		op.State = work.OperationSending
		op.Revision++
		_, _, err = tx.PutOperation(op, 1)
		perform = err == nil
		return err
	})
	if err != nil || !perform {
		return err
	}
	refreshErr := r.provision.RefreshHome(ctx, facts.Project.ID)
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	saveErr := r.store.Within(receiptCtx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		expected := op.Revision
		op.Revision++
		op.UpdatedAt = time.Now().UTC()
		if refreshErr == nil {
			op.State = work.OperationConfirmed
			op.LastError = ""
		} else {
			op.State = work.OperationUnknown
			op.LastError = refreshErr.Error()
		}
		_, _, err = tx.PutOperation(op, expected)
		return err
	})
	return errors.Join(refreshErr, saveErr)
}
func teamProjectProblem(reason string) string {
	value := strings.ToLower(reason)
	switch {
	case strings.Contains(value, "location"), strings.Contains(value, "worktree"), strings.Contains(value, "directory"):
		return "工作目录尚未准备就绪"
	case strings.Contains(value, "profile"), strings.Contains(value, "model"):
		return "模型配置需要检查"
	case strings.Contains(value, "permission"), strings.Contains(value, "access"):
		return "执行权限等待确认"
	case strings.Contains(value, "member"), strings.Contains(value, "bot"):
		return "成员连接尚未就绪"
	default:
		return "暂时无法开始工作，需要核对执行状态"
	}
}
func teamProjectHomeState(state string) string {
	switch state {
	case "conflict":
		return "您的修改待核对"
	case "unknown":
		return "更新结果待核对"
	case "synced", "ready":
		return "已同步"
	default:
		return "等待更新"
	}
}
