package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

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

func TestLegacyFeishuRegistrationRunnerUsesHistoricalFlow(t *testing.T) {
	var mu sync.Mutex
	var requests []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
			return
		}
		mu.Lock()
		requests = append(requests, r.Form)
		mu.Unlock()
		switch r.Form.Get("action") {
		case "init":
			_, _ = w.Write([]byte(`{"supported_auth_methods":["client_secret"]}`))
		case "begin":
			_, _ = w.Write([]byte(`{"device_code":"device-1","verification_uri_complete":"https://accounts.feishu.cn/qr?code=a%2Bb","interval":0,"expire_in":60}`))
		case "poll":
			_, _ = w.Write([]byte(`{"client_id":"cli_1","client_secret":"secret_1","user_info":{"open_id":"ou_1"}}`))
		default:
			http.Error(w, "unexpected action", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	qrCh := make(chan feishuRegistrationQRCode, 1)
	resultCh := make(chan feishuRegistrationResult, 1)
	runner := &legacyFeishuRegistrationRunner{
		httpClient:      server.Client(),
		registrationURL: server.URL,
		waitFn:          func(context.Context, time.Duration) error { return nil },
	}
	runner.Start(context.Background(), feishuRegistrationOptions{}, feishuRegistrationCallbacks{
		OnQRCode:   func(info feishuRegistrationQRCode) { qrCh <- info },
		OnComplete: func(result feishuRegistrationResult) { resultCh <- result },
	})

	select {
	case qr := <-qrCh:
		if qr.URL != "https://accounts.feishu.cn/qr?code=a%2Bb" {
			t.Fatalf("QR URL = %q, want original verification_uri_complete", qr.URL)
		}
		if qr.Interval != 5*time.Second {
			t.Fatalf("default interval = %s, want 5s", qr.Interval)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for QR callback")
	}

	select {
	case result := <-resultCh:
		if result.AppID != "cli_1" || result.AppSecret != "secret_1" || result.InstallerID != "ou_1" {
			t.Fatalf("registration result = %#v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for registration result")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("request count = %d, want init, begin, poll", len(requests))
	}
	if got := requests[0].Get("action"); got != "init" {
		t.Fatalf("first action = %q, want init", got)
	}
	if got := requests[1].Get("action"); got != "begin" {
		t.Fatalf("second action = %q, want begin", got)
	}
	for key, want := range map[string]string{
		"archetype":         "PersonalAgent",
		"auth_method":       "client_secret",
		"request_user_info": "open_id",
	} {
		if got := requests[1].Get(key); got != want {
			t.Fatalf("begin %s = %q, want %q", key, got, want)
		}
	}
	if got := requests[2].Get("action"); got != "poll" {
		t.Fatalf("third action = %q, want poll", got)
	}
	if got := requests[2].Get("device_code"); got != "device-1" {
		t.Fatalf("poll device_code = %q, want device-1", got)
	}
}

func TestLegacyFeishuRegistrationRunnerRetriesTransientPollErrors(t *testing.T) {
	var mu sync.Mutex
	pollCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
			return
		}
		switch r.Form.Get("action") {
		case "init":
			_, _ = w.Write([]byte(`{}`))
		case "begin":
			_, _ = w.Write([]byte(`{"device_code":"device-2","verification_uri_complete":"https://example.test/qr","interval":1,"expire_in":60}`))
		case "poll":
			mu.Lock()
			pollCount++
			count := pollCount
			mu.Unlock()
			if count == 1 {
				http.Error(w, "temporary outage", http.StatusBadGateway)
				return
			}
			if count == 2 {
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			_, _ = w.Write([]byte(`{"client_id":"cli_2","client_secret":"secret_2","open_id":"ou_2"}`))
		}
	}))
	defer server.Close()

	resultCh := make(chan feishuRegistrationResult, 1)
	failureCh := make(chan feishuRegistrationFailure, 1)
	runner := &legacyFeishuRegistrationRunner{
		httpClient:      server.Client(),
		registrationURL: server.URL,
		waitFn:          func(context.Context, time.Duration) error { return nil },
	}
	runner.Start(context.Background(), feishuRegistrationOptions{}, feishuRegistrationCallbacks{
		OnComplete: func(result feishuRegistrationResult) { resultCh <- result },
		OnFailure:  func(failure feishuRegistrationFailure) { failureCh <- failure },
	})

	select {
	case result := <-resultCh:
		if result.AppID != "cli_2" || result.InstallerID != "ou_2" {
			t.Fatalf("registration result = %#v", result)
		}
	case failure := <-failureCh:
		t.Fatalf("transient poll error became terminal failure: %#v", failure)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for retry result")
	}
}

