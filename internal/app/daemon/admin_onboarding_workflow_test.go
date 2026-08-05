package daemon

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func stubSetupAutoConfigPlanner(t *testing.T, planner func(context.Context, feishu.LiveGatewayConfig) (feishu.AutoConfigPlan, error)) {
	t.Helper()
	workflowSetupFacade(t).plan = planner
}

func stubSetupLongConnectionStatus(t *testing.T, status feishu.LongConnectionStatus, err error) {
	t.Helper()
	workflowSetupFacade(t).status = func(context.Context, feishu.LiveGatewayConfig) (feishu.LongConnectionStatus, error) {
		return status, err
	}
}

func workflowSetupFacade(t *testing.T) *setupFacadeFunc {
	t.Helper()
	if current, ok := feishuSetupFacade.(*setupFacadeFunc); ok {
		return current
	}
	facade := &setupFacadeFunc{}
	stubFeishuSetupFacade(t, facade)
	return facade
}

func stubSetupAutostartStatus(t *testing.T) {
	t.Helper()
	oldDetectAutostart := detectAutostart
	detectAutostart = func(statePath string) (install.AutostartStatus, error) {
		return install.AutostartStatus{
			Platform:         "linux",
			Supported:        true,
			Manager:          install.ServiceManagerSystemdUser,
			CurrentManager:   install.ServiceManagerDetached,
			Status:           "disabled",
			InstallStatePath: statePath,
			CanApply:         true,
		}, nil
	}
	t.Cleanup(func() {
		detectAutostart = oldDetectAutostart
	})
}

func newVerifiedSetupWorkflowApp(t *testing.T) *App {
	t.Helper()

	home := t.TempDir()
	setTestHome(t, home)

	app, token := newRemoteSetupTestApp(t, home)
	cookie := exchangeSetupSessionCookie(t, app, token)

	req := performSetupRequestWithCookie(http.MethodPost, "/api/setup/feishu/apps", `{"id":"main","name":"Main Bot","appId":"cli_xxx","appSecret":"secret_xxx"}`, cookie)
	rec := performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}

	req = performSetupRequestWithCookie(http.MethodPost, "/api/setup/feishu/apps/main/verify", "", cookie)
	rec = performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	return app
}

func TestOnboardingAutoConfigCanContinueDegraded(t *testing.T) {
	base := feishu.AutoConfigPlan{
		Status: feishu.AutoConfigStatusApplyRequired,
	}

	cases := []struct {
		name   string
		mutate func(*feishu.AutoConfigPlan)
		want   bool
	}{
		{
			name: "optional apply-required changes can continue degraded",
			want: true,
		},
		{
			name: "blocking requirements can still continue with explicit degraded choice",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.BlockingRequirements = []feishu.AutoConfigRequirementStatus{
					{Kind: feishu.AutoConfigRequirementKindScope, Key: "im:message:send_as_bot", Required: true},
				}
			},
			want: true,
		},
		{
			name: "awaiting review can continue degraded when no blocking diff remains",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.Status = feishu.AutoConfigStatusAwaitingReview
			},
			want: true,
		},
		{
			name: "verification failure can continue degraded when no blocking diff remains",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.Status = feishu.AutoConfigStatusVerificationFailed
			},
			want: true,
		},
		{
			name: "ability mismatch can continue with explicit degraded choice",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.Diff.AbilityPatchRequired = true
			},
			want: true,
		},
		{
			name: "callback type mismatch can continue with explicit degraded choice",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.Diff.CallbackTypeMismatch = true
			},
			want: true,
		},
		{
			name: "callback request url mismatch can continue with explicit degraded choice",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.Diff.CallbackRequestURLMismatch = true
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := base
			if tc.mutate != nil {
				tc.mutate(&plan)
			}
			if got := onboardingAutoConfigCanContinueDegraded(plan); got != tc.want {
				t.Fatalf("onboardingAutoConfigCanContinueDegraded() = %t, want %t for plan %#v", got, tc.want, plan)
			}
		})
	}
}

