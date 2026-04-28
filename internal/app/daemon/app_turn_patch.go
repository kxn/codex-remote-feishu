package daemon

import (
	"fmt"
	"strings"
	"time"

	turnpatchruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/turnpatchruntime"
	"github.com/kxn/codex-remote-feishu/internal/codexstate"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	turnPatchFlowTTL         = 15 * time.Minute
	turnPatchFlowIDPrefix    = "turn-patch-flow-"
	turnPatchRequestIDPrefix = "turn-patch-req-"
)

type turnPatchTarget struct {
	Surface     *state.SurfaceConsoleRecord
	Instance    *state.InstanceRecord
	ThreadID    string
	ThreadTitle string
}

func (a *App) interceptTurnPatchActionLocked(action control.Action) ([]eventcontract.Event, bool) {
	a.reapTurnPatchRuntimeLocked(time.Now().UTC())

	if flow := a.editingTurnPatchFlowForSurfaceLocked(action.SurfaceSessionID); flow != nil {
		if turnPatchRequestIDForAction(action) == strings.TrimSpace(flow.RequestID) {
			return a.handleTurnPatchRequestActionLocked(action, flow), true
		}
		if !turnPatchEditingFlowAllowsAction(action) {
			a.ensureSurfaceRouteForNotice(action)
			return []eventcontract.Event{
				turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_flow_active", "当前正在填写 patch 卡，请先提交或取消。"),
			}, true
		}
	}

	if tx := a.turnPatchTransactionForActionLocked(action); tx != nil && !turnPatchTransactionAllowsAction(action) {
		a.ensureSurfaceRouteForNotice(action)
		return []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_running", turnPatchTransactionBlockedText(tx)),
		}, true
	}

	if flow := a.findTurnPatchFlowByRequestIDLocked(turnPatchRequestIDForAction(action)); flow != nil {
		return a.handleTurnPatchRequestActionLocked(action, flow), true
	}

	switch action.Kind {
	case control.ActionTurnPatchCommand:
		return a.handleTurnPatchCommandLocked(action), true
	case control.ActionTurnPatchRollback:
		return a.handleTurnPatchRollbackCommandLocked(action, ""), true
	default:
		return nil, false
	}
}

func turnPatchEditingFlowAllowsAction(action control.Action) bool {
	switch action.Kind {
	case control.ActionRespondRequest, control.ActionControlRequest, control.ActionReactionCreated, control.ActionMessageRecalled:
		return true
	default:
		return false
	}
}

func turnPatchTransactionAllowsAction(action control.Action) bool {
	switch action.Kind {
	case control.ActionStatus,
		control.ActionListInstances,
		control.ActionShowCommandHelp,
		control.ActionShowCommandMenu,
		control.ActionShowHistory,
		control.ActionDebugCommand,
		control.ActionReactionCreated,
		control.ActionMessageRecalled:
		return true
	default:
		return false
	}
}

func turnPatchTransactionBlockedText(tx *turnpatchruntime.Transaction) string {
	if tx == nil || tx.Kind != turnpatchruntime.TransactionKindRollback {
		return "当前正在修补当前会话，请等待完成后再继续。"
	}
	return "当前正在回滚最近一次修补，请等待完成后再继续。"
}

func (a *App) handleTurnPatchCommandLocked(action control.Action) []eventcontract.Event {
	args := turnPatchCommandArgs(action)
	switch {
	case len(args) == 0:
		return a.openTurnPatchFlowLocked(action)
	case strings.EqualFold(args[0], "rollback"):
		if len(args) > 2 {
			a.ensureSurfaceRouteForNotice(action)
			return []eventcontract.Event{
				turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_invalid_args", "用法只支持 `/bendtomywill` 或 `/bendtomywill rollback [patch_id]`。"),
			}
		}
		patchID := ""
		if len(args) == 2 {
			patchID = strings.TrimSpace(args[1])
		}
		return a.handleTurnPatchRollbackCommandLocked(action, patchID)
	default:
		a.ensureSurfaceRouteForNotice(action)
		return []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_invalid_args", "用法只支持 `/bendtomywill` 或 `/bendtomywill rollback [patch_id]`。"),
		}
	}
}