func TestLegacyFeishuRegistrationRunnerMapsTerminalPollErrors(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		status    string
		errorCode string
	}{
		{name: "expired", response: `{"error":"expired_token"}`, status: feishuOnboardingStatusExpired, errorCode: "expired_token"},
		{name: "denied", response: `{"error":"access_denied"}`, status: feishuOnboardingStatusFailed, errorCode: "access_denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Errorf("parse form: %v", err)
					return
				}
				switch r.Form.Get("action") {
				case "init":
					_, _ = w.Write([]byte(`{}`))
				case "begin":
					_, _ = w.Write([]byte(`{"device_code":"device-terminal","verification_uri_complete":"https://example.test/qr","interval":1,"expire_in":60}`))
				case "poll":
					_, _ = w.Write([]byte(tt.response))
				}
			}))
			defer server.Close()

			failureCh := make(chan feishuRegistrationFailure, 1)
			runner := &legacyFeishuRegistrationRunner{
				httpClient:      server.Client(),
				registrationURL: server.URL,
				waitFn:          func(context.Context, time.Duration) error { return nil },
			}
			runner.Start(context.Background(), feishuRegistrationOptions{}, feishuRegistrationCallbacks{
				OnFailure: func(failure feishuRegistrationFailure) { failureCh <- failure },
			})

			select {
			case failure := <-failureCh:
				if failure.Status != tt.status || failure.ErrorCode != tt.errorCode {
					t.Fatalf("failure = %#v, want status=%q code=%q", failure, tt.status, tt.errorCode)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for terminal failure")
			}
		})
	}
}

func TestLegacyFeishuRegistrationRunnerCancellationStopsPolling(t *testing.T) {
	pollStarted := make(chan struct{})
	pollCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
			return
		}
		switch r.Form.Get("action") {
		case "init":
			_, _ = w.Write([]byte(`{}`))
		case "begin":
			_, _ = w.Write([]byte(`{"device_code":"device-cancel","verification_uri_complete":"https://example.test/qr","interval":1,"expire_in":60}`))
		case "poll":
			close(pollStarted)
			<-r.Context().Done()
			close(pollCanceled)
		}
	}))
	defer server.Close()

	resultCh := make(chan feishuRegistrationResult, 1)
	failureCh := make(chan feishuRegistrationFailure, 1)
	runner := &legacyFeishuRegistrationRunner{
		httpClient:      server.Client(),
		registrationURL: server.URL,
		waitFn:          func(context.Context, time.Duration) error { return nil },
	}
	run := runner.Start(context.Background(), feishuRegistrationOptions{}, feishuRegistrationCallbacks{
		OnComplete: func(result feishuRegistrationResult) { resultCh <- result },
		OnFailure:  func(failure feishuRegistrationFailure) { failureCh <- failure },
	})

	select {
	case <-pollStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for poll request")
	}
	run.Cancel()
	select {
	case <-pollCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("poll request was not canceled")
	}
	select {
	case result := <-resultCh:
		t.Fatalf("canceled registration completed: %#v", result)
	case failure := <-failureCh:
		t.Fatalf("canceled registration failed: %#v", failure)
	case <-time.After(100 * time.Millisecond):
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
