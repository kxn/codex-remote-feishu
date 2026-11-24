package daemon

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestReconnectStageUsesExactConfirmedReceipt(t *testing.T) {
	action := control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface", MessageID: "message", ActorUserID: "user", Text: "hello", Files: []control.ActionFileAttachment{{SourceMessageID: "file", LocalPath: "/original", FileName: "ref.txt"}}}
	actionRaw, _ := json.Marshal(action)
	body, err := privatechat.EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindDaemonCommand, SurfaceSessionID: "surface", DaemonCommand: &control.DaemonCommand{Kind: control.DaemonCommandPrivateStageInput, SurfaceSessionID: "surface", InstanceID: "instance", ThreadCWD: "/workspace", Text: string(actionRaw)}})
	if err != nil {
		t.Fatal(err)
	}
	effect := privatechat.Effect{ID: "effect", Body: body}
	receipt := privateStageReceipt{Version: 1, EffectID: "effect", SurfaceID: "surface", Files: []privateStagedFile{{SourceMessageID: "file", Target: ".assets/ref.txt", Name: "ref.txt", SHA256: "sha"}}}
	files := []control.ActionFileAttachment{{SourceMessageID: "file", LocalPath: "/workspace/.assets/ref.txt", FileName: "ref.txt"}}
	pending := &state.PendingTextInputRecord{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: inboundFileAttachmentPrompt(files)}}}
	verify := func(want bool) {
		t.Helper()
		raw, _ := json.Marshal(receipt)
		got, err := (privateHostReducer{}).VerifyReconnectStage(action, effect, raw, pending)
		if err != nil || got != want {
			t.Fatalf("matched=%v want=%v err=%v", got, want, err)
		}
	}
	verify(true)
	receipt.Files[0].Target = "../escape"
	verify(false)
	receipt.Files[0].Target = ".assets/other"
	verify(false)
}

func TestReconnectStageInboundContractRetainsPlatformFacts(t *testing.T) {
	base := control.Action{Kind: control.ActionTextMessage, GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u", MessageID: "message", Text: "text", Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "body"}}, Files: []control.ActionFileAttachment{{SourceMessageID: "file", LocalPath: "/source", FileName: "name"}}, Inbound: &control.ActionInboundMeta{EventID: "event", EventType: "message", RequestID: "request", OpenMessageID: "reply"}}
	staged := base
	meta := *base.Inbound
	meta.LifecycleVerdict = control.InboundLifecycleCurrent
	staged.Inbound = &meta
	if !samePrivateStagedInbound(staged, base) {
		t.Fatal("classification projection should not change original facts")
	}
	for name, mutate := range map[string]func(*control.Action){
		"owner": func(a *control.Action) { a.ActorUserID = "other" }, "text": func(a *control.Action) { a.Text = "other" },
		"input": func(a *control.Action) { a.Inputs = []agentproto.Input{{Type: agentproto.InputText, Text: "other"}} },
		"file": func(a *control.Action) {
			a.Files = []control.ActionFileAttachment{{SourceMessageID: "other", LocalPath: "/source", FileName: "name"}}
		},
		"event": func(a *control.Action) { x := *a.Inbound; x.EventID = "other"; a.Inbound = &x },
		"reply": func(a *control.Action) { x := *a.Inbound; x.OpenMessageID = "other"; a.Inbound = &x },
		"verdict": func(a *control.Action) {
			x := *a.Inbound
			x.LifecycleVerdict = control.InboundLifecycleOld
			a.Inbound = &x
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := staged
			mutate(&candidate)
			if samePrivateStagedInbound(candidate, base) {
				t.Fatal("changed fact accepted")
			}
		})
	}
}