func (a *App) handleTurnPatchRollbackCommandLocked(action control.Action, patchID string) []eventcontract.Event {
	if patchID == "" {
		args := turnPatchCommandArgs(action)
		if action.Kind == control.ActionTurnPatchRollback && len(args) > 1 {
			a.ensureSurfaceRouteForNotice(action)
			return []eventcontract.Event{
				turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_invalid_args", "回滚命令最多只接受一个 patch id。"),
			}
		}
		if action.Kind == control.ActionTurnPatchRollback && len(args) == 1 {
			patchID = strings.TrimSpace(args[0])
		}
	}
	target, blocked := a.turnPatchResolveTargetLocked(action, true)
	if blocked != nil {
		return blocked
	}
	if flow := a.findTurnPatchFlowByPatchIDLocked(patchID); flow != nil {
		return a.beginTurnPatchRollbackLocked(action, flow, patchID)
	}
	now := time.Now().UTC()
	flow := &turnpatchruntime.FlowRecord{
		FlowID:           a.nextTurnPatchFlowIDLocked(),
		RequestID:        a.nextTurnPatchRequestIDLocked(),
		InstanceID:       strings.TrimSpace(target.Instance.InstanceID),
		SurfaceSessionID: strings.TrimSpace(target.Surface.SurfaceSessionID),
		OwnerUserID:      strings.TrimSpace(firstNonEmpty(action.ActorUserID, target.Surface.ActorUserID)),
		ThreadID:         strings.TrimSpace(target.ThreadID),
		ThreadTitle:      strings.TrimSpace(target.ThreadTitle),
		MessageID:        strings.TrimSpace(action.MessageID),
		Stage:            turnpatchruntime.FlowStageApplied,
		PatchID:          strings.TrimSpace(patchID),
		CreatedAt:        now,
		UpdatedAt:        now,
		ExpiresAt:        now.Add(turnPatchFlowTTL),
	}
	a.turnPatchRuntime.ActiveFlows[flow.RequestID] = flow
	return a.beginTurnPatchRollbackLocked(action, flow, patchID)
}

func (a *App) openTurnPatchFlowLocked(action control.Action) []eventcontract.Event {
	target, blocked := a.turnPatchResolveTargetLocked(action, true)
	if blocked != nil {
		return blocked
	}
	if flow := a.editingTurnPatchFlowForSurfaceLocked(action.SurfaceSessionID); flow != nil {
		a.ensureSurfaceRouteForNotice(action)
		return []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_flow_active", "当前已有未提交的 patch 卡，请先提交或取消。"),
		}
	}
	storage := a.turnPatchRuntime.Storage
	preview, err := storage.PreviewLatestAssistantTurn(target.ThreadID)
	if err != nil {
		a.ensureSurfaceRouteForNotice(action)
		return []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_preview_failed", turnPatchOpenFailureText(err)),
		}
	}
	candidates := turnPatchDetectCandidates(preview)
	if len(candidates) == 0 {
		a.ensureSurfaceRouteForNotice(action)
		return []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_no_candidate", "当前会话最新 assistant turn 没有命中可修补候选点。"),
		}
	}
	flow := a.newTurnPatchFlowLocked(action, target, preview, candidates)
	return []eventcontract.Event{
		turnPatchRequestEvent(action.SurfaceSessionID, action.MessageID, turnPatchRequestView(flow), action.IsCardAction()),
	}
}

func (a *App) handleTurnPatchRequestActionLocked(action control.Action, flow *turnpatchruntime.FlowRecord) []eventcontract.Event {
	if flow == nil {
		return nil
	}
	if !flow.ExpiresAt.IsZero() && !flow.ExpiresAt.After(time.Now().UTC()) {
		delete(a.turnPatchRuntime.ActiveFlows, flow.RequestID)
		a.ensureSurfaceRouteForNotice(action)
		return []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_expired", "这张 patch 卡已失效，请重新发送 `/bendtomywill`。"),
		}
	}
	if flow.SurfaceSessionID != "" && strings.TrimSpace(flow.SurfaceSessionID) != strings.TrimSpace(action.SurfaceSessionID) {
		a.ensureSurfaceRouteForNotice(action)
		return []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_expired", "这张 patch 卡已失效，请重新发送 `/bendtomywill`。"),
		}
	}
	actorUserID := strings.TrimSpace(firstNonEmpty(action.ActorUserID, a.service.SurfaceActorUserID(action.SurfaceSessionID)))
	if owner := strings.TrimSpace(flow.OwnerUserID); owner != "" && actorUserID != "" && owner != actorUserID {
		a.ensureSurfaceRouteForNotice(action)
		return []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_unauthorized", "这张 patch 卡只允许发起者本人操作。"),
		}
	}
	if revision := turnPatchRequestRevision(action); revision > 0 && revision != flow.Revision {
		a.ensureSurfaceRouteForNotice(action)
		return []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "request_card_expired", "这张 patch 卡已过期，请在最新卡片上继续。"),
		}
	}
	if strings.TrimSpace(action.MessageID) != "" {
		flow.MessageID = strings.TrimSpace(action.MessageID)
	}
	if flow.Stage != turnpatchruntime.FlowStageEditing {
		a.ensureSurfaceRouteForNotice(action)
		return []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_expired", "这张 patch 卡已结束，请重新发送 `/bendtomywill`。"),
		}
	}
	switch action.Kind {
	case control.ActionRespondRequest:
		return a.respondTurnPatchRequestLocked(action, flow)
	case control.ActionControlRequest:
		return a.controlTurnPatchRequestLocked(action, flow)
	default:
		return nil
	}
}

