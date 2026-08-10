package daemon

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishufacts"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishuroomstate"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type primaryBootstrapGateway struct {
	recordingGateway
	chatInfo         feishu.ChatInfo
	chatInfoErr      error
	chatInfoCalls    int
	chatInfoRequests []feishu.ChatInfoRequest
	mu               sync.Mutex
	chatInfoGate     <-chan struct{}
	chatInfoSeen     chan<- struct{}
}

func (g *primaryBootstrapGateway) ReadChatInfo(_ context.Context, req feishu.ChatInfoRequest) (feishu.ChatInfo, error) {
	g.mu.Lock()
	g.chatInfoCalls++
	g.chatInfoRequests = append(g.chatInfoRequests, req)
	if g.chatInfoSeen != nil {
		select {
		case g.chatInfoSeen <- struct{}{}:
		default:
		}
	}
	g.mu.Unlock()
	if g.chatInfoGate != nil {
		<-g.chatInfoGate
	}
	if req.ChatID != "oc_room" {
		return feishu.ChatInfo{}, nil
	}
	return g.chatInfo, g.chatInfoErr
}

func (g *primaryBootstrapGateway) Apply(_ context.Context, operations []feishu.Operation) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.operations = append(g.operations, operations...)
	return nil
}

func TestFeishuBotAddedAutoPrimarySetsEmptyRoomWhenOnlyBot(t *testing.T) {
	gateway := &primaryBootstrapGateway{chatInfo: feishu.ChatInfo{BotCount: 1, ChatMode: "group"}}
	app := newPrimaryBootstrapTestApp(t, gateway)

	app.HandleGatewayAction(context.Background(), botAddedAction("app-1", "oc_room"))

	records := app.service.FeishuRoomState()
	if len(records) != 1 || records[0].PrimaryGatewayID != "app-1" || records[0].PrimaryUpdatedBy != "ou_operator" {
		t.Fatalf("room state = %#v, want auto primary app-1", records)
	}
	if gateway.chatInfoCalls != 1 {
		t.Fatalf("chat info calls = %d, want 1", gateway.chatInfoCalls)
	}
	if len(gateway.chatInfoRequests) != 1 || gateway.chatInfoRequests[0].GatewayID != "app-1" || gateway.chatInfoRequests[0].ChatID != "oc_room" {
		t.Fatalf("chat info requests = %#v, want gateway-scoped oc_room read", gateway.chatInfoRequests)
	}
	if len(gateway.operations) != 1 || gateway.operations[0].Kind != feishu.OperationSendText {
		t.Fatalf("operations = %#v, want one text notice", gateway.operations)
	}
	if !strings.Contains(gateway.operations[0].Text, "已自动成为本群主机器人") {
		t.Fatalf("notice text = %q", gateway.operations[0].Text)
	}
	if got := app.feishuPrimaryGatewayForChat("oc_room"); got != "app-1" {
		t.Fatalf("primary snapshot = %q, want app-1", got)
	}
}

func TestFeishuBotAddedAutoPrimaryDoesNotOverwriteExistingPrimary(t *testing.T) {
	gateway := &primaryBootstrapGateway{chatInfo: feishu.ChatInfo{BotCount: 1, ChatMode: "group"}}
	app := newPrimaryBootstrapTestApp(t, gateway)
	app.service.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{
		{RoomID: "feishu:chat:oc_room", ChatID: "oc_room", PrimaryGatewayID: "app-existing"},
	})
	app.syncFeishuRoomStateLocked()

	app.HandleGatewayAction(context.Background(), botAddedAction("app-1", "oc_room"))

	records := app.service.FeishuRoomState()
	if len(records) != 1 || records[0].PrimaryGatewayID != "app-existing" {
		t.Fatalf("room state = %#v, want existing primary preserved", records)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("unexpected operations when primary exists: %#v", gateway.operations)
	}
}

func TestFeishuBotAddedAutoPrimarySkipsWhenMultipleBots(t *testing.T) {
	gateway := &primaryBootstrapGateway{chatInfo: feishu.ChatInfo{BotCount: 2, ChatMode: "group"}}
	app := newPrimaryBootstrapTestApp(t, gateway)

	app.HandleGatewayAction(context.Background(), botAddedAction("app-1", "oc_room"))

	if records := app.service.FeishuRoomState(); len(records) != 0 {
		t.Fatalf("room state = %#v, want no primary", records)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("unexpected operations for non-unique bot: %#v", gateway.operations)
	}
}

func TestFeishuBotAddedAutoPrimarySkipsNonGroupChat(t *testing.T) {
	gateway := &primaryBootstrapGateway{chatInfo: feishu.ChatInfo{BotCount: 1, ChatMode: "p2p"}}
	app := newPrimaryBootstrapTestApp(t, gateway)

	app.HandleGatewayAction(context.Background(), botAddedAction("app-1", "oc_room"))

	if records := app.service.FeishuRoomState(); len(records) != 0 {
		t.Fatalf("room state = %#v, want no primary outside group chat", records)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("unexpected operations for non-group chat: %#v", gateway.operations)
	}
}

