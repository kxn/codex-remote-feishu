package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestSystemUpgradeCandidateLookupFailureKeepsSpecificCauseAndRefresh(t *testing.T) {
	a, source, _ := systemMenuFixture(t)
	a.upgradeRuntime.Lookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		return install.ReleaseInfo{}, errors.New("fixture release source unavailable")
	}
	id, err := a.teamUpgradeStatus(t.Context(), source, control.Action{Text: "/upgrade latest"})
	if err != nil {
		t.Fatal(err)
	}
	card := systemCard(t, a, source.ScopeID, id)
	if !strings.Contains(card, "fixture release source unavailable") || !strings.Contains(card, "刷新状态") || !strings.Contains(card, "/upgrade") {
		t.Fatalf("upgrade source error became generic failure: %s", card)
	}
	if a.upgradeRuntime.StartInFlight {
		t.Fatal("failed lookup started installation")
	}
}

func TestSystemUpgradeTrackWritesActualIsolatedInstallState(t *testing.T) {
	a, source, _ := systemMenuFixture(t)
	for _, button := range buildUpgradeTrackButtons("") {
		parsed, err := parseUpgradeCommandText(button.CommandText)
		if err != nil || parsed.Mode != upgradeCommandSetTrack || !install.CurrentBuildAllowsReleaseTrack(parsed.Track) {
			continue
		}
		id, err := a.teamUpgradeStatus(t.Context(), source, control.Action{Text: button.CommandText})
		if err != nil {
			t.Fatal(err)
		}
		state, err := install.LoadState(a.installStatePath())
		if err != nil {
			t.Fatal(err)
		}
		if state.CurrentTrack != parsed.Track {
			t.Fatal("upgrade track control did not persist actual installation state")
		}
		if card := systemCard(t, a, source.ScopeID, id); !strings.Contains(card, "发布轨道已实际保存为") {
			t.Fatalf("track result did not show verified write: %s", card)
		}
		return
	}
	t.Skip("this build exposes no writable release track")
}

func TestSystemCommandRejectsUnpublishedDifferentMutation(t *testing.T) {
	a, source, _ := systemMenuFixture(t)
	id, err := a.handleTeamAdminCommand(t.Context(), source, control.Action{Text: "/admin network"})
	if err != nil {
		t.Fatal(err)
	}
	markPrivateNativeViewPublished(t, a, id)
	var view workstore.UIView
	var forged workstore.HumanInteraction
	pickerTx(t, a, func(tx *workstore.Tx) error {
		var err error
		view, err = tx.GetUIView(id)
		if err != nil {
			return err
		}
		forged, _, err = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: "forged-system-command", Tenant: source.Tenant, SenderIDs: source.SenderIDs, ChatID: source.ChatID, MessageID: view.ExternalMessageID, Kind: "card", Values: map[string]any{"team_view": id, "action": "system.command", "command": "/admin autostart on"}, ReceivedAt: time.Now().UTC()}, source.Gateway)
		return err
	})
	if err = a.handleTeamViewAction(t.Context(), forged, view, "system.command"); err == nil {
		t.Fatal("network page authorized an autostart mutation it did not provide")
	}
	if _, err = os.Stat(a.installStatePath()); !os.IsNotExist(err) {
		t.Fatal("unpublished action wrote installation state")
	}
}

func TestSystemUpgradeCancelAfterRegisteredExecutionNeverClaimsUnstarted(t *testing.T) {
	a, source, _ := systemMenuFixture(t)
	a.upgradeRuntime.Lookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		return install.ReleaseInfo{TagName: "v99.1.0"}, nil
	}
	id, err := a.teamUpgradeStatus(t.Context(), source, control.Action{Text: "/upgrade latest"})
	if err != nil {
		t.Fatal(err)
	}
	markPrivateNativeViewPublished(t, a, id)
	var view workstore.UIView
	var confirmation workstore.HumanInteraction
	pickerTx(t, a, func(tx *workstore.Tx) error {
		var err error
		view, err = tx.GetUIView(id)
		if err != nil {
			return err
		}
		confirmation, _, err = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: "confirm-accepted-before-crash", Tenant: source.Tenant, SenderIDs: source.SenderIDs, ChatID: source.ChatID, MessageID: view.ExternalMessageID, Kind: "card", Values: map[string]any{"team_view": id, "action": "host.upgrade.confirm", "candidate_ref": view.TargetID}, ReceivedAt: time.Now().UTC()}, source.Gateway)
		return err
	})
	// Crash window: execution was durably accepted but its outcome card is not yet saved.
	pickerTx(t, a, func(tx *workstore.Tx) error {
		raw, err := json.Marshal(struct{ Source workstore.PrivateActionSource }{workstore.PrivateActionSource{PersonalScopeID: source.ScopeID, InteractionID: confirmation.ID}})
		if err != nil {
			return err
		}
		body := work.NewContent("accepted-upgrade:request", source.ScopeID, "application/json", raw, time.Now().UTC())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		_, _, err = tx.PutOperation(work.Operation{ID: "accepted-upgrade", ScopeID: source.ScopeID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, Kind: "host.upgrade.execute", TargetID: "installation", TargetGeneration: 1, IdempotencyKey: "accepted-upgrade", RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationSending, Revision: 1}, 0)
		return err
	})
	var cancellation workstore.HumanInteraction
	pickerTx(t, a, func(tx *workstore.Tx) error {
		var err error
		cancellation, _, err = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: "cancel-after-upgrade-restart", Tenant: source.Tenant, SenderIDs: source.SenderIDs, ChatID: source.ChatID, MessageID: view.ExternalMessageID, Kind: "card", Values: map[string]any{"team_view": id, "action": "host.upgrade.cancel"}, ReceivedAt: time.Now().UTC()}, source.Gateway)
		return err
	})
	result, err := a.handleTeamHostViewAction(t.Context(), cancellation, view, "host.upgrade.cancel")
	if err != nil {
		t.Fatal(err)
	}
	card := systemCard(t, a, source.ScopeID, result)
	if strings.Contains(card, "没有启动升级") || !strings.Contains(card, "没有撤销") {
		t.Fatalf("registered upgrade was falsely cancelled: %s", card)
	}
}