func (a *App) respondTurnPatchRequestLocked(action control.Action, flow *turnpatchruntime.FlowRecord) []eventcontract.Event {
	currentIdx := turnPatchCurrentQuestionIndex(flow)
	if currentIdx < 0 || currentIdx >= len(flow.Candidates) {
		a.ensureSurfaceRouteForNotice(action)
		return []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_invalid", "当前 patch 卡缺少有效候选点，请重新发送 `/bendtomywill`。"),
		}
	}
	answers := turnPatchAnswersForAction(action)
	answered := false
	for _, candidate := range flow.Candidates {
		value := strings.TrimSpace(answers[candidate.QuestionID])
		if value == "" {
			continue
		}
		flow.Answers[candidate.QuestionID] = value
		answered = true
	}
	current := flow.Candidates[currentIdx]
	if strings.TrimSpace(flow.Answers[current.QuestionID]) == "" {
		flow.Answers[current.QuestionID] = strings.TrimSpace(current.DefaultText)
		answered = strings.TrimSpace(flow.Answers[current.QuestionID]) != ""
	}
	if !answered && !turnPatchQuestionsComplete(flow) {
		flow.Revision++
		flow.StatusText = "请先填写替换文本后再提交。"
		a.refreshTurnPatchFlowLocked(flow, turnpatchruntime.FlowStageEditing)
		return []eventcontract.Event{
			turnPatchRequestEvent(action.SurfaceSessionID, action.MessageID, turnPatchRequestView(flow), true),
		}
	}
	flow.StatusText = ""
	flow.CurrentQuestionIndex = turnPatchCurrentQuestionIndex(flow)
	if !turnPatchQuestionsComplete(flow) {
		flow.Revision++
		a.refreshTurnPatchFlowLocked(flow, turnpatchruntime.FlowStageEditing)
		return []eventcontract.Event{
			turnPatchRequestEvent(action.SurfaceSessionID, action.MessageID, turnPatchRequestView(flow), true),
		}
	}
	return a.beginTurnPatchApplyLocked(action, flow)
}

func (a *App) controlTurnPatchRequestLocked(action control.Action, flow *turnpatchruntime.FlowRecord) []eventcontract.Event {
	requestControl := action.RequestControl
	if requestControl == nil {
		return nil
	}
	switch frontstagecontract.NormalizeRequestControlToken(requestControl.Control) {
	case frontstagecontract.NormalizeRequestControlToken(frontstagecontract.RequestControlCancelTurn),
		frontstagecontract.NormalizeRequestControlToken(frontstagecontract.RequestControlCancelRequest):
		flow.Stage = turnpatchruntime.FlowStageCancelled
		flow.StatusText = ""
		a.refreshTurnPatchFlowLocked(flow, turnpatchruntime.FlowStageCancelled)
		return []eventcontract.Event{
			turnPatchPageEvent(action.SurfaceSessionID, turnPatchCancelledPageView(flow), true),
		}
	default:
		a.ensureSurfaceRouteForNotice(action)
		return []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_invalid", "当前 patch 卡不支持这个控制动作。"),
		}
	}
}