func TestOnboardingAutostartStageUsesDetectedStateWithoutMachineDecision(t *testing.T) {
	oldDetectAutostart := detectAutostart
	detectAutostart = func(statePath string) (install.AutostartStatus, error) {
		return install.AutostartStatus{
			Platform:  "linux",
			Supported: true,
			Enabled:   false,
			CanApply:  true,
		}, nil
	}
	t.Cleanup(func() { detectAutostart = oldDetectAutostart })

	app, _ := newRemoteSetupTestApp(t, t.TempDir())
	decidedAt := time.Now().UTC()
	cfg := config.DefaultAppConfig()
	cfg.Admin.Onboarding.AutostartDecision = &config.OnboardingDecision{
		Value:     onboardingDecisionAutostartEnabled,
		DecidedAt: &decidedAt,
	}

	stage := app.buildOnboardingAutostartStage(cfg)
	if stage.Status != onboardingStageStatusComplete {
		t.Fatalf("status = %q, want %q", stage.Status, onboardingStageStatusComplete)
	}
	if stage.Summary != "自动启动未启用。" {
		t.Fatalf("summary = %q, want objective disabled summary", stage.Summary)
	}
	if !xutil.ContainsString(stage.AllowedActions, "apply") {
		t.Fatalf("allowed actions = %#v, want apply", stage.AllowedActions)
	}
}

func TestSetupOnboardingWorkflowDeferredAutoConfigHonorsFinalBlockingState(t *testing.T) {
	cases := []struct {
		name           string
		plan           feishu.AutoConfigPlan
		wantAutoStatus string
		wantMenuStatus string
		wantStage      string
		wantDefer      bool
	}{
		{
			name: "required ability patch honors deferred",
			plan: feishu.AutoConfigPlan{
				Status:  feishu.AutoConfigStatusApplyRequired,
				Summary: "当前机器人能力还没有生效。",
				Diff: feishu.AutoConfigDiff{
					AbilityPatchRequired: true,
				},
			},
			wantAutoStatus: onboardingStageStatusDeferred,
			wantMenuStatus: onboardingStageStatusPending,
			wantStage:      onboardingStageMenu,
		},
		{
			name: "awaiting review honors deferred when no blocking diff remains",
			plan: feishu.AutoConfigPlan{
				Status:  feishu.AutoConfigStatusAwaitingReview,
				Summary: "飞书应用变更已进入审核流程，正在等待审核结果。",
			},
			wantAutoStatus: onboardingStageStatusDeferred,
			wantMenuStatus: onboardingStageStatusPending,
			wantStage:      onboardingStageMenu,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newVerifiedSetupWorkflowApp(t)
			stubSetupAutoConfigPlanner(t, func(context.Context, feishu.LiveGatewayConfig) (feishu.AutoConfigPlan, error) {
				return tc.plan, nil
			})

			if err := app.writeFeishuAppAutoConfigDecision("main", onboardingDecisionDeferred, time.Now().UTC()); err != nil {
				t.Fatalf("writeFeishuAppAutoConfigDecision: %v", err)
			}

			workflow, err := app.buildOnboardingWorkflow("main")
			if err != nil {
				t.Fatalf("buildOnboardingWorkflow: %v", err)
			}
			if workflow.App == nil {
				t.Fatal("workflow app is nil")
			}
			if workflow.App.AutoConfig.Status != tc.wantAutoStatus {
				t.Fatalf("auto-config status = %q, want %q", workflow.App.AutoConfig.Status, tc.wantAutoStatus)
			}
			if workflow.App.Menu.Status != tc.wantMenuStatus {
				t.Fatalf("menu status = %q, want %q", workflow.App.Menu.Status, tc.wantMenuStatus)
			}
			if workflow.CurrentStage != tc.wantStage {
				t.Fatalf("current stage = %q, want %q", workflow.CurrentStage, tc.wantStage)
			}
			if workflow.Completion.CanComplete {
				t.Fatal("completion unexpectedly allowed")
			}
			if got := xutil.ContainsString(workflow.App.AutoConfig.AllowedActions, "defer"); got != tc.wantDefer {
				t.Fatalf("allowed actions = %#v, defer presence = %t, want %t", workflow.App.AutoConfig.AllowedActions, got, tc.wantDefer)
			}
		})
	}
}

