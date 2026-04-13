package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestVSCodeApplyManagedShimClearsLegacySettingsOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VSCODE_SERVER_EXTENSIONS_DIR", filepath.Join(home, ".vscode-server", "extensions"))

	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	defaults, err := install.DetectPlatformDefaults()
	if err != nil {
		t.Fatalf("DetectPlatformDefaults: %v", err)
	}
	writeLegacyVSCodeSettings(t, defaults.VSCodeSettingsPath, binaryPath)

	entrypoint := testVSCodeBundleEntrypoint(home, ".vscode-server", "1")
	writeExecutableFile(t, entrypoint, "orig")

	app, _, _ := newVSCodeAdminTestApp(t, home, binaryPath, true)

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/vscode/apply", `{"mode":"managed_shim"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	var detect vscodeDetectResponse
	if err := json.NewDecoder(rec.Body).Decode(&detect); err != nil {
		t.Fatalf("decode detect: %v", err)
	}
	if detect.Settings.CLIExecutable != "" || detect.Settings.MatchesBinary {
		t.Fatalf("expected legacy settings override to be cleared, got %#v", detect.Settings)
	}

	rawSettings, err := os.ReadFile(defaults.VSCodeSettingsPath)
	if err != nil {
		t.Fatalf("ReadFile(settings): %v", err)
	}
	if strings.Contains(string(rawSettings), "chatgpt.cliExecutable") {
		t.Fatalf("expected apply to remove cliExecutable override, got %s", string(rawSettings))
	}
}

func TestDaemonModeSwitchToVSCodePromptsMigrationForLegacySettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VSCODE_SERVER_EXTENSIONS_DIR", filepath.Join(home, ".vscode-server", "extensions"))

	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	defaults, err := install.DetectPlatformDefaults()
	if err != nil {
		t.Fatalf("DetectPlatformDefaults: %v", err)
	}
	writeLegacyVSCodeSettings(t, defaults.VSCodeSettingsPath, binaryPath)

	entrypoint := testVSCodeBundleEntrypoint(home, ".vscode-server", "1")
	writeExecutableFile(t, entrypoint, "orig")

	gateway := &recordingGateway{}
	app, _, _ := newVSCodeAdminTestAppWithGateway(t, gateway, home, binaryPath, true)

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})

	card := findOperationByTitle(gateway.operations, "VS Code 接入需要迁移")
	if card == nil {
		t.Fatalf("expected migration card after switching to vscode mode, got %#v", gateway.operations)
	}
	if !strings.Contains(card.CardBody, "旧版 settings.json 覆盖") {
		t.Fatalf("expected migration reason in card body, got %#v", card)
	}
	if !operationHasCommandButton(*card, "迁移并重新接入", vscodeMigrateCommandText) {
		t.Fatalf("expected migration card button, got %#v", card.CardElements)
	}
}

func TestDaemonVSCodeCompatibilityBlocksAutoResumeUntilMigrationApplied(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VSCODE_SERVER_EXTENSIONS_DIR", filepath.Join(home, ".vscode-server", "extensions"))

	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	entrypointV1 := testVSCodeBundleEntrypoint(home, ".vscode-server", "1")
	writeExecutableFile(t, entrypointV1, "orig-v1")
	putSurfaceResumeStateForTest(t, filepath.Join(home, ".local", "state", "codex-remote"), SurfaceResumeEntry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "vscode",
		ResumeInstanceID:   "inst-vscode-1",
		ResumeThreadID:     "thread-1",
		ResumeWorkspaceKey: "/data/dl/droid",
		ResumeRouteMode:    "follow_local",
	})

	gateway := &recordingGateway{}
	app, _, _ := newVSCodeAdminTestAppWithGateway(t, gateway, home, binaryPath, true)

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/vscode/apply", `{"mode":"managed_shim"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	entrypointV2 := testVSCodeBundleEntrypoint(home, ".vscode-server", "2")
	writeExecutableFile(t, entrypointV2, "orig-v2")
	now := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Dir(filepath.Dir(filepath.Dir(entrypointV2))), now, now); err != nil {
		t.Fatalf("Chtimes(new extension dir): %v", err)
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-vscode-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
			Source:        "vscode",
		},
	})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "" {
		t.Fatalf("expected compatibility issue to keep vscode surface detached, got %#v", snapshot)
	}

	card := findOperationByTitle(gateway.operations, "VS Code 接入需要修复")
	if card == nil {
		t.Fatalf("expected repair card while managed shim is stale, got %#v", gateway.operations)
	}
	if !operationHasCommandButton(*card, "重新接入扩展入口", vscodeMigrateCommandText) {
		t.Fatalf("expected repair card button, got %#v", card.CardElements)
	}
}