func (a *App) beginTurnPatchApplyLocked(action control.Action, flow *turnpatchruntime.FlowRecord) []eventcontract.Event {
	target, blocked := a.turnPatchResolveTargetLocked(action, true)
	if blocked != nil {
		if len(blocked) != 0 && blocked[0].Notice != nil && strings.TrimSpace(blocked[0].Notice.Code) == "turn_patch_busy" {
			flow.Revision++
			flow.StatusText = "当前实例暂时不空闲，请等本轮 turn、请求或排队输入清空后点击“重新提交”。"
			a.refreshTurnPatchFlowLocked(flow, turnpatchruntime.FlowStageEditing)
			return []eventcontract.Event{
				turnPatchRequestEvent(action.SurfaceSessionID, action.MessageID, turnPatchRequestView(flow), true),
			}
		}
		text := "当前 attached thread 已不再满足修补条件，请重新发送 `/bendtomywill`。"
		if len(blocked) != 0 && blocked[0].Notice != nil && strings.TrimSpace(blocked[0].Notice.Text) != "" {
			text = blocked[0].Notice.Text
		}
		a.refreshTurnPatchFlowLocked(flow, turnpatchruntime.FlowStageFailed)
		return []eventcontract.Event{
			turnPatchPageEvent(action.SurfaceSessionID, turnPatchFailedPageView(flow, "当前会话修补失败", text), true),
		}
	}
	if strings.TrimSpace(target.ThreadID) != strings.TrimSpace(flow.ThreadID) || strings.TrimSpace(target.Instance.InstanceID) != strings.TrimSpace(flow.InstanceID) {
		flow.Stage = turnpatchruntime.FlowStageFailed
		a.refreshTurnPatchFlowLocked(flow, turnpatchruntime.FlowStageFailed)
		return []eventcontract.Event{
			turnPatchPageEvent(action.SurfaceSessionID, turnPatchFailedPageView(flow, "当前会话修补失败", "当前 attached thread 已变化，请重新发送 `/bendtomywill`。"), true),
		}
	}
	tx := a.newTurnPatchTransactionLocked(flow, turnpatchruntime.TransactionKindApply)
	a.refreshTurnPatchFlowLocked(flow, turnpatchruntime.FlowStageApplying)
	go a.runTurnPatchApplyTransaction(tx.ID)
	return []eventcontract.Event{
		turnPatchPageEvent(action.SurfaceSessionID, turnPatchApplyingPageView(flow, true), true),
	}
}

func (a *App) beginTurnPatchRollbackLocked(action control.Action, flow *turnpatchruntime.FlowRecord, patchID string) []eventcontract.Event {
	target, blocked := a.turnPatchResolveTargetLocked(action, true)
	if blocked != nil {
		return blocked
	}
	flow.InstanceID = strings.TrimSpace(target.Instance.InstanceID)
	flow.ThreadID = strings.TrimSpace(target.ThreadID)
	flow.ThreadTitle = strings.TrimSpace(target.ThreadTitle)
	flow.OwnerUserID = strings.TrimSpace(firstNonEmpty(flow.OwnerUserID, action.ActorUserID))
	flow.SurfaceSessionID = strings.TrimSpace(firstNonEmpty(flow.SurfaceSessionID, action.SurfaceSessionID))
	flow.PatchID = strings.TrimSpace(firstNonEmpty(patchID, flow.PatchID))
	flow.MessageID = strings.TrimSpace(firstNonEmpty(action.MessageID, flow.MessageID))
	a.refreshTurnPatchFlowLocked(flow, turnpatchruntime.FlowStageRollbackRunning)
	tx := a.newTurnPatchTransactionLocked(flow, turnpatchruntime.TransactionKindRollback)
	go a.runTurnPatchRollbackTransaction(tx.ID)
	return []eventcontract.Event{
		turnPatchPageEvent(action.SurfaceSessionID, turnPatchRollbackRunningPageView(flow, action.IsCardAction()), action.IsCardAction()),
	}
}

