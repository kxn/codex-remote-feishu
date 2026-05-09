package daemon

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

const (
	onboardingStageRuntimeRequirements = "runtime_requirements"
	onboardingStageConnect             = "connect"
	onboardingStageAutoConfig          = "auto_config"
	onboardingStageMenu                = "menu"
	onboardingStageAutostart           = "autostart"
	onboardingStageVSCode              = "vscode"
	onboardingStageDone                = "done"

	onboardingStageStatusBlocked       = "blocked"
	onboardingStageStatusPending       = "pending"
	onboardingStageStatusComplete      = "complete"
	onboardingStageStatusDeferred      = "deferred"
	onboardingStageStatusNotApplicable = "not_applicable"

	onboardingMachineStateBlocked                = "blocked"
	onboardingMachineStateUsable                 = "usable"
	onboardingMachineStateUsableWithPendingItems = "usable_with_pending_items"
	onboardingMachineStateCompleted              = "completed"

	onboardingDecisionAutostartEnabled  = "enabled"
	onboardingDecisionDeferred          = "deferred"
	onboardingDecisionVSCodeManaged     = "managed_shim"
	onboardingDecisionVSCodeRemoteOnly  = "remote_only"
	onboardingDecisionPermissionSkipped = "skipped"
	onboardingDecisionMenuConfirmed     = "confirmed"
)

type onboardingWorkflowResponse struct {
	Apps                []adminFeishuAppSummary           `json:"apps,omitempty"`
	SelectedAppID       string                            `json:"selectedAppId,omitempty"`
	CurrentStage        string                            `json:"currentStage"`
	MachineState        string                            `json:"machineState"`
	Completion          onboardingWorkflowCompletionView  `json:"completion"`
	RuntimeRequirements runtimeRequirementsResponse       `json:"runtimeRequirements"`
	App                 *onboardingWorkflowAppView        `json:"app,omitempty"`
	Autostart           onboardingWorkflowMachineStepView `json:"autostart"`
	VSCode              onboardingWorkflowMachineStepView `json:"vscode"`
	Guide               onboardingWorkflowGuideView       `json:"guide,omitempty"`
	Stages              []onboardingWorkflowStageView     `json:"stages,omitempty"`
}

type onboardingWorkflowCompletionView struct {
	SetupRequired  bool   `json:"setupRequired"`
	CanComplete    bool   `json:"canComplete"`
	Summary        string `json:"summary"`
	BlockingReason string `json:"blockingReason,omitempty"`
}

type onboardingWorkflowGuideView struct {
	AutoConfiguredSummary  string   `json:"autoConfiguredSummary,omitempty"`
	RemainingManualActions []string `json:"remainingManualActions,omitempty"`
	RecommendedNextStep    string   `json:"recommendedNextStep,omitempty"`
}

type onboardingWorkflowDecisionView struct {
	Value     string     `json:"value,omitempty"`
	DecidedAt *time.Time `json:"decidedAt,omitempty"`
}

