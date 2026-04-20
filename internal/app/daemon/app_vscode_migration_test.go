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

	"github.com/kxn/codex-remote-feishu/internal/adapter/editor"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
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

	gateway := newLifecycleGateway()
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

	card := waitForLifecycleOperationTitle(t, gateway, "VS Code 接入需要迁移")
	if !strings.Contains(operationCardText(card), "旧版 settings.json 覆盖") {
		t.Fatalf("expected migration reason in structured card text, got %#v", card)
	}
	if !operationHasCallbackButton(card, "迁移并重新接入", vscodeMigrationOwnerPayloadKind, vscodeMigrationOwnerActionRun) {
		t.Fatalf("expected migration card button, got %#v", card.CardElements)
	}
}

func TestDaemonVSCodeMigrateCommandRejectsNormalMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VSCODE_SERVER_EXTENSIONS_DIR", filepath.Join(home, ".vscode-server", "extensions"))

	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	gateway := newLifecycleGateway()
	app, _, _ := newVSCodeAdminTestAppWithGateway(t, gateway, home, binaryPath, true)

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionVSCodeMigrateCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/vscode-migrate",
	})

	card := waitForLifecycleOperationTitle(t, gateway, "仅 VS Code 模式可用")
	if !strings.Contains(operationCardText(card), "/mode vscode") {
		t.Fatalf("expected normal-mode rejection to mention /mode vscode, got %#v", card.CardElements)
	}
	if operationHasCallbackButton(card, "迁移并重新接入", vscodeMigrationOwnerPayloadKind, vscodeMigrationOwnerActionRun) {
		t.Fatalf("expected normal-mode rejection card to avoid migrate action, got %#v", card.CardElements)
	}

	app.mu.Lock()
	flow := app.activeVSCodeMigrationFlowLocked("surface-1")
	app.mu.Unlock()
	if flow != nil {
		t.Fatalf("expected normal-mode /vscode-migrate to avoid opening owner flow, got %#v", flow)
	}
}

func TestHandleGatewayActionReplacesModeCardWithVSCodeMigrationPrompt(t *testing.T) {
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
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-mode-vscode-1",
		Text:             "/mode vscode",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected stamped mode switch to replace current card, got %#v", result)
	}
	if result.ReplaceCurrentCard.CardTitle != "VS Code 接入需要迁移" {
		t.Fatalf("expected migration prompt to stay on current card, got %#v", result.ReplaceCurrentCard)
	}
	if !operationHasCallbackButton(*result.ReplaceCurrentCard, "迁移并重新接入", vscodeMigrationOwnerPayloadKind, vscodeMigrationOwnerActionRun) {
		t.Fatalf("expected in-place migration button, got %#v", result.ReplaceCurrentCard.CardElements)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
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

	gateway := newLifecycleGateway()
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

	card := waitForLifecycleOperationTitle(t, gateway, "VS Code 接入需要修复")
	if !operationHasCallbackButton(card, "重新接入扩展入口", vscodeMigrationOwnerPayloadKind, vscodeMigrationOwnerActionRun) {
		t.Fatalf("expected repair card button, got %#v", card.CardElements)
	}
}

func TestDaemonVSCodeMigrateCommandOpensOwnerFlowAndAppliesManagedShim(t *testing.T) {
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

	app.service.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeVSCode, state.SurfaceVerbosityNormal)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionVSCodeMigrateCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/vscode-migrate",
	})

	prompt := findOperationByTitle(gateway.operations, "VS Code 接入需要迁移")
	if prompt == nil {
		t.Fatalf("expected /vscode-migrate to open migration page, got %#v", gateway.operations)
	}
	if !operationHasCallbackButton(*prompt, "迁移并重新接入", vscodeMigrationOwnerPayloadKind, vscodeMigrationOwnerActionRun) {
		t.Fatalf("expected /vscode-migrate page to expose owner-flow callback, got %#v", prompt.CardElements)
	}
	flowID := requiredCallbackPickerID(t, *prompt, "迁移并重新接入", vscodeMigrationOwnerPayloadKind)

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionVSCodeMigrate,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        prompt.MessageID,
		PickerID:         flowID,
		OptionID:         vscodeMigrationOwnerActionRun,
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})
	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected migrate owner-flow callback to replace current card, got %#v", result)
	}
	if !strings.Contains(operationCardText(*result.ReplaceCurrentCard), "请重新打开 VS Code 开始使用") {
		t.Fatalf("expected owner-flow callback to show reopen guidance, got %#v", result.ReplaceCurrentCard.CardElements)
	}

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
	if _, err := os.Stat(editor.ManagedShimRealBinaryPath(entrypoint)); err != nil {
		t.Fatalf("expected shim backup after migration: %v", err)
	}
}

