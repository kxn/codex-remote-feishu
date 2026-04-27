package daemon

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestAdminRuntimeRequirementsDetectWithAbsoluteCodexPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	binaryPath := filepath.Join(home, executableName("codex-remote"))
	writeExecutableFile(t, binaryPath, "wrapper-binary")
	codexPath := filepath.Join(home, "bin", executableName("codex"))
	writeExecutableFile(t, codexPath, "real-codex")

	app, configPath, _ := newVSCodeAdminTestApp(t, home, binaryPath, false)
	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	loaded.Config.Wrapper.CodexRealBinary = codexPath
	if err := config.WriteAppConfig(configPath, loaded.Config); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/runtime-requirements/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var payload runtimeRequirementsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode detect: %v", err)
	}
	if !payload.Ready {
		t.Fatalf("expected runtime requirements to be ready, got %#v", payload)
	}
	if payload.CodexRealBinary != codexPath {
		t.Fatalf("codex real binary = %q, want %q", payload.CodexRealBinary, codexPath)
	}
	if !testutil.SamePath(payload.ResolvedCodexRealBinary, codexPath) {
		t.Fatalf("resolved codex real binary = %q, want %q", payload.ResolvedCodexRealBinary, codexPath)
	}
	if payload.LookupMode != "absolute" {
		t.Fatalf("lookup mode = %q, want absolute", payload.LookupMode)
	}
	if got := checkStatusByID(payload.Checks, "real_codex_binary"); got != runtimeRequirementStatusPass {
		t.Fatalf("real_codex_binary status = %q, want pass", got)
	}
	if got := checkStatusByID(payload.Checks, "lookup_mode"); got != runtimeRequirementStatusPass {
		t.Fatalf("lookup_mode status = %q, want pass", got)
	}
}

func TestAdminRuntimeRequirementsWarnWhenCodexComesFromPATH(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	binaryPath := filepath.Join(home, executableName("codex-remote"))
	writeExecutableFile(t, binaryPath, "wrapper-binary")
	pathDir := filepath.Join(home, "bin")
	codexPath := filepath.Join(pathDir, executableName("codex"))
	writeExecutableFile(t, codexPath, "real-codex")
	t.Setenv("PATH", pathDir)

	app, _, _ := newVSCodeAdminTestApp(t, home, binaryPath, false)

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/runtime-requirements/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var payload runtimeRequirementsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode detect: %v", err)
	}
	if !payload.Ready {
		t.Fatalf("expected runtime requirements to remain ready, got %#v", payload)
	}
	if payload.LookupMode != "path_search" {
		t.Fatalf("lookup mode = %q, want path_search", payload.LookupMode)
	}
	if !testutil.SamePath(payload.ResolvedCodexRealBinary, codexPath) {
		t.Fatalf("resolved codex real binary = %q, want %q", payload.ResolvedCodexRealBinary, codexPath)
	}
	if got := checkStatusByID(payload.Checks, "lookup_mode"); got != runtimeRequirementStatusWarn {
		t.Fatalf("lookup_mode status = %q, want warn", got)
	}
}

func TestAdminRuntimeRequirementsAcceptManagedShimBundleFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(home, "missing-bin"))

	binaryPath := filepath.Join(home, executableName("codex-remote"))
	writeExecutableFile(t, binaryPath, "wrapper-binary")
	bundleCodex := testVSCodeBundleEntrypoint(home, ".vscode-server", "26.422.30944-linux-x64")
	writeExecutableFile(t, bundleCodex, "bundle-codex")

	app, _, _ := newVSCodeAdminTestApp(t, home, binaryPath, false)

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/runtime-requirements/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var payload runtimeRequirementsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode detect: %v", err)
	}
	if !payload.Ready {
		t.Fatalf("expected runtime requirements to remain ready with bundle fallback, got %#v", payload)
	}
	if !testutil.SamePath(payload.ResolvedCodexRealBinary, bundleCodex) {
		t.Fatalf("resolved codex real binary = %q, want %q", payload.ResolvedCodexRealBinary, bundleCodex)
	}
	if payload.LookupMode != "path_search" {
		t.Fatalf("lookup mode = %q, want path_search", payload.LookupMode)
	}
	if got := checkStatusByID(payload.Checks, "real_codex_binary"); got != runtimeRequirementStatusPass {
		t.Fatalf("real_codex_binary status = %q, want pass", got)
	}
	if got := checkStatusByID(payload.Checks, "lookup_mode"); got != runtimeRequirementStatusWarn {
		t.Fatalf("lookup_mode status = %q, want warn", got)
	}
}

func TestAdminRuntimeRequirementsFailWhenCodexPointsBackToCurrentBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	binaryPath := filepath.Join(home, executableName("codex-remote"))
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	app, configPath, _ := newVSCodeAdminTestApp(t, home, binaryPath, false)
	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	loaded.Config.Wrapper.CodexRealBinary = binaryPath
	if err := config.WriteAppConfig(configPath, loaded.Config); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/runtime-requirements/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var payload runtimeRequirementsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode detect: %v", err)
	}
	if payload.Ready {
		t.Fatalf("expected runtime requirements to fail, got %#v", payload)
	}
	if got := checkStatusByID(payload.Checks, "binary_loop"); got != runtimeRequirementStatusFail {
		t.Fatalf("binary_loop status = %q, want fail", got)
	}
}

func TestSetupRuntimeRequirementsEndpointRemainAvailableAfterCredentialsSaved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	pathDir := filepath.Join(home, "bin")
	codexPath := filepath.Join(pathDir, executableName("codex"))
	writeExecutableFile(t, codexPath, "real-codex")
	t.Setenv("PATH", pathDir)

	app, token := newRemoteSetupTestApp(t, home)
	cookie := exchangeSetupSessionCookie(t, app, token)

	req := performSetupRequestWithCookie(http.MethodGet, "/api/setup/runtime-requirements/detect", "", cookie)
	rec := performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
}

func checkStatusByID(checks []runtimeRequirementCheck, id string) string {
	for _, check := range checks {
		if check.ID == id {
			return check.Status
		}
	}
	return ""
}

func executableName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}