func TestSetupOnboardingWorkflowIncludesLongConnectionStatus(t *testing.T) {
	app := newVerifiedSetupWorkflowApp(t)
	stubSetupAutoConfigPlanner(t, func(context.Context, feishu.LiveGatewayConfig) (feishu.AutoConfigPlan, error) {
		return feishu.AutoConfigPlan{
			Status:  feishu.AutoConfigStatusClean,
			Summary: "飞书应用配置已收敛。",
		}, nil
	})
	stubSetupLongConnectionStatus(t, feishu.LongConnectionStatus{
		OnlineInstanceCount: 0,
		CheckedAt:           time.Now().UTC(),
	}, nil)

	workflow, err := app.buildOnboardingWorkflow("main")
	if err != nil {
		t.Fatalf("buildOnboardingWorkflow: %v", err)
	}
	if workflow.App == nil {
		t.Fatal("workflow app is nil")
	}
	if workflow.App.AutoConfig.LongConnection == nil {
		t.Fatalf("expected long connection status in auto-config view")
	}
	if workflow.App.AutoConfig.LongConnection.OnlineInstanceCount != 0 {
		t.Fatalf("unexpected long connection status: %#v", workflow.App.AutoConfig.LongConnection)
	}
	if workflow.App.AutoConfig.Status != onboardingStageStatusComplete {
		t.Fatalf("auto-config status = %q, want complete", workflow.App.AutoConfig.Status)
	}
	if !strings.Contains(workflow.App.AutoConfig.Summary, "暂未确认本机长连接在线") {
		t.Fatalf("expected concise long-connection hint, got %q", workflow.App.AutoConfig.Summary)
	}
}

func TestSetupOnboardingWorkflowKeepsDeferredAutoConfigOnPlanError(t *testing.T) {
	stubSetupAutostartStatus(t)
	app := newVerifiedSetupWorkflowApp(t)
	stubSetupAutoConfigPlanner(t, func(context.Context, feishu.LiveGatewayConfig) (feishu.AutoConfigPlan, error) {
		return feishu.AutoConfigPlan{}, errors.New("temporary planner failure")
	})

	if err := app.writeFeishuAppAutoConfigDecision("main", onboardingDecisionDeferred, time.Now().UTC()); err != nil {
		t.Fatalf("writeFeishuAppAutoConfigDecision: %v", err)
	}
	if err := app.writeFeishuAppMenuDecision("main", onboardingDecisionMenuConfirmed, time.Now().UTC()); err != nil {
		t.Fatalf("writeFeishuAppMenuDecision: %v", err)
	}

	workflow, err := app.buildOnboardingWorkflow("main")
	if err != nil {
		t.Fatalf("buildOnboardingWorkflow: %v", err)
	}
	if workflow.App == nil {
		t.Fatal("workflow app is nil")
	}
	if workflow.App.AutoConfig.Status != onboardingStageStatusDeferred {
		t.Fatalf("auto-config status = %q, want deferred", workflow.App.AutoConfig.Status)
	}
	if !strings.Contains(workflow.App.AutoConfig.Summary, "保留你已选择的降级继续") {
		t.Fatalf("auto-config summary = %q, want deferred retry summary", workflow.App.AutoConfig.Summary)
	}
	if workflow.App.Menu.Status != onboardingStageStatusComplete {
		t.Fatalf("menu status = %q, want complete", workflow.App.Menu.Status)
	}
	if workflow.CurrentStage != onboardingStageAutostart {
		t.Fatalf("current stage = %q, want autostart before machine integration is reviewed", workflow.CurrentStage)
	}
}

func TestSetupOnboardingAutostartDoesNotOfferApplyWhenCannotApply(t *testing.T) {
	oldDetectAutostart := detectAutostart
	detectAutostart = func(statePath string) (install.AutostartStatus, error) {
		return install.AutostartStatus{
			Platform:         "darwin",
			Supported:        true,
			Manager:          install.ServiceManagerLaunchdUser,
			CurrentManager:   install.ServiceManagerDetached,
			Status:           "disabled",
			InstallStatePath: statePath,
			CanApply:         false,
			Warning:          "自动启动状态暂时不可写。",
			LingerHint:       "请稍后在管理页重试。",
		}, nil
	}
	t.Cleanup(func() {
		detectAutostart = oldDetectAutostart
	})

	app := newVerifiedSetupWorkflowApp(t)
	if err := app.writeFeishuAppAutoConfigDecision("main", onboardingDecisionDeferred, time.Now().UTC()); err != nil {
		t.Fatalf("writeFeishuAppAutoConfigDecision: %v", err)
	}
	if err := app.writeFeishuAppMenuDecision("main", onboardingDecisionMenuConfirmed, time.Now().UTC()); err != nil {
		t.Fatalf("writeFeishuAppMenuDecision: %v", err)
	}

	workflow, err := app.buildOnboardingWorkflow("main")
	if err != nil {
		t.Fatalf("buildOnboardingWorkflow: %v", err)
	}
	if xutil.ContainsString(workflow.Autostart.AllowedActions, "apply") {
		t.Fatalf("autostart allowed actions = %#v, apply should not be exposed when canApply=false", workflow.Autostart.AllowedActions)
	}
	if xutil.ContainsString(workflow.Autostart.AllowedActions, "defer") {
		t.Fatalf("autostart allowed actions = %#v, per-item defer should not be exposed", workflow.Autostart.AllowedActions)
	}
	if workflow.Autostart.Autostart == nil || workflow.Autostart.Autostart.Warning == "" || workflow.Autostart.Autostart.LingerHint == "" {
		t.Fatalf("autostart payload did not preserve warning/hint: %#v", workflow.Autostart.Autostart)
	}
}