type onboardingWorkflowStageView struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Status         string   `json:"status"`
	Summary        string   `json:"summary"`
	Blocking       bool     `json:"blocking,omitempty"`
	Optional       bool     `json:"optional,omitempty"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type onboardingWorkflowPermissionView struct {
	onboardingWorkflowStageView
	MissingScopes []feishuAppPermissionCheckItem `json:"missingScopes,omitempty"`
	GrantJSON     string                         `json:"grantJSON,omitempty"`
	LastCheckedAt *time.Time                     `json:"lastCheckedAt,omitempty"`
}

type onboardingWorkflowMachineStepView struct {
	onboardingWorkflowStageView
	Decision  *onboardingWorkflowDecisionView `json:"decision,omitempty"`
	Autostart *autostartResponse              `json:"autostart,omitempty"`
	VSCode    *vscodeDetectResponse           `json:"vscode,omitempty"`
	Error     string                          `json:"error,omitempty"`
}

type onboardingWorkflowAutoConfigView struct {
	onboardingWorkflowStageView
	Decision *onboardingWorkflowDecisionView `json:"decision,omitempty"`
	Plan     *feishu.AutoConfigPlan          `json:"plan,omitempty"`
	Error    string                          `json:"error,omitempty"`
}

type onboardingWorkflowAppView struct {
	App        adminFeishuAppSummary            `json:"app"`
	Connection onboardingWorkflowStageView      `json:"connection"`
	AutoConfig onboardingWorkflowAutoConfigView `json:"autoConfig"`
	Menu       onboardingWorkflowStageView      `json:"menu"`
}

func (a *App) handleOnboardingWorkflow(w http.ResponseWriter, r *http.Request) {
	payload, err := a.buildOnboardingWorkflow(strings.TrimSpace(r.URL.Query().Get("app")))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "onboarding_workflow_unavailable",
			Message: "failed to build onboarding workflow",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) buildOnboardingWorkflow(preferredAppID string) (onboardingWorkflowResponse, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return onboardingWorkflowResponse{}, err
	}
	apps, err := a.adminFeishuApps(loaded)
	if err != nil {
		return onboardingWorkflowResponse{}, err
	}
	runtimeReqs, err := a.buildRuntimeRequirementsResponse()
	if err != nil {
		return onboardingWorkflowResponse{}, err
	}

	selectedAppID, selectedApp := selectOnboardingApp(apps, preferredAppID)
	appState := onboardingAppState(loaded.Config, selectedAppID)
	connection := buildOnboardingConnectionStage(selectedApp)
	autoConfig := buildBlockedOnboardingAutoConfigStage("请先完成连接验证。")
	if connection.Status == onboardingStageStatusComplete && selectedApp != nil {
		autoConfig = a.buildOnboardingAutoConfigStage(selectedAppID, appState)
	}
	menu := buildBlockedOnboardingMenuStage("请先完成飞书自动配置。")
	if selectedApp != nil {
		menu = buildOnboardingMenuStage(selectedApp, autoConfig, appState)
	}

	autostartStage := a.buildOnboardingAutostartStage(loaded.Config)
	vscodeStage := a.buildOnboardingVSCodeStage(loaded.Config)

	stages := []onboardingWorkflowStageView{
		stageView(onboardingStageRuntimeRequirements, "环境检查", runtimeRequirementsSummaryStatus(runtimeReqs.Ready), runtimeReqs.Summary, !runtimeReqs.Ready, false, []string{"retry"}),
		connection,
		autoConfig.onboardingWorkflowStageView,
		menu,
		autostartStage.onboardingWorkflowStageView,
		vscodeStage.onboardingWorkflowStageView,
	}

	canComplete := runtimeReqs.Ready &&
		connection.Status == onboardingStageStatusComplete &&
		onboardingStageResolved(autoConfig.Status) &&
		onboardingStageResolved(menu.Status) &&
		machineDecisionSatisfied(autostartStage.Status) &&
		machineDecisionSatisfied(vscodeStage.Status)

	machinePending := !machineDecisionSatisfied(autostartStage.Status) || !machineDecisionSatisfied(vscodeStage.Status)
	machineState := onboardingMachineStateBlocked
	switch {
	case !runtimeReqs.Ready || connection.Status != onboardingStageStatusComplete || !onboardingStageResolved(autoConfig.Status) || !onboardingStageResolved(menu.Status):
		machineState = onboardingMachineStateBlocked
	case canComplete && !machinePending:
		machineState = onboardingMachineStateCompleted
	case machinePending:
		machineState = onboardingMachineStateUsableWithPendingItems
	default:
		machineState = onboardingMachineStateUsable
	}

	currentStage := firstPendingStageID(stages)
	if currentStage == "" {
		currentStage = onboardingStageDone
	}
	if canComplete && currentStage == onboardingStageDone {
		stages = append(stages, stageView(onboardingStageDone, "完成", onboardingStageStatusComplete, "当前 setup 已经可以完成。", false, false, []string{"complete_setup"}))
	}

	guide := buildOnboardingGuide(currentStage, connection, autoConfig, menu, autostartStage, vscodeStage, canComplete)
	completion := buildOnboardingCompletion(canComplete, runtimeReqs.Ready, connection, autoConfig, menu, autostartStage, vscodeStage)

	response := onboardingWorkflowResponse{
		Apps:                apps,
		SelectedAppID:       selectedAppID,
		CurrentStage:        currentStage,
		MachineState:        machineState,
		Completion:          completion,
		RuntimeRequirements: runtimeReqs,
		Autostart:           autostartStage,
		VSCode:              vscodeStage,
		Guide:               guide,
		Stages:              stages,
	}
	if selectedApp != nil {
		response.App = &onboardingWorkflowAppView{
			App:        *selectedApp,
			Connection: connection,
			AutoConfig: autoConfig,
			Menu:       menu,
		}
	}
	return response, nil
}

func selectOnboardingApp(apps []adminFeishuAppSummary, preferredAppID string) (string, *adminFeishuAppSummary) {
	if preferred := canonicalGatewayID(preferredAppID); preferred != "" {
		for i := range apps {
			if canonicalGatewayID(apps[i].ID) == preferred {
				return apps[i].ID, &apps[i]
			}
		}
	}
	if len(apps) == 0 {
		return "", nil
	}
	return apps[0].ID, &apps[0]
}

func buildOnboardingConnectionStage(app *adminFeishuAppSummary) onboardingWorkflowStageView {
	if app == nil {
		return stageView(onboardingStageConnect, "飞书连接", onboardingStageStatusBlocked, "还没有接入可用的飞书应用。", true, false, []string{"start_qr", "submit_manual"})
	}
	if app.RuntimeApply != nil && app.RuntimeApply.Pending {
		return stageView(onboardingStageConnect, "飞书连接", onboardingStageStatusBlocked, "当前飞书应用还在同步运行态配置，请稍后刷新。", true, false, nil)
	}
	if app.ReadOnly && app.RuntimeOnly && app.HasSecret && strings.TrimSpace(app.AppID) != "" {
		return stageView(onboardingStageConnect, "飞书连接", onboardingStageStatusComplete, "当前飞书应用由运行时接管，可直接继续后续联调。", false, false, []string{"verify"})
	}
	if app.Status != nil {
		switch app.Status.State {
		case feishu.GatewayStateConnected:
			return stageView(onboardingStageConnect, "飞书连接", onboardingStageStatusComplete, "当前飞书应用已连通运行态。", false, false, []string{"verify"})
		case feishu.GatewayStateDisabled:
			return stageView(onboardingStageConnect, "飞书连接", onboardingStageStatusBlocked, "当前飞书应用未启用，请先启用后继续。", true, false, nil)
		}
	}
	if app.VerifiedAt != nil {
		return stageView(onboardingStageConnect, "飞书连接", onboardingStageStatusComplete, "当前飞书应用连接验证已通过。", false, false, []string{"verify"})
	}
	if strings.TrimSpace(app.AppID) != "" && app.HasSecret {
		return stageView(onboardingStageConnect, "飞书连接", onboardingStageStatusBlocked, "当前飞书应用还没有完成连接验证。", true, false, []string{"verify", "submit_manual"})
	}
	return stageView(onboardingStageConnect, "飞书连接", onboardingStageStatusBlocked, "当前飞书应用信息还不完整。", true, false, []string{"submit_manual"})
}

func (a *App) buildOnboardingAutoConfigStage(gatewayID string, state config.FeishuAppOnboardingState) onboardingWorkflowAutoConfigView {
	decision := onboardingDecisionViewFromConfig(state.AutoConfigDecision)
	_, runtimeCfg, err := a.loadFeishuAutoConfigTarget(gatewayID)
	if err != nil {
		summary := "暂时无法读取飞书自动配置状态，请稍后重试。"
		switch {
		case strings.HasPrefix(err.Error(), "feishu_app_runtime_unavailable:"):
			summary = "当前机器人还在同步运行设置，请稍后再检查自动配置。"
		case strings.HasPrefix(err.Error(), "feishu_app_not_found:"):
			summary = "当前飞书应用暂时不可用，请重新连接后再继续。"
		}
		return onboardingWorkflowAutoConfigView{
			onboardingWorkflowStageView: stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusPending, summary, false, false, []string{"retry"}),
			Decision:                    decision,
			Error:                       err.Error(),
		}
	}

	planCtx, cancel := context.WithTimeout(context.Background(), defaultFeishuAutoConfigPlanTimeout)
	defer cancel()
	plan, err := planFeishuAppAutoConfig(planCtx, runtimeCfg)
	if err != nil {
		return onboardingWorkflowAutoConfigView{
			onboardingWorkflowStageView: stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusPending, "暂时无法读取飞书自动配置状态，请稍后重试。", false, false, []string{"retry"}),
			Decision:                    decision,
			Error:                       err.Error(),
		}
	}

	view := onboardingWorkflowAutoConfigView{
		Decision: decision,
		Plan:     &plan,
	}
	switch plan.Status {
	case feishu.AutoConfigStatusClean:
		view.onboardingWorkflowStageView = stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusComplete, firstNonEmpty(strings.TrimSpace(plan.Summary), "当前飞书应用配置已收敛。"), false, false, []string{"retry"})
	case feishu.AutoConfigStatusDegraded:
		view.onboardingWorkflowStageView = stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusComplete, firstNonEmpty(strings.TrimSpace(plan.Summary), "飞书应用已可用，但仍有可降级缺失项。"), false, false, []string{"retry"})
	case feishu.AutoConfigStatusApplyRequired:
		if onboardingAutoConfigCanDefer(plan) && onboardingAutoConfigDeferred(state.AutoConfigDecision) {
			view.onboardingWorkflowStageView = stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusDeferred, "你已选择先按降级继续，后续仍可回到这里重新补齐。", false, true, []string{"apply", "retry"})
			break
		}
		actions := []string{"apply", "retry"}
		if onboardingAutoConfigCanDefer(plan) {
			actions = append(actions, "defer")
		}
		view.onboardingWorkflowStageView = stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusPending, firstNonEmpty(strings.TrimSpace(plan.Summary), "当前还需要自动补齐飞书配置。"), false, onboardingAutoConfigCanDefer(plan), actions)
	case feishu.AutoConfigStatusPublishRequired:
		if onboardingAutoConfigCanDefer(plan) && onboardingAutoConfigDeferred(state.AutoConfigDecision) {
			view.onboardingWorkflowStageView = stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusDeferred, "你已选择先按降级继续，后续仍可回到这里重新发布。", false, true, []string{"publish", "retry"})
			break
		}
		actions := []string{"publish", "retry"}
		if onboardingAutoConfigCanDefer(plan) {
			actions = append(actions, "defer")
		}
		view.onboardingWorkflowStageView = stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusPending, firstNonEmpty(strings.TrimSpace(plan.Summary), "自动补齐后的配置仍需提交飞书发布。"), false, onboardingAutoConfigCanDefer(plan), actions)
	case feishu.AutoConfigStatusAwaitingReview:
		if len(plan.BlockingRequirements) > 0 {
			view.onboardingWorkflowStageView = stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusBlocked, firstNonEmpty(strings.TrimSpace(plan.Summary), "飞书应用变更正在等待管理员处理，当前还不能继续。"), true, false, []string{"retry"})
			break
		}
		if onboardingAutoConfigDeferred(state.AutoConfigDecision) {
			view.onboardingWorkflowStageView = stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusDeferred, "你已选择先按降级继续，后续仍可回到这里查看审核结果。", false, true, []string{"retry"})
			break
		}
		view.onboardingWorkflowStageView = stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusPending, firstNonEmpty(strings.TrimSpace(plan.Summary), "当前变更正在等待管理员处理。若只影响可选能力，你也可以先按降级继续。"), false, true, []string{"defer", "retry"})
	case feishu.AutoConfigStatusBlocked, feishu.AutoConfigStatusUnsupported:
		view.onboardingWorkflowStageView = stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusBlocked, firstNonEmpty(strings.TrimSpace(plan.Summary), "当前飞书自动配置还不能继续。"), true, false, []string{"retry"})
	default:
		view.onboardingWorkflowStageView = stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusPending, firstNonEmpty(strings.TrimSpace(plan.Summary), "暂时无法读取飞书自动配置状态，请稍后重试。"), false, false, []string{"retry"})
	}
	return view
}

func buildBlockedOnboardingAutoConfigStage(summary string) onboardingWorkflowAutoConfigView {
	return onboardingWorkflowAutoConfigView{
		onboardingWorkflowStageView: stageView(onboardingStageAutoConfig, "飞书自动配置", onboardingStageStatusBlocked, summary, true, false, nil),
	}
}

func buildOnboardingMenuStage(app *adminFeishuAppSummary, autoConfig onboardingWorkflowAutoConfigView, state config.FeishuAppOnboardingState) onboardingWorkflowStageView {
	if app == nil {
		return buildBlockedOnboardingMenuStage("请先接入可用的飞书应用。")
	}
	if !onboardingStageResolved(autoConfig.Status) {
		return buildBlockedOnboardingMenuStage("请先完成飞书自动配置。")
	}
	if onboardingMenuConfirmed(state.MenuDecision) {
		return stageView(onboardingStageMenu, "菜单确认", onboardingStageStatusComplete, "你已确认机器人菜单配置完成。", false, false, []string{"open_bot"})
	}
	return stageView(onboardingStageMenu, "菜单确认", onboardingStageStatusPending, "请在飞书后台确认机器人菜单配置完成，然后回到这里继续。", false, false, []string{"open_bot", "confirm"})
}

func buildBlockedOnboardingMenuStage(summary string) onboardingWorkflowStageView {
	return stageView(onboardingStageMenu, "菜单确认", onboardingStageStatusBlocked, summary, true, false, nil)
}

func (a *App) buildOnboardingAutostartStage(cfg config.AppConfig) onboardingWorkflowMachineStepView {
	decision := onboardingDecisionViewFromConfig(cfg.Admin.Onboarding.AutostartDecision)
	status, err := detectAutostart(a.installStatePath())
	if err != nil {
		stage := stageView(onboardingStageAutostart, "自动启动", onboardingStageStatusPending, "暂时无法确认自动启动状态，你也可以先记录稍后处理。", false, true, []string{"defer"})
		return onboardingWorkflowMachineStepView{
			onboardingWorkflowStageView: stage,
			Decision:                    decision,
			Error:                       err.Error(),
		}
	}
	view := onboardingWorkflowMachineStepView{
		Autostart: &status,
		Decision:  decision,
	}
	switch {
	case !status.Supported:
		view.onboardingWorkflowStageView = stageView(onboardingStageAutostart, "自动启动", onboardingStageStatusNotApplicable, "当前系统不支持自动启动。", false, true, nil)
	case decision != nil && decision.Value == onboardingDecisionDeferred:
		view.onboardingWorkflowStageView = stageView(onboardingStageAutostart, "自动启动", onboardingStageStatusDeferred, "你选择稍后再处理自动启动。", false, true, []string{"apply", "record_enabled"})
	case decision != nil && decision.Value == onboardingDecisionAutostartEnabled && status.Enabled:
		view.onboardingWorkflowStageView = stageView(onboardingStageAutostart, "自动启动", onboardingStageStatusComplete, "自动启动已经启用，并且当前决策已记录。", false, true, []string{"apply", "defer"})
	case status.Enabled:
		view.onboardingWorkflowStageView = stageView(onboardingStageAutostart, "自动启动", onboardingStageStatusPending, "当前已经启用自动启动，但还没有记录这项机器决策。", false, true, []string{"record_enabled", "defer"})
	default:
		view.onboardingWorkflowStageView = stageView(onboardingStageAutostart, "自动启动", onboardingStageStatusPending, "当前还没有完成自动启动决策。", false, true, []string{"apply", "defer"})
	}
	return view
}

func (a *App) buildOnboardingVSCodeStage(cfg config.AppConfig) onboardingWorkflowMachineStepView {
	decision := onboardingDecisionViewFromConfig(cfg.Admin.Onboarding.VSCodeDecision)
	status, err := a.buildVSCodeDetectResponse()
	if err != nil {
		stage := stageView(onboardingStageVSCode, "VS Code 集成", onboardingStageStatusPending, "暂时无法确认 VS Code 集成状态，你也可以先记录稍后处理。", false, true, []string{"defer", "remote_only"})
		return onboardingWorkflowMachineStepView{
			onboardingWorkflowStageView: stage,
			Decision:                    decision,
			Error:                       err.Error(),
		}
	}
	ready := workflowVSCodeReady(status)
	view := onboardingWorkflowMachineStepView{
		VSCode:   &status,
		Decision: decision,
	}
	switch {
	case decision != nil && decision.Value == onboardingDecisionDeferred:
		view.onboardingWorkflowStageView = stageView(onboardingStageVSCode, "VS Code 集成", onboardingStageStatusDeferred, "你选择稍后再处理 VS Code 集成。", false, true, []string{"apply", "record_managed_shim", "remote_only"})
	case decision != nil && decision.Value == onboardingDecisionVSCodeRemoteOnly:
		view.onboardingWorkflowStageView = stageView(onboardingStageVSCode, "VS Code 集成", onboardingStageStatusDeferred, "你选择留到目标 SSH 机器上处理 VS Code 集成。", false, true, []string{"apply", "record_managed_shim", "defer"})
	case decision != nil && decision.Value == onboardingDecisionVSCodeManaged && ready:
		view.onboardingWorkflowStageView = stageView(onboardingStageVSCode, "VS Code 集成", onboardingStageStatusComplete, "VS Code 集成已经完成，并且当前决策已记录。", false, true, []string{"apply", "defer", "remote_only"})
	case ready:
		view.onboardingWorkflowStageView = stageView(onboardingStageVSCode, "VS Code 集成", onboardingStageStatusPending, "当前已经检测到 VS Code 集成，但还没有记录你的处理决策。", false, true, []string{"record_managed_shim", "defer", "remote_only"})
	default:
		view.onboardingWorkflowStageView = stageView(onboardingStageVSCode, "VS Code 集成", onboardingStageStatusPending, "当前还没有完成 VS Code 集成决策。", false, true, []string{"apply", "defer", "remote_only"})
	}
	return view
}

func workflowVSCodeReady(status vscodeDetectResponse) bool {
	return status.LatestShim.MatchesBinary && !status.NeedsShimReinstall && !status.Settings.MatchesBinary
}

func buildOnboardingGuide(
	currentStage string,
	connection onboardingWorkflowStageView,
	autoConfig onboardingWorkflowAutoConfigView,
	menu onboardingWorkflowStageView,
	autostart onboardingWorkflowMachineStepView,
	vscode onboardingWorkflowMachineStepView,
	canComplete bool,
) onboardingWorkflowGuideView {
	remaining := make([]string, 0, 5)
	appendRemaining := func(status string, text string) {
		if status == onboardingStageStatusPending || status == onboardingStageStatusBlocked {
			remaining = append(remaining, text)
		}
	}
	appendRemaining(connection.Status, "接入并验证一个可用的飞书应用。")
	appendRemaining(autoConfig.Status, "完成飞书自动配置，确认缺失项与后果。")
	appendRemaining(menu.Status, "在飞书后台确认机器人菜单配置。")
	appendRemaining(autostart.Status, "决定是否在这台机器上启用自动启动。")
	appendRemaining(vscode.Status, "决定如何处理这台机器上的 VS Code 集成。")
	summary := ""
	switch {
	case canComplete && len(remaining) > 0:
		summary = "当前 setup 已经可以完成，但仍有建议补齐项。"
	case canComplete:
		summary = "当前机器的 onboarding 已经收口完成。"
	case !onboardingStageResolved(autoConfig.Status):
		summary = "当前飞书应用已经接入，下面请先完成飞书自动配置。"
	case !onboardingStageResolved(menu.Status):
		summary = "飞书自动配置已经收口，下面请完成菜单确认。"
	case onboardingStageResolved(menu.Status):
		summary = "当前基础接入已经完成，下面请继续处理这台机器上的可选设置。"
	case connection.Status == onboardingStageStatusComplete:
		summary = "当前飞书应用已经接入，下面请先完成飞书自动配置。"
	default:
		summary = "请先让这台机器和一个可用飞书应用进入可继续联调的状态。"
	}
	return onboardingWorkflowGuideView{
		AutoConfiguredSummary:  summary,
		RemainingManualActions: remaining,
		RecommendedNextStep:    currentStage,
	}
}

func buildOnboardingCompletion(
	canComplete bool,
	runtimeReady bool,
	connection onboardingWorkflowStageView,
	autoConfig onboardingWorkflowAutoConfigView,
	menu onboardingWorkflowStageView,
	autostart onboardingWorkflowMachineStepView,
	vscode onboardingWorkflowMachineStepView,
) onboardingWorkflowCompletionView {
	if canComplete {
		return onboardingWorkflowCompletionView{
			SetupRequired: false,
			CanComplete:   true,
			Summary:       "当前 setup 已可完成。",
		}
	}
	blockingReason := blockingReasonForCompletion(runtimeReady, connection, autoConfig, menu, autostart, vscode)
	return onboardingWorkflowCompletionView{
		SetupRequired:  true,
		CanComplete:    false,
		Summary:        "当前 setup 还不能完成，请先处理阻塞项。",
		BlockingReason: blockingReason,
	}
}

func blockingReasonForCompletion(
	runtimeReady bool,
	connection onboardingWorkflowStageView,
	autoConfig onboardingWorkflowAutoConfigView,
	menu onboardingWorkflowStageView,
	autostart onboardingWorkflowMachineStepView,
	vscode onboardingWorkflowMachineStepView,
) string {
	switch {
	case !runtimeReady:
		return "基础运行环境还没有通过。"
	case connection.Status != onboardingStageStatusComplete:
		return "还没有完成飞书连接验证。"
	case !onboardingStageResolved(autoConfig.Status):
		if strings.TrimSpace(autoConfig.Summary) != "" {
			return autoConfig.Summary
		}
		return "飞书自动配置还没有完成。"
	case !onboardingStageResolved(menu.Status):
		return "还没有确认机器人菜单配置。"
	case !machineDecisionSatisfied(autostart.Status):
		return "还没有完成自动启动决策。"
	case !machineDecisionSatisfied(vscode.Status):
		return "还没有完成 VS Code 集成决策。"
	default:
		return ""
	}
}

func machineDecisionSatisfied(status string) bool {
	switch status {
	case onboardingStageStatusComplete, onboardingStageStatusDeferred, onboardingStageStatusNotApplicable:
		return true
	default:
		return false
	}
}

func onboardingStageResolved(status string) bool {
	switch status {
	case onboardingStageStatusComplete, onboardingStageStatusDeferred, onboardingStageStatusNotApplicable:
		return true
	default:
		return false
	}
}

func firstPendingStageID(stages []onboardingWorkflowStageView) string {
	for _, stage := range stages {
		if stage.Status == onboardingStageStatusBlocked || stage.Status == onboardingStageStatusPending {
			return stage.ID
		}
	}
	return ""
}

func runtimeRequirementsSummaryStatus(ready bool) string {
	if ready {
		return onboardingStageStatusComplete
	}
	return onboardingStageStatusBlocked
}

func stageView(id, title, status, summary string, blocking bool, optional bool, allowedActions []string) onboardingWorkflowStageView {
	return onboardingWorkflowStageView{
		ID:             id,
		Title:          title,
		Status:         status,
		Summary:        summary,
		Blocking:       blocking,
		Optional:       optional,
		AllowedActions: append([]string(nil), allowedActions...),
	}
}

func onboardingDecisionViewFromConfig(decision *config.OnboardingDecision) *onboardingWorkflowDecisionView {
	if decision == nil || strings.TrimSpace(decision.Value) == "" {
		return nil
	}
	return &onboardingWorkflowDecisionView{
		Value:     strings.TrimSpace(decision.Value),
		DecidedAt: decision.DecidedAt,
	}
}

func onboardingPermissionSkipped(decision *config.OnboardingDecision) bool {
	return decision != nil && strings.TrimSpace(decision.Value) == onboardingDecisionPermissionSkipped
}

func onboardingAutoConfigDeferred(decision *config.OnboardingDecision) bool {
	return decision != nil && strings.TrimSpace(decision.Value) == onboardingDecisionDeferred
}

func onboardingAutoConfigCanDefer(plan feishu.AutoConfigPlan) bool {
	return len(plan.BlockingRequirements) == 0 && (plan.Status == feishu.AutoConfigStatusApplyRequired || plan.Status == feishu.AutoConfigStatusPublishRequired || plan.Status == feishu.AutoConfigStatusAwaitingReview)
}

func onboardingMenuConfirmed(decision *config.OnboardingDecision) bool {
	return decision != nil && strings.TrimSpace(decision.Value) == onboardingDecisionMenuConfirmed
}

func onboardingAppState(cfg config.AppConfig, gatewayID string) config.FeishuAppOnboardingState {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" || cfg.Admin.Onboarding.Apps == nil {
		return config.FeishuAppOnboardingState{}
	}
	return cfg.Admin.Onboarding.Apps[gatewayID]
}
