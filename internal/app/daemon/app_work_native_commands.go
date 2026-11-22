package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

var errTeamNoNativeSession = errors.New("当前工作还没有原生会话；发送工作内容后会准备执行环境")

func (a *App) handleTeamNativeCommand(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	switch action.Kind {
	case control.ActionSteerAll:
		return a.steerTeamPendingInputs(ctx, source)
	case control.ActionMCPOAuthCommand:
		return a.handleTeamMCP(ctx, source, action)
	case control.ActionWorkspaceNewDir, control.ActionWorkspaceNewGit, control.ActionWorkspaceNewWorktree:
		return a.queueTeamWorkspaceWork(ctx, source, action)
	case control.ActionAutoWhipCommand, control.ActionAutoContinueCommand:
		return a.handleTeamAutomation(ctx, source, action)
	case control.ActionFollowLocal, control.ActionControlRequest, control.ActionVSCodeMigrate, control.ActionVSCodeMigrateCommand:
		return a.queueTeamPicker(ctx, source, "native.retired", "retired-native-control", 1, workui.Panel{Title: "人工接管入口已退役", Paragraphs: []string{"新版不再提供 VS Code 式原生会话接管。可以继续在飞书查看状态、审阅、暂停或继续工作。"}})

	case control.ActionSendFile:
		return a.handleTeamSendFile(ctx, source, action)
	case control.ActionAdminRoot, control.ActionAdminCommand:
		return a.handleTeamAdminCommand(ctx, source, action)
	case control.ActionUpgradeCommand:
		return a.teamUpgradeStatus(ctx, source, action)
	case control.ActionUseThread, control.ActionAttachInstance, control.ActionAttachWorkspace:
		if action.ThreadID != "" {
			if err := a.navigateFromSource(ctx, source, "select_context", "", action.ThreadID, source.NavigationRevision); err != nil {
				return "", err
			}
		}
		return a.queuePrivatePanel(ctx, source, "contexts", "")
	case control.ActionDetach, control.ActionWorkspaceDetach:
		if err := a.navigateFromSource(ctx, source, "return_personal", "", "", source.NavigationRevision); err != nil {
			return "", err
		}
		return a.queuePrivatePanel(ctx, source, "menu", "已返回私人工作。项目中的工作会按原计划继续。")
	case control.ActionDebugCommand:
		return a.inspectTeamNativeWorker(ctx, source)
	case control.ActionGoalCommand:
		return a.teamNativeGoal(ctx, source, action)
	case control.ActionCompact:
		return a.queueTeamNativeMaintenance(ctx, source, agentproto.CommandThreadCompactStart)
	case control.ActionReviewCommand:
		return a.queueTeamReview(ctx, source, action)
	default:
		return "", fmt.Errorf("这个命令尚无可验证的团队执行入口；可以通过 /menu 查看当前工作、目录和设置。")
	}
}
func (a *App) teamNativeTarget(tx *workstore.Tx, source teamHumanSource) (work.WorkerSession, workstore.CarrierRegistration, error) {
	context, _, err := worksettings.FrozenContext(tx, source.Authority)
	if err != nil {
		return work.WorkerSession{}, workstore.CarrierRegistration{}, err
	}
	if context.Purpose == work.PurposeControl || context.Purpose == work.PurposeReview {
		return work.WorkerSession{}, workstore.CarrierRegistration{}, workstore.ErrScopeDenied
	}
	workers, err := tx.ListWorkers(context.ScopeID)
	if err != nil {
		return work.WorkerSession{}, workstore.CarrierRegistration{}, err
	}
	var selected work.WorkerSession
	for _, worker := range workers {
		if worker.ContextID != context.ID || worker.State == work.WorkerRetired {
			continue
		}
		if selected.ID != "" {
			return selected, workstore.CarrierRegistration{}, fmt.Errorf("当前工作存在多个执行身份，需先核对恢复状态")
		}
		selected = worker
	}
	if selected.ID == "" {
		return selected, workstore.CarrierRegistration{}, errTeamNoNativeSession
	}
	carrier, err := tx.GetCarrier(selected.CarrierIncarnation)
	if err != nil {
		return selected, carrier, err
	}
	if !carrier.Matches(selected) {
		return selected, carrier, workstore.ErrConflict
	}
	return selected, carrier, nil
}
func (a *App) inspectTeamNativeWorker(ctx context.Context, source teamHumanSource) (string, error) {
	var request workruntime.InspectionRequest
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		worker, carrier, e := a.teamNativeTarget(tx, source)
		if e != nil {
			return e
		}
		request = workruntime.InspectionRequest{Carrier: carrier, Binding: carrier.Owner, SessionID: worker.NativeSessionID, LocationID: worker.LocationID, LocationGeneration: worker.LocationGeneration}
		return nil
	})
	if err != nil {
		return "", err
	}
	observation, err := a.teamRuntime.driver.Inspect(ctx, request)
	if err != nil {
		return "", err
	}
	note := "会话还有执行，或尚未确认停止。"
	if observation.NativeStopped {
		note = "会话当前空闲。"
	}
	if !observation.ContinuationStopped {
		note += "持续执行仍未停止。"
	}
	if !observation.BackgroundWorkStopped {
		note += "后台工作仍未确认停止。"
	}
	note += "这次检查没有中断工作。"
	return a.queuePrivatePanel(ctx, source, "status", note)
}
func (a *App) teamNativeGoal(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	a.mu.Lock()
	backend := a.workBotSettingsLocked(source.Gateway.GatewayRef).Backend
	a.mu.Unlock()
	if backend != agentproto.BackendCodex {
		return "", fmt.Errorf("会话目标仅适用于 Codex；请先切换执行后端")
	}
	fields := strings.Fields(action.Text)
	if len(fields) == 2 {
		switch strings.ToLower(fields[1]) {
		case "new", "set", "edit":
			return a.queueTeamGoalPanel(ctx, source, strings.ToLower(fields[1]), "")
		case "clear", "stop":
			return a.queueTeamGoalPanel(ctx, source, "clear", "")
		}
	}
	parsedKind, parsedGoal, parseErr := parseTeamGoal(action.Text)
	if parseErr != nil {
		return "", parseErr
	}
	if parsedKind == agentproto.CommandThreadGoalGet {
		missing := false
		if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			_, _, err := a.teamNativeTarget(tx, source)
			if errors.Is(err, errTeamNoNativeSession) {
				missing = true
				return nil
			}
			return err
		}); err != nil {
			return "", err
		}
		if missing {
			return a.queueTeamGoalPanel(ctx, source, "overview", "当前工作还没有原生会话。可以先设置目标和预算，再开始工作。")
		}
	}
	if parsedKind == agentproto.CommandThreadGoalSet && parsedGoal.Status == "" {
		a.mu.Lock()
		settings := a.workBotSettingsLocked(source.Gateway.GatewayRef)
		a.mu.Unlock()
		if settings.Backend != agentproto.BackendCodex {
			return "", fmt.Errorf("会话目标仅适用于 Codex；当前后端未设置目标")
		}

		idle := false
		err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			worker, _, e := a.teamNativeTarget(tx, source)
			if e != nil {
				idle = true
				return nil
			}
			_, e = tx.GetOccupiedAttemptForWorker(worker.ID)
			if e == workstore.ErrNotFound {
				idle = true
				return nil
			}
			return e
		})
		if err != nil {
			return "", err
		}
		if idle {
			return a.queueTeamIdleGoal(ctx, source, parsedGoal)
		}
	}

	if parsedKind == agentproto.CommandThreadGoalSet && parsedGoal.Status == "active" {
		idle := false
		err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			if e := a.validateTeamGoalSourceSnapshot(tx, source); e != nil {
				return e
			}
			worker, carrier, e := a.teamNativeTarget(tx, source)
			if e != nil {
				return e
			}
			if _, e = tx.GetOccupiedAttemptForWorker(worker.ID); e == nil {
				return nil
			} else if e != workstore.ErrNotFound {
				return e
			}
			grant, e := tx.GetContinuationGrant(carrier.Incarnation, worker.NativeSessionID)
			if e != nil {
				return e
			}
			if grant.Status != "paused" || !grant.Reconciled || grant.GoalVersion <= 0 || grant.TokenBudget <= 0 || grant.Objective == "" {
				return fmt.Errorf("请先查询并确认当前已暂停的会话目标")
			}
			budget := grant.TokenBudget
			parsedGoal.Objective = grant.Objective
			parsedGoal.TokenBudget = &budget
			parsedGoal.Expected = teamGoalPrecondition(worker, carrier, grant)
			idle = true
			return nil
		})
		if err != nil {
			return "", err
		}
		if idle {
			return a.queueTeamIdleGoal(ctx, source, parsedGoal)
		}
	}

	pendingViewID := workui.ID(source.Key, "session-goal", "overview")
	noClearTarget := false
	var command agentproto.Command
	var instance string
	send := false
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if parsedKind != agentproto.CommandThreadGoalGet {
			if e := a.validateTeamGoalSourceSnapshot(tx, source); e != nil {
				return e
			}
		}
		worker, carrier, e := a.teamNativeTarget(tx, source)
		if e != nil {
			return e
		}
		if worker.Backend != work.BackendCodex || worker.NativeSessionID == "" || worker.Controller != work.ControllerAutomatic {
			return fmt.Errorf("会话目标只适用于已准备好的 Codex 自动执行会话")
		}
		attempt, e := tx.GetOccupiedAttemptForWorker(worker.ID)
		ownerOnly := e == workstore.ErrNotFound && (parsedKind != agentproto.CommandThreadGoalSet || parsedGoal.Status == "paused")
		if e != nil && !ownerOnly {
			return e
		}
		if !ownerOnly && attempt.State != work.AttemptRunning {
			return fmt.Errorf("当前轮次尚未实际运行，目标操作未发送")
		}
		binding := carrier.Owner
		if !ownerOnly {
			binding.AttemptID = attempt.ID
			binding.RunRef = attempt.RunRef.RunID
		}
		command = agentproto.Command{CommandID: teamID("human-goal", source.Key), Kind: agentproto.CommandThreadGoalGet, Execution: &binding, Target: agentproto.Target{ThreadID: worker.NativeSessionID}, IssuedAt: time.Now().UTC()}
		command.Kind, command.Goal, e = parseTeamGoal(action.Text)
		if e != nil {
			return e
		}
		if command.Kind == agentproto.CommandThreadGoalGet || command.Kind == agentproto.CommandThreadGoalClear {
			command.Goal.Expected, e = tx.CaptureGoalControlSnapshot(worker, carrier)
			if e != nil {
				return e
			}
			if command.Kind == agentproto.CommandThreadGoalClear && command.Goal.Expected.NoGrant && command.Goal.Expected.ObservationID == "" {
				noClearTarget = true
				return nil
			}
		}
		if command.Kind == agentproto.CommandThreadGoalSet {
			grant, lookup := tx.GetContinuationGrant(worker.CarrierIncarnation, worker.NativeSessionID)
			if command.Goal.Status != "" {
				if lookup != nil || (!ownerOnly && grant.Binding != binding) || (ownerOnly && !carrier.Owner.SameOwner(grant.Binding)) || grant.GoalVersion < 1 {
					return fmt.Errorf("当前尚无已确认的会话目标，请先用 /goal 查询")
				}
				command.Goal.Expected = teamGoalPrecondition(worker, carrier, grant)
				command.Goal.Objective = grant.Objective
				budget := grant.TokenBudget
				command.Goal.TokenBudget = &budget
			}
			if command.Goal.TokenBudget == nil && lookup == nil && grant.Binding == binding && grant.TokenBudget > 0 && strings.Contains(action.Text, " edit ") {
				budget := grant.TokenBudget
				command.Goal.TokenBudget = &budget
			}
			if command.Goal.TokenBudget == nil {
				return fmt.Errorf("请用 --budget 明确填写会话持续执行的 token 预算")
			}
		}

		if command.Kind == agentproto.CommandThreadGoalSet && !ownerOnly {
			if carrier.Owner.BotGeneration != source.Gateway.BotGeneration {
				return fmt.Errorf("身份配置已更新，请先核对并重建当前执行环境")
			}
			gates, e := tx.ListPauseGates(worker.ScopeID)
			if e != nil {
				return e
			}
			allowed, e := work.GatesAllowRunForContext(worker.ScopeID, attempt.TaskID, worker.ContextID, gates)
			if e != nil {
				return e
			}
			if !allowed {
				return fmt.Errorf("当前工作已暂停，不能设置持续执行目标")
			}
			if _, e = tx.GetRuntimeGate(worker.ID); e == nil {
				return fmt.Errorf("原生状态待核对，不能设置持续执行目标")
			} else if e != workstore.ErrNotFound {
				return e
			}
		}
		if existing, e := tx.GetOperation(command.CommandID); e == nil {
			if existing.Kind != "native.user.goal" || existing.TargetID != worker.ID {
				return workstore.ErrIdempotencyConflict
			}
			if e = a.queueTeamGoalPanelTx(tx, source, "overview", "会话目标操作已记录，正在等待原生反馈。", pendingViewID); e != nil {
				return e
			}
			return attachTeamGoalPending(tx, command.CommandID, pendingViewID, time.Now().UTC())
		} else if e != workstore.ErrNotFound {
			return e
		}
		_, humanSource, e := tx.ValidatePrivateAction(source.Authority)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(command)
		if e != nil {
			return e
		}
		body := work.NewContent(command.CommandID+":body", worker.ScopeID, "application/json", raw, time.Now().UTC())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		actor := work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}
		if ownerOnly {
			actor = work.Actor{Kind: work.ActorSystem, ID: "user-session-goal"}
		}
		_, _, e = tx.PutOperation(work.Operation{ID: command.CommandID, ScopeID: worker.ScopeID, ContextID: worker.ContextID, Actor: actor, Kind: "native.user.goal", Steps: []work.OperationStep{{ID: "human_source", State: work.OperationConfirmed, Receipt: stringMustJSON(humanSource)}}, TargetID: worker.ID, TargetGeneration: worker.Generation, RequestHash: body.SHA256, RequestRef: body.ID, IdempotencyKey: command.CommandID, State: work.OperationSending, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, 0)
		if e != nil {
			return e
		}
		if e = a.queueTeamGoalPanelTx(tx, source, "overview", "会话目标操作已记录，正在等待原生反馈。", pendingViewID); e != nil {
			return e
		}
		if e = attachTeamGoalPending(tx, command.CommandID, pendingViewID, time.Now().UTC()); e != nil {
			return e
		}
		instance = worker.InstanceID
		send = true
		return nil
	})
	if err != nil {
		return "", err
	}
	if noClearTarget {
		return a.queueTeamGoalPanel(ctx, source, "overview", "尚未核对当前原生目标，请点击「刷新会话目标」查询后再确认清除。")
	}
	if send {
		if err = a.teamRuntime.driver.SendCommand(ctx, instance, command); err != nil {
			_ = a.teamRuntime.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
				op, e := tx.GetOperation(command.CommandID)
				if e != nil {
					return e
				}
				before := op.Revision
				op.Revision++
				op.State = work.OperationUnknown
				op.LastError = err.Error()
				_, _, e = tx.PutOperation(op, before)
				return e
			})
			return "", err
		}
	}
	return pendingViewID, nil
}