func (a *App) newTurnPatchFlowLocked(action control.Action, target *turnPatchTarget, preview *codexstate.TurnPatchPreview, candidates []turnpatchruntime.Candidate) *turnpatchruntime.FlowRecord {
	now := time.Now().UTC()
	flow := &turnpatchruntime.FlowRecord{
		FlowID:           a.nextTurnPatchFlowIDLocked(),
		RequestID:        a.nextTurnPatchRequestIDLocked(),
		InstanceID:       strings.TrimSpace(target.Instance.InstanceID),
		SurfaceSessionID: strings.TrimSpace(target.Surface.SurfaceSessionID),
		OwnerUserID:      strings.TrimSpace(action.ActorUserID),
		ThreadID:         strings.TrimSpace(target.ThreadID),
		ThreadTitle:      strings.TrimSpace(target.ThreadTitle),
		TurnID:           strings.TrimSpace(preview.TurnID),
		RolloutDigest:    strings.TrimSpace(preview.RolloutDigest),
		MessageID:        strings.TrimSpace(action.MessageID),
		Revision:         1,
		Answers:          map[string]string{},
		Candidates:       append([]turnpatchruntime.Candidate(nil), candidates...),
		Stage:            turnpatchruntime.FlowStageEditing,
		CreatedAt:        now,
		UpdatedAt:        now,
		ExpiresAt:        now.Add(turnPatchFlowTTL),
	}
	a.turnPatchRuntime.ActiveFlows[flow.RequestID] = flow
	return flow
}

func (a *App) nextTurnPatchFlowIDLocked() string {
	a.turnPatchRuntime.NextFlowSeq++
	return fmt.Sprintf("%s%d", turnPatchFlowIDPrefix, a.turnPatchRuntime.NextFlowSeq)
}

func (a *App) nextTurnPatchRequestIDLocked() string {
	a.turnPatchRuntime.NextFlowSeq++
	return fmt.Sprintf("%s%d", turnPatchRequestIDPrefix, a.turnPatchRuntime.NextFlowSeq)
}

func (a *App) refreshTurnPatchFlowLocked(flow *turnpatchruntime.FlowRecord, stage turnpatchruntime.FlowStage) {
	if flow == nil {
		return
	}
	now := time.Now().UTC()
	flow.Stage = stage
	flow.UpdatedAt = now
	flow.ExpiresAt = now.Add(turnPatchFlowTTL)
}

func (a *App) reapTurnPatchRuntimeLocked(now time.Time) {
	for requestID, flow := range a.turnPatchRuntime.ActiveFlows {
		if flow == nil {
			delete(a.turnPatchRuntime.ActiveFlows, requestID)
			continue
		}
		if flow.ExpiresAt.IsZero() || flow.ExpiresAt.After(now) {
			continue
		}
		delete(a.turnPatchRuntime.ActiveFlows, requestID)
	}
}

func (a *App) editingTurnPatchFlowForSurfaceLocked(surfaceID string) *turnpatchruntime.FlowRecord {
	surfaceID = strings.TrimSpace(surfaceID)
	if surfaceID == "" {
		return nil
	}
	for _, flow := range a.turnPatchRuntime.ActiveFlows {
		if flow == nil || flow.Stage != turnpatchruntime.FlowStageEditing {
			continue
		}
		if strings.TrimSpace(flow.SurfaceSessionID) != surfaceID {
			continue
		}
		return flow
	}
	return nil
}

func (a *App) findTurnPatchFlowByRequestIDLocked(requestID string) *turnpatchruntime.FlowRecord {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil
	}
	return a.turnPatchRuntime.ActiveFlows[requestID]
}

func (a *App) findTurnPatchFlowByFlowIDLocked(flowID string) *turnpatchruntime.FlowRecord {
	flowID = strings.TrimSpace(flowID)
	if flowID == "" {
		return nil
	}
	for _, flow := range a.turnPatchRuntime.ActiveFlows {
		if flow == nil || strings.TrimSpace(flow.FlowID) != flowID {
			continue
		}
		return flow
	}
	return nil
}

func (a *App) findTurnPatchFlowByPatchIDLocked(patchID string) *turnpatchruntime.FlowRecord {
	patchID = strings.TrimSpace(patchID)
	if patchID == "" {
		return nil
	}
	for _, flow := range a.turnPatchRuntime.ActiveFlows {
		if flow == nil || strings.TrimSpace(flow.PatchID) != patchID {
			continue
		}
		return flow
	}
	return nil
}

func (a *App) recordTurnPatchFlowMessageLocked(trackingKey, messageID string) {
	flow := a.findTurnPatchFlowByFlowIDLocked(trackingKey)
	if flow == nil {
		return
	}
	flow.MessageID = strings.TrimSpace(messageID)
}

func (a *App) turnPatchTransactionForActionLocked(action control.Action) *turnpatchruntime.Transaction {
	instanceID := strings.TrimSpace(a.service.AttachedInstanceID(action.SurfaceSessionID))
	if instanceID == "" {
		return nil
	}
	return a.turnPatchRuntime.ActiveTx[instanceID]
}

