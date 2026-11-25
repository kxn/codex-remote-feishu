package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type newWorkspaceJourneyCatalog struct{ cwd string }

func (c newWorkspaceJourneyCatalog) RecentThreads(int) ([]state.ThreadRecord, error) {
	return []state.ThreadRecord{{ThreadID: "fixture-history", CWD: c.cwd, Name: "Previous session"}}, nil
}
func (c newWorkspaceJourneyCatalog) RecentWorkspaces(int) (map[string]time.Time, error) {
	return map[string]time.Time{c.cwd: time.Now()}, nil
}
func (c newWorkspaceJourneyCatalog) ThreadByID(string) (*state.ThreadRecord, error) { return nil, nil }

// The native binary is explicitly supplied by the local test operator; no
// prompt is sent and the platform is the in-memory gateway.
func TestPrivateSDKPickerNewWorkspaceRealCodex(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX wrapper fixture")
	}
	codex := os.Getenv("PRIVATE_REAL_CODEX_BIN")
	if codex == "" {
		t.Skip("set PRIVATE_REAL_CODEX_BIN to test actual local Codex")
	}
	app, gateway := newApplicationRuntimeTestApp(t)
	r := app.applications
	cwd := t.TempDir()
	app.service.SetPersistedThreadCatalog(newWorkspaceJourneyCatalog{cwd: cwd})
	settings := state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.OAuthCodexProfileID}
	if os.Getenv("PRIVATE_REAL_CODEX_EXISTING_MODEL") == "1" {
		settings.PromptOverride.Model = os.Getenv("PRIVATE_REAL_CODEX_MODEL")
		if settings.PromptOverride.Model == "" {
			t.Fatal("existing model case requires PRIVATE_REAL_CODEX_MODEL")
		}
	}
	setTestWorkBotSettings(t, app, settings)
	timeout := 10 * time.Second
	if os.Getenv("PRIVATE_REAL_CODEX_TWO_TURNS") == "1" {
		timeout = 180 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := app.headlessRuntime.Paths.StateDir
	pinnedVersion := os.Getenv("PRIVATE_REAL_CODEX_VERSION")
	if pinnedVersion == "" {
		t.Skip("set PRIVATE_REAL_CODEX_VERSION for the isolated managed pin")
	}
	managedRoot := filepath.Join(root, "codex-runtime")
	if err = os.MkdirAll(managedRoot, 0700); err != nil {
		t.Fatal(err)
	}
	pin, err := json.Marshal(map[string]string{"binary": codex, "version": pinnedVersion})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(managedRoot, "exact-version.json"), pin, 0600); err != nil {
		t.Fatal(err)
	}
	isolatedHome := t.TempDir()
	if sourceHome := os.Getenv("PRIVATE_REAL_CODEX_HOME"); sourceHome != "" {
		credentials, readErr := os.ReadFile(filepath.Join(sourceHome, "auth.json"))
		if readErr != nil {
			t.Fatalf("read supplied Codex authentication: %v", readErr)
		}
		isolatedCodex := filepath.Join(isolatedHome, ".codex")
		if err = os.MkdirAll(isolatedCodex, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(isolatedCodex, "auth.json"), credentials, 0600); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("PRIVATE_REAL_CODEX_TWO_TURNS") == "1" {
			if raw, e := os.ReadFile(filepath.Join(sourceHome, "config.toml")); e == nil {
				if e = os.WriteFile(filepath.Join(isolatedCodex, "config.toml"), raw, 0600); e != nil {
					t.Fatal(e)
				}
			} else if !os.IsNotExist(e) {
				t.Fatal(e)
			}
		}
	} else {
		t.Skip("set PRIVATE_REAL_CODEX_HOME for isolated OAuth probe")
	}
	app.headlessRuntime.BaseEnv = []string{"HOME=" + isolatedHome, "PATH=" + os.Getenv("PATH")}
	if os.Getenv("PRIVATE_REAL_CODEX_TWO_TURNS") == "1" {
		app.headlessRuntime.BaseEnv = buildDaemonHeadlessBaseEnv(os.Environ(), config.CaptureProxyEnv())
		app.headlessRuntime.BaseEnv = config.UpsertEnvValue(app.headlessRuntime.BaseEnv, "HOME", isolatedHome)
		app.headlessRuntime.BaseEnv = config.UpsertEnvValue(app.headlessRuntime.BaseEnv, "CODEX_HOME", filepath.Join(isolatedHome, ".codex"))
	}
	app.headlessRuntime.CodexRealBinary = codex
	app.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
	server := httptest.NewServer(app.relay)
	defer server.Close()
	relayURL := "ws" + strings.TrimPrefix(server.URL, "http")
	var child *exec.Cmd
	r.native.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) {
		child = exec.Command(exe, "-test.run=^TestPrivateRealCodexWrapperProcess$")
		child.Env = append(v.Env, "PRIVATE_REAL_CODEX_WRAPPER=1", "FIXTURE_RELAY_URL="+relayURL, "FIXTURE_STATE_DIR="+root, "FIXTURE_CODEX_BIN="+codex)
		child.Dir = v.WorkDir
		child.Stdout = io.Discard
		child.Stderr = os.Stderr
		if e := child.Start(); e != nil {
			return 0, e
		}
		return child.Process.Pid, nil
	}
	defer func() {
		if child != nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	errorsCh := make(chan error, 8)
	if err = r.startWorkers(ctx, errorsCh); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = r.stopWorkers(stop)
		if child != nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
		_ = r.drainNativeDeliveries(stop)
	}()
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "list", MessageID: "source", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/list"}`, ReceivedAt: time.Now()}
	if os.Getenv("PRIVATE_REAL_CODEX_TOOLS") == "1" {
		access := raw
		access.EventID, access.MessageID, access.RawContent = "real-access", "real-access", `{"text":"/access full"}`
		if e := r.hook.Receive(ctx, access); e != nil {
			t.Fatal(e)
		}
		owner := privatechat.Owner{GatewayID: "gateway", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "private-chat", UserID: "user", TenantID: "tenant", AppID: "app"}
		for {
			cp, e := r.journal.LoadCheckpoint(ctx, owner, 1)
			if e == nil && cp.Core.PromptOverride.AccessMode == agentproto.AccessModeFullAccess {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("SDK full access setting did not persist")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	if os.Getenv("PRIVATE_REAL_CODEX_PLAN") == "1" {
		plan := raw
		plan.EventID, plan.MessageID, plan.RawContent = "real-plan", "real-plan", `{"text":"/plan on"}`
		if e := r.hook.Receive(ctx, plan); e != nil {
			t.Fatal(e)
		}
		owner := privatechat.Owner{GatewayID: "gateway", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "private-chat", UserID: "user", TenantID: "tenant", AppID: "app"}
		for {
			cp, e := r.journal.LoadCheckpoint(ctx, owner, 1)
			if e == nil && cp.Core.PlanModeOverrideSet && cp.Core.PlanMode == "on" {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("SDK plan setting did not persist")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	if err = r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	var card feishu.Operation
	for card.MessageID == "" {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind == feishu.OperationSendCard && op.MessageID != "" {
				f, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
				var d any
				if e == nil && json.Unmarshal(f, &d) == nil && findPrivateCallback(d, "target_picker_confirm") != nil {
					card = op
					break
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("picker was not published")
		case <-time.After(10 * time.Millisecond):
		}
	}
	for {
		if _, e := r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", card.MessageID); e == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("picker publication was not registered")
		case <-time.After(10 * time.Millisecond):
		}
	}
	frozen, err := feishu.FreezePrivateOperation(card, app.daemonLifecycleID)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err = json.Unmarshal(frozen, &document); err != nil {
		t.Fatal(err)
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", ReceivedAt: time.Now()}
	if selectValue := findPrivateCallback(document, "target_picker_select_workspace"); selectValue != nil {
		click.EventID = "choose-workspace"
		click.Values = selectValue
		click.Option = cwd
		response, e := r.hook.ReceiveInteractionResponse(ctx, click)
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(response.RawBody, &document); e != nil {
			t.Fatal(e)
		}
	}
	confirm := findPrivateCallback(document, "target_picker_confirm")
	if confirm == nil {
		t.Fatal("new-session confirm missing")
	}
	click.EventID = "confirm-new"
	click.Values = confirm
	click.Option = ""
	confirmResponse, confirmErr := r.hook.ReceiveInteractionResponse(ctx, click)
	if confirmErr != nil {
		t.Fatal(confirmErr)
	}
	_ = confirmResponse
	surface := gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user")
	readyInstance := ""
	for readyInstance == "" {
		status, e := r.processor.ReadRuntimeStatus(ctx)
		if e != nil {
			t.Fatal(e)
		}
		for _, s := range status.Surfaces {
			if s.SurfaceSessionID == surface && s.RouteMode == state.RouteModeNewThreadReady {
				if s.AttachedInstanceID == "" || s.PreparedThreadCWD != cwd || s.PendingHeadless != nil {
					t.Fatalf("incorrect new workspace: %+v", s)
				}
				for _, inst := range status.Instances {
					if inst.InstanceID == s.AttachedInstanceID && inst.CodexProfileID == state.OAuthCodexProfileID && inst.Online {
						readyInstance = inst.InstanceID
						break
					}
				}
				if readyInstance == "" {
					t.Fatal("connected instance missing oauth profile")
				}
				break
			}
		}
		if readyInstance != "" {
			break
		}
		select {
		case e := <-errorsCh:
			t.Fatal(e)
		case <-ctx.Done():
			for _, v := range status.Surfaces {
				t.Logf("surface route=%s pending=%+v attached=%q profile=%q", v.RouteMode, v.PendingHeadless, v.AttachedInstanceID, v.CodexProfileID)
			}
			for _, v := range status.Instances {
				t.Logf("instance online=%v backend=%s profile=%q cwd=%q", v.Online, v.Backend, v.CodexProfileID, v.WorkspaceRoot)
			}
			for _, op := range gateway.snapshotOperations() {
				rawOp, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
				t.Logf("operation=%s document=%s", op.Kind, rawOp)
			}
			t.Fatal("new session did not become ready")
		case <-time.After(20 * time.Millisecond):
		}
	}
	for {
		success := false
		for _, op := range gateway.snapshotOperations() {
			if op.Kind != feishu.OperationUpdateCard || op.MessageID != card.MessageID {
				continue
			}
			rawOp, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if e == nil && strings.Contains(string(rawOp), "已进入新会话待命") {
				success = true
				break
			}
		}
		if success {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("same-card success was not published")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if os.Getenv("PRIVATE_REAL_CODEX_TWO_TURNS") == "1" {
		testPrivateRealCodexTwoTurns(t, ctx, r, gateway, app, raw)
	}
	owner := privatechat.Owner{GatewayID: "gateway", SurfaceID: surface, ChatID: "private-chat", UserID: "user", TenantID: "tenant", AppID: "app"}
	effects, e := r.processor.ApplyOwnerDeadline(ctx, owner, 1, "after-fresh-hello", time.Now().Add(time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	for _, effect := range effects {
		event, _, decodeErr := privatechat.DecodeStoredEffect(effect.Effect.Body)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandKillHeadless {
			t.Fatal("deadline killed connected new instance")
		}
	}
	final, e := r.processor.ReadRuntimeStatus(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, inst := range final.Instances {
		if inst.InstanceID == readyInstance && inst.Online {
			return
		}
	}
	t.Fatal("connected new instance did not remain online")
}

// Runs the actual wrapper configuration loader with the exact environment
// produced by the private launcher; only test transport and binary paths are
// redirected to isolated local fixtures.
func TestPrivateRealCodexWrapperProcess(t *testing.T) {
	if os.Getenv("PRIVATE_REAL_CODEX_WRAPPER") != "1" {
		return
	}
	cfg, err := wrapper.LoadConfig([]string{"app-server"}, "fixture", "fixture")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(71)
	}
	cfg.RelayServerURL = os.Getenv("FIXTURE_RELAY_URL")
	cfg.CodexRealBinary = os.Getenv("FIXTURE_CODEX_BIN")
	cfg.RuntimePaths = relayruntime.Paths{StateDir: os.Getenv("FIXTURE_STATE_DIR"), ManagerLockFile: filepath.Join(os.Getenv("FIXTURE_STATE_DIR"), "manager.lock")}
	cfg.BuildFingerprint = "fixture"
	app := wrapper.New(cfg)
	code, err := app.Run(context.Background(), os.Stdin, io.Discard, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}
