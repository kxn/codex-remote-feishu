package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func TestPrivateSDKUserGraphSettingsThenBusyAndNextTurn(t *testing.T) {
	a := filepath.Join(t.TempDir(), "a")
	b := filepath.Join(t.TempDir(), "b")
	if e := os.MkdirAll(a, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(b, 0700); e != nil {
		t.Fatal(e)
	}
	g := newGraphEnv(t, a, b)
	u := g.user
	before := len(u.gateway.snapshotOperations())
	u.send(g.app, "first-hi", "hi")
	graphSettingsWait(t, g, before, "/list")
	for _, step := range []struct{ id, text, visible string }{{"set-model", "/model fixture-model", "已更新飞书临时模型覆盖"}, {"set-reasoning", "/reasoning high", "已更新飞书临时推理强度覆盖"}, {"set-access", "/access full", "已更新当前会话的执行权限模式"}, {"set-plan", "/plan on", "Plan mode"}, {"set-verbose", "/verbose normal", "前端详细程度"}} {
		before = len(u.gateway.snapshotOperations())
		u.send(g.app, step.id, step.text)
		graphSettingsWait(t, g, before, step.visible)
	}
	u.chooseNew(g.app, "settings-a")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "settings-first", "settings first")
	u.waitFinal(g.app, before, "settings-first", "done-graph-a")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "settings-busy", "graph-busy")
	graphSettingsWait(t, g, before, `"EmojiType":"THINKING"`)
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "busy-menu", "/menu")
	graphSettingsWait(t, g, before, "菜单")
	var menu feishu.Operation
	var menuDoc any
	for _, op := range u.gateway.snapshotOperations()[before:] {
		raw, _ := feishu.FreezePrivateOperation(op, g.app.daemonLifecycleID)
		var doc any
		if op.Kind == feishu.OperationSendCard && json.Unmarshal(raw, &doc) == nil && assembledMenuValue(doc, "send_settings") != nil {
			menu, menuDoc = op, doc
		}
	}
	if menu.MessageID == "" {
		t.Fatal("working menu card not visible")
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "busy-settings-page", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: menu.MessageID, Kind: "card", Values: assembledMenuValue(menuDoc, "send_settings"), ReceivedAt: time.Now()}
	response, err := g.app.applications.hook.ReceiveInteractionResponse(u.ctx, click)
	if err != nil || !strings.Contains(string(response.RawBody), "参数设置") {
		t.Fatalf("open settings during turn: %s %v", response.RawBody, err)
	}
	if err = json.Unmarshal(response.RawBody, &menuDoc); err != nil {
		t.Fatal(err)
	}
	click.EventID, click.Values = "busy-menu-back", assembledMenuBack(menuDoc)
	if click.Values == nil {
		t.Fatal("working settings breadcrumb missing")
	}
	response, err = g.app.applications.hook.ReceiveInteractionResponse(u.ctx, click)
	if err != nil || len(response.RawBody) == 0 {
		t.Fatalf("return to working menu: %s %v", response.RawBody, err)
	}
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "busy-status", "/status")
	graphSettingsWait(t, g, before, "执行中")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "busy-verbose", "/verbose quiet")
	graphSettingsWait(t, g, before, "前端详细程度")
	before = len(u.gateway.snapshotOperations())
	if e := os.WriteFile(g.release, []byte("release"), 0600); e != nil {
		t.Fatal(e)
	}
	u.waitFinal(g.app, before, "settings-busy", "done-graph-a")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "settings-next", "after busy")
	u.waitFinal(g.app, before, "settings-next", "done-graph-a")
	trace, e := os.ReadFile(g.trace)
	if e != nil {
		t.Fatal(e)
	}
	var first, next string
	for _, line := range strings.Split(string(trace), "\n") {
		if strings.Contains(line, "settings first") {
			first = line
		}
		if strings.Contains(line, "after busy") {
			next = line
		}
	}
	for _, v := range []string{"fixture-model", "high", "dangerFullAccess", "\"mode\":\"plan\""} {
		if !strings.Contains(first, v) {
			t.Errorf("first native turn missing %q: %s", v, first)
		}
	}
	for _, v := range []string{"fixture-model", "high", "dangerFullAccess", `"mode":"plan"`} {
		if !strings.Contains(next, v) {
			t.Errorf("next native turn lost %q: %s", v, next)
		}
	}
}

func graphSettingsWait(t *testing.T, g graphEnv, before int, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, op := range g.user.gateway.snapshotOperations()[before:] {
			raw, _ := feishu.FreezePrivateOperation(op, g.app.daemonLifecycleID)
			if strings.Contains(string(raw), want) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, op := range g.user.gateway.snapshotOperations()[before:] {
		raw, _ := feishu.FreezePrivateOperation(op, g.app.daemonLifecycleID)
		t.Logf("settings operation %s", raw)
	}
	t.Fatalf("settings result %q not visible", want)
}