func (a *App) turnPatchResolveTargetLocked(action control.Action, requireIdle bool) (*turnPatchTarget, []eventcontract.Event) {
	if a.turnPatchRuntime.Storage == nil {
		a.ensureSurfaceRouteForNotice(action)
		return nil, []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_unavailable", "当前实例没有启用 thread patch 存储，暂时不能执行 `/bendtomywill`。"),
		}
	}
	surface := a.service.Surface(action.SurfaceSessionID)
	if surface == nil || strings.TrimSpace(surface.AttachedInstanceID) == "" {
		a.ensureSurfaceRouteForNotice(action)
		return nil, []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_unattached", "当前飞书会话还没有 attached 实例，不能执行 `/bendtomywill`。"),
		}
	}
	if state.NormalizeProductMode(surface.ProductMode) == state.ProductModeVSCode {
		a.ensureSurfaceRouteForNotice(action)
		return nil, []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_vscode_unsupported", "VS Code surface 暂不支持 `/bendtomywill`，请切回 normal 模式后再试。"),
		}
	}
	inst := a.service.Instance(surface.AttachedInstanceID)
	if inst == nil || !inst.Online {
		a.ensureSurfaceRouteForNotice(action)
		return nil, []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_instance_offline", "当前 attached 实例不在线，不能执行 `/bendtomywill`。"),
		}
	}
	threadID := strings.TrimSpace(surface.SelectedThreadID)
	if threadID == "" {
		a.ensureSurfaceRouteForNotice(action)
		return nil, []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_thread_missing", "当前 surface 还没有选中的会话，不能执行 `/bendtomywill`。"),
		}
	}
	if requireIdle && a.turnPatchInstanceBusyLocked(inst.InstanceID, action.SurfaceSessionID) {
		a.ensureSurfaceRouteForNotice(action)
		return nil, []eventcontract.Event{
			turnPatchNoticeEvent(action.SurfaceSessionID, "turn_patch_busy", "当前实例还不空闲，请等 turn、请求和排队输入清空后再试。"),
		}
	}
	return &turnPatchTarget{
		Surface:     surface,
		Instance:    inst,
		ThreadID:    threadID,
		ThreadTitle: a.turnPatchThreadTitleLocked(surface, inst),
	}, nil
}

func (a *App) turnPatchThreadTitleLocked(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) string {
	if surface == nil {
		return ""
	}
	if snapshot := a.service.SurfaceSnapshot(surface.SurfaceSessionID); snapshot != nil {
		if title := strings.TrimSpace(snapshot.Attachment.SelectedThreadTitle); title != "" {
			return title
		}
	}
	threadID := strings.TrimSpace(surface.SelectedThreadID)
	if inst == nil || threadID == "" {
		return threadID
	}
	thread := inst.Threads[threadID]
	if thread == nil {
		return threadID
	}
	return firstNonEmpty(
		strings.TrimSpace(thread.Name),
		strings.TrimSpace(thread.FirstUserMessage),
		strings.TrimSpace(thread.LastUserMessage),
		strings.TrimSpace(thread.Preview),
		threadID,
	)
}

func (a *App) turnPatchInstanceBusyLocked(instanceID, initiatorSurfaceID string) bool {
	if strings.TrimSpace(instanceID) == "" {
		return true
	}
	if a.turnPatchRuntime.ActiveTx[strings.TrimSpace(instanceID)] != nil {
		return true
	}
	if a.codexUpgradeRuntime.Active != nil || a.upgradeRuntime.StartInFlight || a.upgradeRuntime.CheckInFlight {
		return true
	}
	if a.activeUpgradeOwnerFlowLocked() != nil || a.activeCodexUpgradeOwnerFlowLocked() != nil {
		return true
	}
	inst := a.service.Instance(instanceID)
	if inst == nil || !inst.Online || strings.TrimSpace(inst.ActiveTurnID) != "" {
		return true
	}
	for _, pending := range a.service.PendingRemoteTurns() {
		if strings.TrimSpace(pending.InstanceID) == strings.TrimSpace(instanceID) {
			return true
		}
	}
	for _, active := range a.service.ActiveRemoteTurns() {
		if strings.TrimSpace(active.InstanceID) == strings.TrimSpace(instanceID) {
			return true
		}
	}
	if a.service.InstanceHasPendingCompact(instanceID) || a.service.InstanceHasPendingSteer(instanceID) {
		return true
	}
	for _, surface := range a.service.Surfaces() {
		if surface == nil || strings.TrimSpace(surface.AttachedInstanceID) != strings.TrimSpace(instanceID) {
			continue
		}
		if strings.TrimSpace(surface.SurfaceSessionID) == strings.TrimSpace(initiatorSurfaceID) {
			if flow := a.editingTurnPatchFlowForSurfaceLocked(surface.SurfaceSessionID); flow != nil {
				continue
			}
		}
		switch {
		case surface.Abandoning,
			surface.PendingHeadless != nil,
			surface.ActiveRequestCapture != nil,
			len(surface.PendingRequests) != 0,
			surface.ActiveQueueItemID != "",
			len(surface.QueuedQueueItemIDs) != 0,
			surface.DispatchMode != state.DispatchModeNormal:
			return true
		}
	}
	return false
}