func TestHandleGatewayActionReplacesVSCodeMigrationCardWithResult(t *testing.T) {
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
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	prompt := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-mode-vscode-1",
		Text:             "/mode vscode",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})
	if prompt == nil || prompt.ReplaceCurrentCard == nil {
		t.Fatalf("expected stamped /mode vscode to replace current card with migration prompt, got %#v", prompt)
	}
	flowID := requiredCallbackPickerID(t, *prompt.ReplaceCurrentCard, "迁移并重新接入", vscodeMigrationOwnerPayloadKind)

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionVSCodeMigrate,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-mode-vscode-1",
		PickerID:         flowID,
		OptionID:         vscodeMigrationOwnerActionRun,
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected stamped migrate callback to replace current card, got %#v", result)
	}
	if !strings.Contains(operationCardText(*result.ReplaceCurrentCard), "请重新打开 VS Code 开始使用") {
		t.Fatalf("expected in-place migrate result text, got %#v", result.ReplaceCurrentCard.CardElements)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
	if _, err := os.Stat(editor.ManagedShimRealBinaryPath(entrypoint)); err != nil {
		t.Fatalf("expected shim backup after in-place migration: %v", err)
	}
}

func TestHandleGatewayActionKeepsLaterVSCodeGuidanceOnSameCard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VSCODE_SERVER_EXTENSIONS_DIR", filepath.Join(home, ".vscode-server", "extensions"))

	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	entrypoint := testVSCodeBundleEntrypoint(home, ".vscode-server", "1")
	writeExecutableFile(t, entrypoint, "orig")

	gateway := newLifecycleGateway()
	app, _, _ := newVSCodeAdminTestAppWithGateway(t, gateway, home, binaryPath, true)
	app.service.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeVSCode, state.SurfaceVerbosityNormal)

	prompt := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionVSCodeMigrateCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-vscode-migrate-1",
		Text:             "/vscode-migrate",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})
	if prompt == nil || prompt.ReplaceCurrentCard == nil {
		t.Fatalf("expected stamped /vscode-migrate to replace current card with migration prompt, got %#v", prompt)
	}
	flowID := requiredCallbackPickerID(t, *prompt.ReplaceCurrentCard, "重新接入扩展入口", vscodeMigrationOwnerPayloadKind)

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionVSCodeMigrate,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-vscode-migrate-1",
		PickerID:         flowID,
		OptionID:         vscodeMigrationOwnerActionRun,
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected stamped migrate callback to replace current card, got %#v", result)
	}

	app.handleUIEvents(context.Background(), []control.UIEvent{{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: "surface-1",
		Notice: &control.Notice{
			Code:  "not_attached_vscode",
			Title: "请先选择实例",
			Text:  "vscode 模式下请先 /list 选择一个 VS Code 实例，再使用 /use 或 /useall。",
		},
	}})

	ops := gateway.snapshotOperations()
	if len(ops) != 1 {
		t.Fatalf("expected later vscode guidance to patch same card instead of appending, got %#v", ops)
	}
	if ops[0].Kind != feishu.OperationUpdateCard || ops[0].MessageID != "om-vscode-migrate-1" {
		t.Fatalf("expected in-place vscode guidance update on migrate card, got %#v", ops[0])
	}
	if !strings.Contains(operationCardText(ops[0]), "/list") {
		t.Fatalf("expected follow-up guidance text to keep /list hint, got %#v", ops[0].CardElements)
	}
	if !operationHasCommandButton(ops[0], "选择实例", "/list") {
		t.Fatalf("expected follow-up guidance to keep select-instance button, got %#v", ops[0].CardElements)
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

	gateway := newLifecycleGateway()
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

	waitForDaemonCondition(t, 2*time.Second, func() bool { return detectCalls == 1 })
	card := waitForLifecycleOperationTitle(t, gateway, "VS Code 接入需要迁移")
	if card.CardTitle == "" {
		t.Fatalf("expected migration card for restored vscode surface")
	}
	if !strings.Contains(operationCardText(card), "旧版 settings.json 覆盖") {
		t.Fatalf("expected restored vscode surface prompt to keep migration reason, got %#v", card)
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

func operationHasCallbackButton(operation feishu.Operation, label, kind, optionID string) bool {
	for _, button := range operationCardButtons(operation) {
		if buttonMatchesCallback(button, label, kind, optionID) {
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

func buttonMatchesCallback(button map[string]any, label, kind, optionID string) bool {
	textNode, _ := button["text"].(map[string]any)
	content, _ := textNode["content"].(string)
	if content != label {
		return false
	}
	value := cardButtonPayload(button)
	actualKind, _ := value["kind"].(string)
	actualOptionID, _ := value[vscodeMigrationOwnerPayloadRunKey].(string)
	return actualKind == kind && actualOptionID == optionID
}

func requiredCallbackPickerID(t *testing.T, operation feishu.Operation, label, kind string) string {
	t.Helper()
	for _, button := range operationCardButtons(operation) {
		textNode, _ := button["text"].(map[string]any)
		content, _ := textNode["content"].(string)
		if content != label {
			continue
		}
		value := cardButtonPayload(button)
		if actualKind, _ := value["kind"].(string); actualKind != kind {
			continue
		}
		flowID, _ := value[vscodeMigrationOwnerPayloadFlowKey].(string)
		if strings.TrimSpace(flowID) != "" {
			return flowID
		}
	}
	t.Fatalf("expected callback button %q kind=%q with picker_id in %#v", label, kind, operation.CardElements)
	return ""
}
