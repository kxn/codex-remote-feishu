package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type privateDirectoryFixture struct {
	privateHostReducer
	root string
}

func (r privateDirectoryFixture) Reduce(app *privatechat.App, action control.Action) ([]eventcontract.Event, error) {
	if action.Kind == control.ActionWorkspaceNewDir {
		app.Service().Surface(action.SurfaceSessionID).ClaimedWorkspaceKey = r.root
	}
	return r.privateHostReducer.Reduce(app, action)
}

func TestPrivateDirectoryCrashRecoveryPreservesExistingContent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	host := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	o := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "tenant", AppID: "bot"}
	path := filepath.Join(t.TempDir(), "db")
	j, err := privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateDirectoryFixture{privateHostReducer{host}, root})
	if err != nil {
		t.Fatal(err)
	}
	base := control.Action{GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u"}
	counter := 0
	apply := func(a control.Action) []privatechat.EffectRecord {
		counter++
		raw, _ := json.Marshal(a)
		seq, _, e := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: fmt.Sprintf("event-%d", counter), Lane: "control", Payload: raw})
		if e != nil {
			t.Fatal(e)
		}
		result, e := p.ApplyIngress(ctx, o, 1, seq)
		if e != nil {
			t.Fatal(e)
		}
		return result
	}
	view := func(effects []privatechat.EffectRecord) (target, pathPicker string) {
		for _, e := range effects {
			event, err := privatechat.DecodeEffectEvent(e.Effect.Body)
			if err != nil {
				t.Fatal(err)
			}
			if event.TargetPickerView != nil {
				target = event.TargetPickerView.PickerID
			}
			if event.PathPickerView != nil {
				pathPicker = event.PathPickerView.PickerID
			}
		}
		return
	}
	base.Kind = control.ActionWorkspaceNewDir
	target, _ := view(apply(base))
	if target == "" {
		t.Fatal("no target picker")
	}
	base.Kind = control.ActionTargetPickerOpenPathPicker
	base.PickerID = target
	base.TargetPickerValue = control.FeishuTargetPickerPathFieldLocalDirectory
	_, picker := view(apply(base))
	if picker == "" {
		t.Fatal("no path picker")
	}
	base.Kind = control.ActionPathPickerConfirm
	base.PickerID = picker
	apply(base)
	base.Kind = control.ActionTargetPickerConfirm
	base.PickerID = target
	base.TargetPickerValue = ""
	base.RequestAnswers = map[string][]string{control.FeishuTargetPickerLocalDirectoryNameFieldName: {"child"}}
	apply(base)
	effects := apply(base)
	var id string
	findIntent := func(effects []privatechat.EffectRecord) {
		for _, e := range effects {
			event, err := privatechat.DecodeEffectEvent(e.Effect.Body)
			if err != nil {
				t.Fatal(err)
			}
			if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateMkdir {
				id = e.Effect.ID
			}
		}
	}
	findIntent(effects)
	if id == "" {
		base.RequestAnswers = nil
		effects = apply(base)
		findIntent(effects)
	}
	if id == "" {
		for _, e := range effects {
			event, _ := privatechat.DecodeEffectEvent(e.Effect.Body)
			if event.TargetPickerView != nil {
				t.Fatalf("no durable mkdir intent, picker checked=%t messages=%v", event.TargetPickerView.LocalDirectoryChecked, event.TargetPickerView.Messages)
			}
		}
		t.Fatal("no durable mkdir intent")
	}
	parent, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := privateDirectoryIdentity(parent)
	parent.Close()
	if err != nil {
		t.Fatal(err)
	}
	intent, _ := json.Marshal(privateMkdirIntent{Version: 1, Parent: root, Name: "child", ParentIdentity: identity})
	if err = j.PrepareLocalIntent(ctx, o, 1, id, intent); err != nil {
		t.Fatal(err)
	}
	if err = j.MarkEffectSending(ctx, o, 1, id); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "child")
	if err = os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "keep"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	app = privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	p, err = privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateDirectoryFixture{privateHostReducer{host}, root})
	if err != nil {
		t.Fatal(err)
	}
	saved := root + "-original"
	if err = os.Rename(root, saved); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = runPrivateMkdir(ctx, host, p, j, o, 1, id); err == nil {
		t.Fatal("replaced parent accepted")
	}
	if _, err = os.Stat(filepath.Join(root, "child")); !os.IsNotExist(err) {
		t.Fatal("created under replacement parent")
	}
	if err = os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(saved, root); err != nil {
		t.Fatal(err)
	}
	if _, err = runPrivateMkdir(ctx, host, p, j, o, 1, id); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "keep")); err != nil || string(b) != "data" {
		t.Fatalf("existing content changed: %s %v", b, err)
	}
	if _, err = runPrivateMkdir(ctx, host, p, j, o, 1, id); err != nil {
		t.Fatalf("receipt replay: %v", err)
	}
}
