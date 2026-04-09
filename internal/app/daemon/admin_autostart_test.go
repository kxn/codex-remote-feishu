package daemon

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
)

func TestAdminAutostartEndpoints(t *testing.T) {
	app, _, _ := newVSCodeAdminTestApp(t, t.TempDir(), seedBinaryForDaemonTest(t), false)

	originalDetect := detectAutostart
	originalApply := applyAutostart
	defer func() {
		detectAutostart = originalDetect
		applyAutostart = originalApply
	}()

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
	applyAutostart = func(opts install.AutostartApplyOptions) (install.AutostartStatus, error) {
		return install.AutostartStatus{
			Platform:         "linux",
			Supported:        true,
			Manager:          install.ServiceManagerSystemdUser,
			CurrentManager:   install.ServiceManagerSystemdUser,
			Status:           "enabled",
			Configured:       true,
			Enabled:          true,
			InstallStatePath: opts.StatePath,
			CanApply:         true,
		}, nil
	}

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/autostart/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var detect autostartResponse
	if err := json.NewDecoder(rec.Body).Decode(&detect); err != nil {
		t.Fatalf("decode detect: %v", err)
	}
	if detect.Status != "disabled" {
		t.Fatalf("detect status = %q, want disabled", detect.Status)
	}

	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/autostart/apply", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("apply status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var applied autostartResponse
	if err := json.NewDecoder(rec.Body).Decode(&applied); err != nil {
		t.Fatalf("decode apply: %v", err)
	}
	if applied.Status != "enabled" || !applied.Enabled {
		t.Fatalf("unexpected apply payload: %#v", applied)
	}
}

func TestSetupAutostartEndpointsRemainAvailableAfterCredentialsSaved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	app, token := newRemoteSetupTestApp(t, home)
	cookie := exchangeSetupSessionCookie(t, app, token)

	originalDetect := detectAutostart
	originalApply := applyAutostart
	defer func() {
		detectAutostart = originalDetect
		applyAutostart = originalApply
	}()

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
	applyAutostart = func(opts install.AutostartApplyOptions) (install.AutostartStatus, error) {
		return install.AutostartStatus{
			Platform:         "linux",
			Supported:        true,
			Manager:          install.ServiceManagerSystemdUser,
			CurrentManager:   install.ServiceManagerSystemdUser,
			Status:           "enabled",
			Configured:       true,
			Enabled:          true,
			InstallStatePath: opts.StatePath,
			CanApply:         true,
		}, nil
	}

	req := performSetupRequestWithCookie(http.MethodGet, "/api/setup/autostart/detect", "", cookie)
	rec := performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	req = performSetupRequestWithCookie(http.MethodPost, "/api/setup/autostart/apply", "", cookie)
	rec = performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
}

func seedBinaryForDaemonTest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex-remote")
	writeExecutableFile(t, path, "wrapper-binary")
	return path
}