func turnPatchRequestIDForAction(action control.Action) string {
	switch action.Kind {
	case control.ActionRespondRequest:
		if action.Request != nil {
			return strings.TrimSpace(action.Request.RequestID)
		}
	case control.ActionControlRequest:
		if action.RequestControl != nil {
			return strings.TrimSpace(action.RequestControl.RequestID)
		}
	}
	return ""
}

func turnPatchRequestRevision(action control.Action) int {
	switch action.Kind {
	case control.ActionRespondRequest:
		if action.Request != nil {
			return action.Request.RequestRevision
		}
	case control.ActionControlRequest:
		if action.RequestControl != nil {
			return action.RequestControl.RequestRevision
		}
	}
	return 0
}

func turnPatchAnswersForAction(action control.Action) map[string]string {
	raw := map[string][]string{}
	switch action.Kind {
	case control.ActionRespondRequest:
		if action.Request != nil && len(action.Request.Answers) != 0 {
			raw = action.Request.Answers
		}
	}
	if len(action.RequestAnswers) != 0 {
		raw = action.RequestAnswers
	}
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for key, values := range raw {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			out[strings.TrimSpace(key)] = value
			break
		}
	}
	return out
}

func turnPatchCommandArgs(action control.Action) []string {
	fields := strings.Fields(strings.TrimSpace(action.Text))
	switch action.Kind {
	case control.ActionTurnPatchCommand:
		if len(fields) <= 1 {
			return nil
		}
		return fields[1:]
	case control.ActionTurnPatchRollback:
		if len(fields) <= 2 {
			return nil
		}
		return fields[2:]
	default:
		return nil
	}
}

func turnPatchOpenFailureText(err error) string {
	switch {
	case err == nil:
		return "当前会话暂时不能打开 patch 卡。"
	case strings.Contains(err.Error(), codexstate.ErrTurnPatchLatestTurnNotFound.Error()),
		strings.Contains(err.Error(), codexstate.ErrTurnPatchRolloutNotFound.Error()):
		return "当前会话还没有可修补的最新 assistant turn。"
	default:
		return "读取当前会话失败：" + err.Error()
	}
}

func turnPatchApplyFailureLines(err error) []string {
	switch {
	case err == nil:
		return []string{"修补失败。"}
	case strings.Contains(err.Error(), codexstate.ErrTurnPatchRolloutDigestMismatch.Error()),
		strings.Contains(err.Error(), codexstate.ErrTurnPatchLatestTurnNotFound.Error()),
		strings.Contains(err.Error(), codexstate.ErrTurnPatchReplacementNotFound.Error()),
		strings.Contains(err.Error(), codexstate.ErrTurnPatchDuplicateDrift.Error()):
		return []string{"当前会话内容已变化，请重新发送 `/bendtomywill` 后再试。"}
	default:
		return []string{"修补失败：" + err.Error()}
	}
}

func turnPatchRollbackFailureLines(err error) []string {
	switch {
	case err == nil:
		return []string{"回滚失败。"}
	case strings.Contains(err.Error(), codexstate.ErrTurnPatchNotLatest.Error()):
		return []string{"最近一次修补已经变化，当前不能继续回滚。"}
	case strings.Contains(err.Error(), codexstate.ErrTurnPatchRollbackDrift.Error()):
		return []string{"修补后的会话内容已变化，当前不能继续回滚。"}
	case strings.Contains(err.Error(), codexstate.ErrTurnPatchActorMismatch.Error()):
		return []string{"这次回滚只允许原发起者本人执行。"}
	default:
		return []string{"回滚失败：" + err.Error()}
	}
}
