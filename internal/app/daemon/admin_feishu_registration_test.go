package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

func TestBuildFeishuRegistrationAddonsFromDefaultManifest(t *testing.T) {
	addons := buildFeishuRegistrationAddons(feishuapp.DefaultManifest())

	if addons.Preset == nil || *addons.Preset {
		t.Fatalf("expected preset=false, got %#v", addons.Preset)
	}
	assertStringContains(t, addons.Scopes.Tenant, "im:message:send_as_bot")
	assertStringContains(t, addons.Scopes.Tenant, "im:resource")
	assertStringContains(t, addons.Events.Items.Tenant, "im.message.receive_v1")
	assertStringContains(t, addons.Callbacks.Items, "card.action.trigger")
	assertSortedUniqueNonEmpty(t, addons.Scopes.Tenant)
	assertSortedUniqueNonEmpty(t, addons.Events.Items.Tenant)
	assertSortedUniqueNonEmpty(t, addons.Callbacks.Items)
	if len(addons.Scopes.User) != 0 {
		t.Fatalf("expected default user scopes to be omitted, got %#v", addons.Scopes.User)
	}
}

func TestFeishuOnboardingRegistrationRunnerUpdatesSession(t *testing.T) {
	cfg := configForRegistrationTest()
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	runner := &fakeFeishuRegistrationRunner{}
	app.feishuRuntime.registration = runner

	view, err := app.createFeishuOnboardingSession(context.Background())
	if err != nil {
		t.Fatalf("createFeishuOnboardingSession: %v", err)
	}
	if len(runner.runs) != 1 {
		t.Fatalf("expected one registration run, got %d", len(runner.runs))
	}
	run := runner.runs[0]
	if !run.Options.CreateOnly {
		t.Fatalf("expected createOnly registration option")
	}
	if run.Options.Addons == nil || run.Options.Addons.Preset == nil || *run.Options.Addons.Preset {
		t.Fatalf("expected preset=false addons, got %#v", run.Options.Addons)
	}
	if _, ok := run.Context.Deadline(); !ok {
		t.Fatalf("expected bounded registration context")
	}

	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	run.EmitQRCode(feishuRegistrationQRCode{
		URL:       "https://example.test/qr",
		ExpiresAt: expiresAt,
		Interval:  3 * time.Second,
	})

	afterQR, ok := app.snapshotFeishuOnboardingSession(view.ID)
	if !ok {
		t.Fatalf("session disappeared after QR callback")
	}
	if afterQR.QRCodeDataURL == "" || afterQR.VerificationURL != "https://example.test/qr" || afterQR.PollIntervalSeconds != 3 {
		t.Fatalf("unexpected QR session view: %#v", afterQR)
	}

	run.Complete(feishuRegistrationResult{
		AppID:       "cli_new",
		AppSecret:   "secret_new",
		InstallerID: "ou_installer",
	})

	ready, ok := app.snapshotFeishuOnboardingSession(view.ID)
	if !ok {
		t.Fatalf("session disappeared after completion")
	}
	if ready.Status != feishuOnboardingStatusReady || ready.AppID != "cli_new" {
		t.Fatalf("unexpected ready session view: %#v", ready)
	}
	if !run.Cancelled {
		t.Fatalf("expected registration run to be cancelled after completion")
	}
}

func TestFeishuOnboardingRegistrationPreQRRunIsBoundedAndCleanedUp(t *testing.T) {
	cfg := configForRegistrationTest()
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	runner := &fakeFeishuRegistrationRunner{}
	app.feishuRuntime.registration = runner

	view, err := app.createFeishuOnboardingSession(context.Background())
	if err != nil {
		t.Fatalf("createFeishuOnboardingSession: %v", err)
	}
	if len(runner.runs) != 1 {
		t.Fatalf("expected one registration run, got %d", len(runner.runs))
	}
	run := runner.runs[0]
	if _, ok := run.Context.Deadline(); !ok {
		t.Fatalf("expected bounded registration context")
	}
	if view.ExpiresAt.IsZero() {
		t.Fatalf("expected pre-QR session to have a cleanup deadline")
	}

	app.cleanupFeishuOnboardingSessions(view.ExpiresAt.Add(16 * time.Minute))
	if _, ok := app.snapshotFeishuOnboardingSession(view.ID); ok {
		t.Fatalf("expected pre-QR session to be removed after cleanup deadline")
	}
	if !run.Cancelled {
		t.Fatalf("expected pre-QR registration run to be cancelled during cleanup")
	}
}