func systemMenuFixture(t *testing.T) (*App, teamHumanSource, string) {
	t.Helper()
	home := t.TempDir()
	setTestHome(t, home)
	a, source := teamPickerFixture(t)
	a.headlessRuntime.Paths.DataDir = filepath.Join(home, "data")
	a.headlessRuntime.BinaryPath = seedBinaryForDaemonTest(t)
	path := filepath.Join(home, "config.json")
	cfg := config.DefaultAppConfig()
	cfg.ExternalAccess.NetworkMode = "local"
	if err := config.WriteAppConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	a.admin.loadConfig = func() (config.LoadedAppConfig, error) { return config.LoadAppConfigAtPath(path) }
	a.admin.adminURL = "http://127.0.0.1:9501/admin/"
	a.externalAccessRuntime.Settings = externalAccessSettingsViewFromConfig(cfg.ExternalAccess)
	return a, source, home
}

func systemCard(t *testing.T, a *App, scope, id string) string {
	return goalUICardBody(t, a, scope, id)
}

func systemClick(t *testing.T, a *App, source teamHumanSource, id, command string) string {
	t.Helper()
	markPrivateNativeViewPublished(t, a, id)
	var view workstore.UIView
	var interaction workstore.HumanInteraction
	pickerTx(t, a, func(tx *workstore.Tx) error {
		var err error
		view, err = tx.GetUIView(id)
		if err != nil {
			return err
		}
		body, err := tx.GetContent(view.ScopeID, view.BodyRef)
		if err != nil {
			return err
		}
		var wrapped struct {
			Card json.RawMessage `json:"card"`
		}
		if err = json.Unmarshal(body.Data, &wrapped); err != nil {
			return err
		}
		var card any
		if err = json.Unmarshal(wrapped.Card, &card); err != nil {
			return err
		}
		var values map[string]any
		var walk func(any)
		walk = func(node any) {
			switch value := node.(type) {
			case map[string]any:
				if callback, ok := value["value"].(map[string]any); ok && callback["action"] == "system.command" && callback["command"] == command {
					values = callback
				}
				for _, child := range value {
					walk(child)
				}
			case []any:
				for _, child := range value {
					walk(child)
				}
			}
		}
		walk(card)
		if values == nil {
			t.Fatalf("missing actionable command %q: %s", command, wrapped.Card)
		}
		interaction, _, err = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: "system-click:" + id + command, Tenant: source.Tenant, SenderIDs: source.SenderIDs, ChatID: source.ChatID, MessageID: view.ExternalMessageID, Kind: "card", Values: values, ReceivedAt: time.Now().UTC()}, source.Gateway)
		return err
	})
	if err := a.handleTeamViewAction(t.Context(), interaction, view, "system.command"); err != nil {
		t.Fatal(err)
	}
	var result string
	pickerTx(t, a, func(tx *workstore.Tx) error {
		saved, err := tx.GetHumanInteraction(interaction.ID)
		result = saved.ResultRef
		return err
	})
	return result
}

