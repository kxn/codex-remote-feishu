package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

// Starts with an unbound message, traverses visible navigation, then executes
// after every slash page. No carrier, receipt, or checkpoint primes the route.
func TestPrivateSDKUserGraphMenuEntrancesAndContinuation(t *testing.T) {
	a, b := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	for _, cwd := range []string{a, b} {
		if err := os.MkdirAll(cwd, 0700); err != nil {
			t.Fatal(err)
		}
	}
	g := newGraphEnv(t, a, b)
	u := g.user
	before := len(u.gateway.snapshotOperations())
	u.send(g.app, "menu-first-hi", "hi")
	u.waitVisible(g.app, before, "/list")
	before = len(u.gateway.snapshotOperations())
	if err := g.app.applications.hook.ReceiveMenu(u.ctx, platformingress.MenuIngressEvent{
		GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "platform-menu",
		SenderIDs: platformingress.SenderIDs{OpenID: "user"}, EventKey: "menu", ReceivedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	menu := graphWaitMenuCard(t, g, before, "菜单")
	var doc any
	raw, _ := feishu.FreezePrivateOperation(menu, g.app.daemonLifecycleID)
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	navigationStart := len(u.gateway.snapshotOperations())
	for i, page := range []struct{ arg, action, title string }{
		{"current_work", "", "基本命令"}, {"send_settings", "", "参数设置"},
		{"switch_target", "surface.command.workspace", "工作区"}, {"common_tools", "", "常用工具"},
		{"maintenance", "surface.command.admin", "系统管理"},
	} {
		value := assembledMenuValue(doc, page.arg)
		if page.action != "" {
			value = assembledActionKind(doc, page.action)
		}
		if value == nil {
			t.Fatalf("missing menu page %s", page.arg)
		}
		click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant",
			EventID: fmt.Sprintf("menu-nav-%d", i), SenderIDs: platformingress.SenderIDs{OpenID: "user"},
			ChatID: "private-chat", MessageID: menu.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()}
		response, err := g.app.applications.hook.ReceiveInteractionResponse(u.ctx, click)
		if err != nil || !strings.Contains(string(response.RawBody), page.title) {
			t.Fatalf("menu %s did not replace original card: %s %v", page.arg, response.RawBody, err)
		}
		var child any
		if err = json.Unmarshal(response.RawBody, &child); err != nil {
			t.Fatal(err)
		}
		back := assembledMenuBack(child)
		if back == nil {
			t.Fatalf("%s missing breadcrumb", page.arg)
		}
		click.EventID, click.Values = fmt.Sprintf("menu-back-%d", i), back
		response, err = g.app.applications.hook.ReceiveInteractionResponse(u.ctx, click)
		if err != nil || json.Unmarshal(response.RawBody, &doc) != nil {
			t.Fatalf("%s did not return to menu: %s %v", page.arg, response.RawBody, err)
		}
		// A fresh event carrying the previous page token cannot act again.
		click.EventID, click.Values = fmt.Sprintf("menu-stale-%d", i), value
		stale, err := g.app.applications.hook.ReceiveInteractionResponse(u.ctx, click)
		if err != nil || (!strings.Contains(string(stale.RawBody), "过期") && !strings.Contains(string(stale.RawBody), "更新")) {
			t.Fatalf("stale %s remained actionable: %s %v", page.arg, stale.RawBody, err)
		}
	}
	for _, op := range u.gateway.snapshotOperations()[navigationStart:] {
		if op.Kind == feishu.OperationSendCard {
			t.Fatalf("menu navigation appended another card instead of replacing %s: %s", menu.MessageID, op.CardTitle)
		}
	}
	u.chooseNew(g.app, "menu-choose-a")
	pages := []struct{ command, title string }{
		{"/menu", "菜单"}, {"/menu current_work", "基本命令"}, {"/menu send_settings", "参数设置"},
		{"/menu switch_target", "工作区"}, {"/menu common_tools", "常用工具"}, {"/menu maintenance", "系统管理"},
		{"/status", "当前状态"}, {"/help", "帮助"},
		{"/use", "会话"}, {"/threads", "会话"}, {"/sessions", "会话"},
		{"/useall", "会话"}, {"/sessionsall", "会话"}, {"/sessions/all", "会话"},
	}
	for i, page := range pages {
		before = len(u.gateway.snapshotOperations())
		u.send(g.app, fmt.Sprintf("slash-page-%d", i), page.command)
		u.waitVisible(g.app, before, page.title)
		before = len(u.gateway.snapshotOperations())
		u.send(g.app, fmt.Sprintf("after-page-%d", i), fmt.Sprintf("continue after %s", page.command))
		u.waitFinal(g.app, before, fmt.Sprintf("after-page-%d", i), "done-graph-a")
	}
	trace, err := os.ReadFile(g.trace)
	if err != nil {
		t.Fatal(err)
	}
	// Check every continuation only after the human sequence. In particular,
	// an old final update cannot pass a later message without native dispatch.
	for _, page := range pages {
		command := page.command
		want := fmt.Sprintf(`"text":%q`, "continue after "+command)
		if strings.Count(string(trace), want) != 1 {
			t.Errorf("continuation %s did not reach native exactly once", command)
		}
	}
}

func graphWaitMenuCard(t *testing.T, g graphEnv, before int, text string) feishu.Operation {
	t.Helper()
	g.user.waitVisible(g.app, before, text)
	for _, op := range g.user.gateway.snapshotOperations()[before:] {
		raw, _ := feishu.FreezePrivateOperation(op, g.app.daemonLifecycleID)
		if op.Kind == feishu.OperationSendCard && op.MessageID != "" && strings.Contains(string(raw), text) {
			return op
		}
	}
	t.Fatalf("visible %q was not a card", text)
	return feishu.Operation{}
}