func TestFeishuOnboardingCleanupCancelsExpiredRegistrationRun(t *testing.T) {
	cfg := configForRegistrationTest()
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	runner := &fakeFeishuRegistrationRunner{}
	app.feishuRuntime.registration = runner

	view, err := app.createFeishuOnboardingSession(context.Background())
	if err != nil {
		t.Fatalf("createFeishuOnboardingSession: %v", err)
	}
	if len(runner.runs) != 1 {
		t.Fatalf("expected one registration run, got %d", len(runner.runs))
	}
	run := runner.runs[0]

	expiresAt := time.Now().UTC().Add(-20 * time.Minute)
	run.EmitQRCode(feishuRegistrationQRCode{
		URL:       "https://example.test/expired",
		ExpiresAt: expiresAt,
		Interval:  3 * time.Second,
	})
	app.cleanupFeishuOnboardingSessions(time.Now().UTC())

	if _, ok := app.snapshotFeishuOnboardingSession(view.ID); ok {
		t.Fatalf("expected expired session to be removed")
	}
	if !run.Cancelled {
		t.Fatalf("expected registration run to be cancelled when session is cleaned up")
	}
}

type fakeFeishuRegistrationRunner struct {
	runs        []*fakeFeishuRegistrationRun
	autoQRCode  *feishuRegistrationQRCode
	autoResult  *feishuRegistrationResult
	autoFailure *feishuRegistrationFailure
}

func (f *fakeFeishuRegistrationRunner) Start(ctx context.Context, options feishuRegistrationOptions, callbacks feishuRegistrationCallbacks) feishuRegistrationRun {
	run := &fakeFeishuRegistrationRun{Context: ctx, Options: options, callbacks: callbacks}
	f.runs = append(f.runs, run)
	if f.autoQRCode != nil {
		callbacks.OnQRCode(*f.autoQRCode)
	}
	if f.autoResult != nil {
		callbacks.OnComplete(*f.autoResult)
	}
	if f.autoFailure != nil {
		callbacks.OnFailure(*f.autoFailure)
	}
	return run
}

type fakeFeishuRegistrationRun struct {
	Context   context.Context
	Options   feishuRegistrationOptions
	callbacks feishuRegistrationCallbacks
	Cancelled bool
}

func (f *fakeFeishuRegistrationRun) Cancel() {
	f.Cancelled = true
}

func (f *fakeFeishuRegistrationRun) EmitQRCode(info feishuRegistrationQRCode) {
	f.callbacks.OnQRCode(info)
}

func (f *fakeFeishuRegistrationRun) Complete(result feishuRegistrationResult) {
	f.callbacks.OnComplete(result)
}

func immediateRegistrationRunner(qrURL, appID, appSecret string) *fakeFeishuRegistrationRunner {
	return &fakeFeishuRegistrationRunner{
		autoQRCode: &feishuRegistrationQRCode{
			URL:       qrURL,
			ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
			Interval:  5 * time.Second,
		},
		autoResult: &feishuRegistrationResult{
			AppID:     appID,
			AppSecret: appSecret,
		},
	}
}

func configForRegistrationTest() config.AppConfig {
	return config.DefaultAppConfig()
}

func assertStringContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("expected %q in %#v", want, values)
}

func assertSortedUniqueNonEmpty(t *testing.T, values []string) {
	t.Helper()
	seen := map[string]bool{}
	for i, value := range values {
		if value == "" {
			t.Fatalf("value[%d] is empty in %#v", i, values)
		}
		if seen[value] {
			t.Fatalf("duplicate value %q in %#v", value, values)
		}
		seen[value] = true
		if i > 0 && values[i-1] > value {
			t.Fatalf("values are not sorted: %#v", values)
		}
	}
}