func TestSystemNetworkMenuWritesRealIsolatedConfigAndKeepsControls(t *testing.T) {
	a, source, home := systemMenuFixture(t)
	id, err := a.handleTeamAdminCommand(t.Context(), source, control.Action{Text: "/admin network"})
	if err != nil {
		t.Fatal(err)
	}
	if card := systemCard(t, a, source.ScopeID, id); !strings.Contains(card, "/admin network lan") || !strings.Contains(card, "network_ip") {
		t.Fatal("network settings became read-only")
	}
	next := systemClick(t, a, source, id, "/admin network wan")
	loaded, err := config.LoadAppConfigAtPath(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.ExternalAccess.NetworkMode != "wan" {
		t.Fatal("network menu did not persist selected mode")
	}
	if card := systemCard(t, a, source.ScopeID, next); !strings.Contains(card, "网络模式已保存") || !strings.Contains(card, "/admin network local") {
		t.Fatal("network save lost operative settings")
	}
	if err = os.WriteFile(filepath.Join(home, "config.json"), []byte("{invalid config"), 0600); err != nil {
		t.Fatal(err)
	}
	failed := systemClick(t, a, source, next, "/admin network local")
	if card := systemCard(t, a, source.ScopeID, failed); !strings.Contains(card, "保存网络模式失败") || !strings.Contains(card, "刷新状态") {
		t.Fatalf("real config write precondition failure lost context: %s", card)
	}
}

func installFakeSystemctl(t *testing.T, home string) (string, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("isolated systemd adapter fixture")
	}
	dir := filepath.Join(home, "fixture-bin")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	logPath, enabled, fail := filepath.Join(home, "systemctl.log"), filepath.Join(home, "enabled"), filepath.Join(home, "fail")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$SYSTEM_MENU_FIXTURE_LOG"
if [ -f "$SYSTEM_MENU_FIXTURE_FAIL" ]; then echo 'fixture user service unavailable'; exit 7; fi
if [ "$1" = '--user' ]; then shift; fi
case "$1" in
  daemon-reload) exit 0 ;;
  enable) : > "$SYSTEM_MENU_FIXTURE_ENABLED" ;;
  disable) rm -f "$SYSTEM_MENU_FIXTURE_ENABLED" ;;
  is-enabled) if [ -f "$SYSTEM_MENU_FIXTURE_ENABLED" ]; then echo enabled; else echo disabled; exit 1; fi ;;
  *) echo "unexpected service lifecycle operation: $*"; exit 17 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SYSTEM_MENU_FIXTURE_LOG", logPath)
	t.Setenv("SYSTEM_MENU_FIXTURE_ENABLED", enabled)
	t.Setenv("SYSTEM_MENU_FIXTURE_FAIL", fail)
	return logPath, fail
}

func TestSystemAutostartMenuRealAdapterDetectEnableDisableAndFailure(t *testing.T) {
	a, source, home := systemMenuFixture(t)
	logPath, fail := installFakeSystemctl(t, home)
	// The shipped menu route must parse the actual read command, not an invented suffix.
	id, err := a.handleTeamPrivateViewOperation(t.Context(), source, workstore.UIView{}, "private.admin-autostart", nil)
	if err != nil {
		t.Fatal(err)
	}
	if card := systemCard(t, a, source.ScopeID, id); !strings.Contains(card, "启用自动启动") {
		t.Fatalf("autostart read did not open controls: %s", card)
	}
	on := systemClick(t, a, source, id, "/admin autostart on")
	status, err := install.DetectAutostart(a.installStatePath())
	if err != nil || !status.Enabled {
		t.Fatalf("real isolated adapter did not enable: %+v %v", status, err)
	}
	if card := systemCard(t, a, source.ScopeID, on); !strings.Contains(card, "已启用") || !strings.Contains(card, "关闭自动启动") {
		t.Fatal("enable result lost effective state or next operation")
	}
	off := systemClick(t, a, source, on, "/admin autostart off")
	status, err = install.DetectAutostart(a.installStatePath())
	if err != nil || status.Enabled {
		t.Fatalf("real isolated adapter did not disable: %+v %v", status, err)
	}
	// The actual OS adapter reports failure; the same settings page preserves a concrete cause and refresh.
	if err = os.WriteFile(fail, []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}
	failed := systemClick(t, a, source, off, "/admin autostart")
	card := systemCard(t, a, source.ScopeID, failed)
	if !strings.Contains(card, "fixture user service unavailable") || !strings.Contains(card, "刷新状态") || !strings.Contains(card, "返回系统管理") {
		t.Fatalf("failure became a generic dead end: %s", card)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(log), "\n") {
		if strings.Contains(line, " restart ") || strings.Contains(line, " start ") || strings.Contains(line, " stop ") {
			t.Fatalf("autostart fixture unexpectedly managed a live service: %s", line)
		}
	}
}

func TestSystemUpgradeAndManagementLinkExposeOperations(t *testing.T) {
	a, source, _ := systemMenuFixture(t)
	id, err := a.teamUpgradeStatus(t.Context(), source, control.Action{Text: "/upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	card := systemCard(t, a, source.ScopeID, id)
	if !strings.Contains(card, "/upgrade latest") || !strings.Contains(card, "/upgrade track") {
		t.Fatalf("upgrade became read-only: %s", card)
	}
	source.Key = "management-link"
	link, err := a.handleTeamAdminCommand(t.Context(), source, control.Action{Text: "/admin web"})
	if err != nil {
		t.Fatal(err)
	}
	card = systemCard(t, a, source.ScopeID, link)
	if !strings.Contains(card, "open_url") || !strings.Contains(card, ":9501/admin/") {
		t.Fatalf("management link has no actual open action: %s", card)
	}
}