func TestDaemonVSCodeMigrateCommandAppliesManagedShimAndPromptsReopen(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VSCODE_SERVER_EXTENSIONS_DIR", filepath.Join(home, ".vscode-server", "extensions"))

	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	defaults, err := install.DetectPlatformDefaults()
	if err != nil {
		t.Fatalf("DetectPlatformDefaults: %v", err)
	}
	writeLegacyVSCodeSettings(t, defaults.VSCodeSettingsPath, binaryPath)

	entrypoint := testVSCodeBundleEntrypoint(home, ".vscode-server", "1")
	writeExecutableFile(t, entrypoint, "orig")

	gateway := &recordingGateway{}
	app, _, _ := newVSCodeAdminTestAppWithGateway(t, gateway, home, binaryPath, true)

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionVSCodeMigrate,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/vscode/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var detect vscodeDetectResponse
	if err := json.NewDecoder(rec.Body).Decode(&detect); err != nil {
		t.Fatalf("decode detect: %v", err)
	}
	if detect.CurrentMode != "managed_shim" {
		t.Fatalf("expected managed_shim after migration, got %#v", detect)
	}
	if detect.Settings.CLIExecutable != "" || detect.Settings.MatchesBinary {
		t.Fatalf("expected legacy settings override cleared after migration, got %#v", detect.Settings)
	}
	if _, err := os.Stat(entrypoint + ".real"); err != nil {
		t.Fatalf("expected shim backup after migration: %v", err)
	}

	found := false
	for _, operation := range gateway.operations {
		if strings.Contains(operation.CardBody, "请重新打开 VS Code 开始使用") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected reopen-vscode success notice, got %#v", gateway.operations)
	}
}

func TestDaemonTickSkipsVSCodeCompatibilityDetectWithoutVSCodeSurface(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	app, _, _ := newVSCodeAdminTestApp(t, home, binaryPath, false)
	detectCalls := 0
	app.vscodeDetect = func() (vscodeDetectResponse, error) {
		detectCalls++
		return vscodeDetectResponse{}, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	app.onTick(context.Background(), time.Now().UTC())
	app.onTick(context.Background(), time.Now().UTC().Add(time.Second))

	if detectCalls != 0 {
		t.Fatalf("expected no vscode compatibility detect on normal tick, got %d", detectCalls)
	}
}

func TestDaemonTickChecksVSCodeCompatibilityOnlyOnceForRestoredVSCodeSurface(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	putSurfaceResumeStateForTest(t, filepath.Join(home, ".local", "state", "codex-remote"), SurfaceResumeEntry{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ProductMode:      "vscode",
	})

	gateway := &recordingGateway{}
	app, _, _ := newVSCodeAdminTestAppWithGateway(t, gateway, home, binaryPath, false)
	detectCalls := 0
	app.vscodeDetect = func() (vscodeDetectResponse, error) {
		detectCalls++
		return vscodeDetectResponse{
			CurrentMode:            "editor_settings",
			LatestBundleEntrypoint: "/tmp/fake-entrypoint",
		}, nil
	}

	app.onTick(context.Background(), time.Now().UTC())
	app.onTick(context.Background(), time.Now().UTC().Add(time.Second))

	if detectCalls != 1 {
		t.Fatalf("expected restored vscode surface to trigger exactly one compatibility detect, got %d", detectCalls)
	}
	card := findOperationByTitle(gateway.operations, "VS Code 接入需要迁移")
	if card == nil {
		t.Fatalf("expected migration card for restored vscode surface, got %#v", gateway.operations)
	}
}

func writeLegacyVSCodeSettings(t *testing.T, path, binaryPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(settings dir): %v", err)
	}
	raw, err := json.MarshalIndent(map[string]any{
		"chatgpt.cliExecutable": binaryPath,
		"editor.fontSize":       14,
	}, "", "  ")
	if err != nil {
		t.Fatalf("Marshal(settings): %v", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("WriteFile(settings): %v", err)
	}
}

func findOperationByTitle(operations []feishu.Operation, title string) *feishu.Operation {
	for i := range operations {
		if operations[i].CardTitle == title {
			return &operations[i]
		}
	}
	return nil
}

func operationHasCommandButton(operation feishu.Operation, label, commandText string) bool {
	for _, button := range operationCardButtons(operation) {
		if buttonMatchesCommand(button, label, commandText) {
			return true
		}
	}
	return false
}

func buttonMatchesCommand(button map[string]any, label, commandText string) bool {
	textNode, _ := button["text"].(map[string]any)
	content, _ := textNode["content"].(string)
	if content != label {
		return false
	}
	value := cardButtonPayload(button)
	actualCommand, _ := value["command_text"].(string)
	return actualCommand == commandText
}
