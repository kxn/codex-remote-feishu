package privatechat

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateGoalNativeResultAfterReceiverAckAndRestart(t *testing.T) {
	testPrivateGoalNativeResultAfterRestart(t, false)
}

type deniedReadAuthority struct{ testNativeAuthority }

func (deniedReadAuthority) WithNativeReadProjection(ctx context.Context, _ agentproto.NativeOwner, _ Owner, _ int64, _, _ string, fn func(bool) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(false)
}

func TestPrivateGoalLateResultAfterForeignClaimShowsOnlyLocalFailure(t *testing.T) {
	testPrivateGoalNativeResultAfterRestart(t, true)
}

func testPrivateGoalNativeResultAfterRestart(t *testing.T, denied bool) {
	ctx := t.Context()
	root := t.TempDir()
	path := filepath.Join(root, "private.db")
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RegisterNativeHello(ctx, lease, nativeHello(native)); err != nil {
		t.Fatal(err)
	}
	inst := p.app.Service().Instance("instance")
	inst.Threads["thread"] = &state.ThreadRecord{ThreadID: "thread", CWD: "/tmp/workspace", Loaded: true}
	attach := privateAction(o, control.ActionAttachInstance)
	attach.InstanceID = "instance"
	if _, err = p.app.Action(attach); err != nil {
		t.Fatal(err)
	}
	p.app.Service().Surface(o.SurfaceID).SelectedThreadID = "thread"
	a := privateAction(o, control.ActionGoalCommand)
	a.Text = "/goal"
	seq := admittedAction(t, j, o, "goal", "control", a)
	effects, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, effect := range effects {
		event, e := DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandThreadGoalGet {
			id = effect.Effect.ID
		}
	}
	if id == "" {
		for _, effect := range effects {
			event, _ := DecodeEffectEvent(effect.Effect.Body)
			t.Logf("goal event: notice=%+v page=%+v", event.Notice, event.PageView)
		}
		t.Fatal("goal command missing")
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: native}
	dispatch, command, err := lease.PrepareEffectDispatch(ctx, id, binding)
	if err != nil {
		t.Fatal(err)
	}
	if err = lease.MarkSending(ctx, dispatch.ID); err != nil {
		t.Fatal(err)
	}
	translator := codex.NewTranslator("instance")
	frames, err := translator.TranslateCommand(command)
	if err != nil || len(frames) != 1 {
		t.Fatalf("translate: %v", err)
	}
	var frame struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(frames[0], &frame); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: native, CWD: cwd, JournalPath: filepath.Join(root, "runtime.db")}
	wrapper, err := runtimejournal.Open(ctx, snapshot.JournalPath, root, cwd, "inc")
	if err != nil {
		t.Fatal(err)
	}
	defer wrapper.Close()
	if err = wrapper.BindPrivateLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, _, err = wrapper.PreparePrivateCommand(command); err != nil {
		t.Fatal(err)
	}
	frameID, err := wrapper.PrepareFrame(command.CommandID, frames[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = wrapper.StartFrame(frameID, frames[0]); err != nil {
		t.Fatal(err)
	}
	if err = lease.RecoverNativeCommandReceipt(ctx, testJournalReader{snapshot, root}, command.CommandID); err != nil {
		t.Fatal(err)
	}
	result, err := translator.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"goal":null}}`, frame.ID)))
	if err != nil || len(result.Events) != 1 {
		t.Fatalf("goal response: %#v %v", result.Events, err)
	}
	ref := persistNative(t, ctx, lease, binding, 1, result.Events[0])
	if got, err := j.DispatchState(ctx, dispatch.ID); err != nil || got != "accepted" {
		t.Fatalf("dispatch did not settle: %q %v", got, err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	var authority NativeLeaseAuthority = testNativeAuthority{}
	if denied {
		authority = deniedReadAuthority{}
	}
	lease, err = j.OpenNativeLease(ctx, authority, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RegisterNativeHello(ctx, lease, nativeHello(native)); err != nil {
		t.Fatal(err)
	}
	projected, err := p.ApplyNative(ctx, lease, ref, CoreNativeReducer{})
	if err != nil {
		t.Fatal(err)
	}
	var page bool
	var content strings.Builder
	for _, effect := range projected {
		event, e := DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.PageView != nil {
			page = true
		}
		encoded, _ := json.Marshal(event)
		content.Write(encoded)
	}
	if !page {
		t.Fatalf("goal result did not produce a page after restart: %#v", projected)
	}
	if denied && !strings.Contains(content.String(), "该会话已由其他载体接管") {
		t.Fatalf("late result missing ownership failure: %s", content.String())
	}
}
