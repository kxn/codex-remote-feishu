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
)

func stubSetupAutoConfigPlanner(t *testing.T, planner func(context.Context, feishu.LiveGatewayConfig) (feishu.AutoConfigPlan, error)) {
	t.Helper()
	oldPlan := planFeishuAppAutoConfig
	planFeishuAppAutoConfig = planner
	t.Cleanup(func() {
		planFeishuAppAutoConfig = oldPlan
	})
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
	t.Setenv("HOME", home)

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
			name: "blocking requirements still block",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.BlockingRequirements = []feishu.AutoConfigRequirementStatus{
					{Kind: feishu.AutoConfigRequirementKindScope, Key: "im:message:send_as_bot", Required: true},
				}
			},
			want: false,
		},
		{
			name: "publish required cannot continue degraded",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.Status = feishu.AutoConfigStatusPublishRequired
			},
			want: false,
		},
		{
			name: "awaiting review cannot continue degraded",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.Status = feishu.AutoConfigStatusAwaitingReview
			},
			want: false,
		},
		{
			name: "ability mismatch blocks degraded continue",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.Diff.AbilityPatchRequired = true
			},
			want: false,
		},
		{
			name: "event subscription type mismatch blocks degraded continue",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.Diff.EventSubscriptionTypeMismatch = true
			},
			want: false,
		},
		{
			name: "event request url mismatch blocks degraded continue",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.Diff.EventRequestURLMismatch = true
			},
			want: false,
		},
		{
			name: "callback type mismatch blocks degraded continue",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.Diff.CallbackTypeMismatch = true
			},
			want: false,
		},
		{
			name: "callback request url mismatch blocks degraded continue",
			mutate: func(plan *feishu.AutoConfigPlan) {
				plan.Diff.CallbackRequestURLMismatch = true
			},
			want: false,
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

func TestSetupOnboardingWorkflowDoesNotHonorDeferredAutoConfigForBlockingStates(t *testing.T) {
	cases := []struct {
		name           string
		plan           feishu.AutoConfigPlan
		wantAutoStatus string
	}{
		{
			name: "required ability patch remains pending",
			plan: feishu.AutoConfigPlan{
				Status:  feishu.AutoConfigStatusApplyRequired,
				Summary: "当前机器人能力还没有生效。",
				Diff: feishu.AutoConfigDiff{
					AbilityPatchRequired: true,
				},
			},
			wantAutoStatus: onboardingStageStatusPending,
		},
		{
			name: "publish required remains pending",
			plan: feishu.AutoConfigPlan{
				Status:  feishu.AutoConfigStatusPublishRequired,
				Summary: "自动补齐后的配置仍需提交飞书发布。",
			},
			wantAutoStatus: onboardingStageStatusPending,
		},
		{
			name: "awaiting review remains blocked",
			plan: feishu.AutoConfigPlan{
				Status:  feishu.AutoConfigStatusAwaitingReview,
				Summary: "飞书应用变更已进入审核流程，正在等待审核结果。",
			},
			wantAutoStatus: onboardingStageStatusBlocked,
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
			if workflow.App.Menu.Status != onboardingStageStatusBlocked {
				t.Fatalf("menu status = %q, want blocked", workflow.App.Menu.Status)
			}
			if workflow.CurrentStage != onboardingStageAutoConfig {
				t.Fatalf("current stage = %q, want auto_config", workflow.CurrentStage)
			}
			if workflow.Completion.CanComplete {
				t.Fatal("completion unexpectedly allowed")
			}
			if containsString(workflow.App.AutoConfig.AllowedActions, "defer") {
				t.Fatalf("allowed actions = %#v, defer should not be exposed", workflow.App.AutoConfig.AllowedActions)
			}
		})
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
		t.Fatalf("current stage = %q, want autostart", workflow.CurrentStage)
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
	if containsString(workflow.Autostart.AllowedActions, "apply") {
		t.Fatalf("autostart allowed actions = %#v, apply should not be exposed when canApply=false", workflow.Autostart.AllowedActions)
	}
	if !containsString(workflow.Autostart.AllowedActions, "defer") {
		t.Fatalf("autostart allowed actions = %#v, want defer fallback", workflow.Autostart.AllowedActions)
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
		t.Fatalf("current stage = %q, want autostart", workflow.CurrentStage)
	}
}
