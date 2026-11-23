package daemon

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type privateWorktreeFixture struct {
	privateHostReducer
	root string
}

func (r privateWorktreeFixture) Reduce(app *privatechat.App, a control.Action) ([]eventcontract.Event, error) {
	if a.Kind == control.ActionWorkspaceNewWorktree {
		v := app.Service().Surface(a.SurfaceSessionID)
		v.ClaimedWorkspaceKey = r.root
		v.PreparedThreadCWD = r.root
		v.RouteMode = state.RouteModeNewThreadReady
	}
	return r.privateHostReducer.Reduce(app, a)
}

func TestPrivateWorktreeObservationUsesRealGitAfterCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	command := exec.Command("git", "init", "-q", repo)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", output, err)
	}
	ctx := context.Background()
	host := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{GitAvailable: true})
	host.service = app.Service()
	o := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "tenant", AppID: "bot"}
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateWorktreeFixture{privateHostReducer{host}, repo})
	if err != nil {
		t.Fatal(err)
	}
	a := control.Action{Kind: control.ActionWorkspaceNewWorktree, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
	raw, _ := json.Marshal(a)
	seq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "worktree", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, e := range effects {
		event, e2 := privatechat.DecodeEffectEvent(e.Effect.Body)
		if e2 != nil {
			t.Fatal(e2)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateWorktreeObserve {
			id = e.Effect.ID
		}
	}
	if id == "" {
		t.Fatal("observation was not committed")
	}
	follow, err := runPrivateWorktreeObservation(ctx, host, p, j, o, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range follow {
		event, e2 := privatechat.DecodeEffectEvent(e.Effect.Body)
		if e2 != nil {
			t.Fatal(e2)
		}
		if event.TargetPickerView != nil && event.TargetPickerView.Page == control.FeishuTargetPickerPageWorktree {
			found = true
		}
	}
	if !found {
		t.Fatal("real worktree picker missing")
	}
	if _, err = runPrivateWorktreeObservation(ctx, host, p, j, o, 1, id); err != nil {
		t.Fatalf("observation replay: %v", err)
	}
}