func TestSetupOnboardingWorkflowKeepsDeferredAutoConfigOnLoadError(t *testing.T) {
	stubSetupAutostartStatus(t)
	app := newVerifiedSetupWorkflowApp(t)

	if err := app.writeFeishuAppAutoConfigDecision("main", onboardingDecisionDeferred, time.Now().UTC()); err != nil {
		t.Fatalf("writeFeishuAppAutoConfigDecision: %v", err)
	}
	if err := app.writeFeishuAppMenuDecision("main", onboardingDecisionMenuConfirmed, time.Now().UTC()); err != nil {
		t.Fatalf("writeFeishuAppMenuDecision: %v", err)
	}

	loaded, err := app.loadAdminConfig()
	if err != nil {
		t.Fatalf("loadAdminConfig: %v", err)
	}
	loadCalls := 0
	app.admin.loadConfig = func() (config.LoadedAppConfig, error) {
		loadCalls++
		if loadCalls == 3 {
			return config.LoadedAppConfig{}, errors.New("temporary config read failure")
		}
		return loaded, nil
	}

	workflow, err := app.buildOnboardingWorkflow("main")
	if err != nil {
		t.Fatalf("buildOnboardingWorkflow: %v", err)
	}
	if workflow.App == nil {
		t.Fatal("workflow app is nil")
	}
	if workflow.App.AutoConfig.Status != onboardingStageStatusDeferred {
		t.Fatalf("auto-config status = %q, want deferred", workflow.App.AutoConfig.Status)
	}
	if !strings.Contains(workflow.App.AutoConfig.Summary, "保留你已选择的降级继续") {
		t.Fatalf("auto-config summary = %q, want deferred retry summary", workflow.App.AutoConfig.Summary)
	}
	if workflow.App.Menu.Status != onboardingStageStatusComplete {
		t.Fatalf("menu status = %q, want complete", workflow.App.Menu.Status)
	}
	if workflow.CurrentStage != onboardingStageAutostart {
		t.Fatalf("current stage = %q, want autostart before machine integration is reviewed", workflow.CurrentStage)
	}
}

func TestSetupOnboardingWorkflowMachineIntegrationReviewedAdvancesToDone(t *testing.T) {
	stubSetupAutostartStatus(t)
	app := newVerifiedSetupWorkflowApp(t)
	stubSetupAutoConfigPlanner(t, func(context.Context, feishu.LiveGatewayConfig) (feishu.AutoConfigPlan, error) {
		return feishu.AutoConfigPlan{
			Status:  feishu.AutoConfigStatusClean,
			Summary: "飞书应用配置已收敛。",
		}, nil
	})

	if err := app.writeFeishuAppMenuDecision("main", onboardingDecisionMenuConfirmed, time.Now().UTC()); err != nil {
		t.Fatalf("writeFeishuAppMenuDecision: %v", err)
	}
	if err := app.writeOnboardingMachineIntegrationReviewed(); err != nil {
		t.Fatalf("writeOnboardingMachineIntegrationReviewed: %v", err)
	}

	workflow, err := app.buildOnboardingWorkflow("main")
	if err != nil {
		t.Fatalf("buildOnboardingWorkflow: %v", err)
	}
	if workflow.CurrentStage != onboardingStageDone {
		t.Fatalf("current stage = %q, want done after machine integration is reviewed", workflow.CurrentStage)
	}
}