func TestFeishuBotAddedAutoPrimaryUsesFreshFactsBeforeChatInfo(t *testing.T) {
	gateway := &primaryBootstrapGateway{chatInfo: feishu.ChatInfo{BotCount: 1, ChatMode: "group"}}
	app := newPrimaryBootstrapTestApp(t, gateway)
	seedPrimaryBootstrapFacts(t, app, "app-1", nil)

	app.HandleGatewayAction(context.Background(), botAddedAction("app-1", "oc_room"))

	if gateway.chatInfoCalls != 0 {
		t.Fatalf("chat info should not run without chat scope, calls=%d", gateway.chatInfoCalls)
	}
	if records := app.service.FeishuRoomState(); len(records) != 0 {
		t.Fatalf("room state = %#v, want no primary without chat scope", records)
	}
	if len(gateway.operations) != 1 || !strings.Contains(gateway.operations[0].Text, "获取群组信息") {
		t.Fatalf("operations = %#v, want permission notice", gateway.operations)
	}
}

func TestFeishuBotAddedAutoPrimaryConcurrentEventsOnlySetOnce(t *testing.T) {
	seen := make(chan struct{}, 2)
	gate := make(chan struct{})
	gateway := &primaryBootstrapGateway{
		chatInfo:     feishu.ChatInfo{BotCount: 1, ChatMode: "group"},
		chatInfoGate: gate,
		chatInfoSeen: seen,
	}
	app := newPrimaryBootstrapTestApp(t, gateway)

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			app.HandleGatewayAction(context.Background(), botAddedAction("app-1", "oc_room"))
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-seen:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for concurrent chat info calls")
		}
	}
	close(gate)
	wg.Wait()

	records := app.service.FeishuRoomState()
	if len(records) != 1 || records[0].PrimaryGatewayID != "app-1" {
		t.Fatalf("room state = %#v, want one primary", records)
	}
	successNotices := 0
	for _, op := range gateway.operations {
		if op.Kind == feishu.OperationSendText && strings.Contains(op.Text, "已自动成为本群主机器人") {
			successNotices++
		}
	}
	if successNotices != 1 {
		t.Fatalf("success notices = %d operations=%#v, want one", successNotices, gateway.operations)
	}
}

func TestFeishuBotAddedAutoPrimaryRecordsChatInfoPermissionGap(t *testing.T) {
	gateway := &primaryBootstrapGateway{chatInfoErr: &feishu.APIError{
		API:  "im.v1.chat.get",
		Code: 99990001,
		Msg:  "permission denied",
		PermissionViolations: []feishu.APIErrorPermissionViolation{
			{Type: "tenant", Subject: "im:chat:readonly"},
		},
		RequestID: "req-chat-get-1",
	}}
	app := newPrimaryBootstrapTestApp(t, gateway)

	app.HandleGatewayAction(context.Background(), botAddedAction("app-1", "oc_room"))

	gaps := app.snapshotFeishuPermissionGaps("app-1")
	if len(gaps) != 1 || gaps[0].Scope != "im:chat:readonly" || gaps[0].SourceAPI != "im.v1.chat.get" {
		t.Fatalf("permission gaps = %#v, want chat.get im:chat:readonly", gaps)
	}
	if len(gateway.operations) != 1 || !strings.Contains(gateway.operations[0].Text, "获取群组信息") {
		t.Fatalf("operations = %#v, want permission notice", gateway.operations)
	}
}

func TestFeishuBotAddedAutoPrimaryRollsBackWhenPersistFails(t *testing.T) {
	gateway := &primaryBootstrapGateway{chatInfo: feishu.ChatInfo{BotCount: 1, ChatMode: "group"}}
	app := newPrimaryBootstrapTestApp(t, gateway)
	app.feishuRoomState.store = feishuroomstate.NewStore(t.TempDir())
	app.feishuRoomState.status = persistedStoreStatusWritable

	app.HandleGatewayAction(context.Background(), botAddedAction("app-1", "oc_room"))

	if records := app.service.FeishuRoomState(); len(records) != 0 {
		t.Fatalf("room state = %#v, want rollback after persist failure", records)
	}
	if got := app.feishuPrimaryGatewayForChat("oc_room"); got != "" {
		t.Fatalf("primary snapshot = %q, want rollback after persist failure", got)
	}
	for _, op := range gateway.operations {
		if strings.Contains(op.Text, "已自动成为本群主机器人") {
			t.Fatalf("unexpected success notice after persist failure: %#v", gateway.operations)
		}
	}
}

func newPrimaryBootstrapTestApp(t *testing.T, gateway *primaryBootstrapGateway) *App {
	t.Helper()
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.configureFeishuFactsStateLocked(t.TempDir())
	seedPrimaryBootstrapFacts(t, app, "app-1", []feishufacts.ScopeStatus{
		{ScopeName: "im:chat:readonly", ScopeType: "tenant", GrantStatus: 1},
	})
	return app
}

func seedPrimaryBootstrapFacts(t *testing.T, app *App, gatewayID string, scopes []feishufacts.ScopeStatus) {
	t.Helper()
	now := time.Now().UTC()
	if err := app.feishuFactsState.store.Put(feishufacts.Record{
		GatewayID:       gatewayID,
		AppID:           "cli_test",
		Scopes:          scopes,
		FetchedAt:       now,
		ScopesFetchedAt: now,
	}); err != nil {
		t.Fatalf("seed facts: %v", err)
	}
}

func botAddedAction(gatewayID, chatID string) control.Action {
	return control.Action{
		Kind:             control.ActionFeishuBotAddedToGroup,
		GatewayID:        gatewayID,
		SurfaceSessionID: "feishu:" + gatewayID + ":chat:" + chatID,
		ChatID:           chatID,
		ActorUserID:      "ou_operator",
	}
}
