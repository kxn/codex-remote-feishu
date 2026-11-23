package privatechat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/acp"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"path/filepath"
	"testing"
)

func TestNativeOpenCodeStreamRequiresDurablePromptFrame(t *testing.T) {
	ctx := t.Context()
	j, e := OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	surface := Owner{"g", "s", "c", "u", "tenant", "app"}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendOpenCode, HistoryNamespace: "history"}
	lease, e := j.OpenNativeLease(ctx, testNativeAuthority{}, owner, surface, 1)
	if e != nil {
		t.Fatal(e)
	}
	seq, _, e := j.Admit(ctx, Ingress{Owner: surface, Generation: 1, EventID: "message", Lane: "input", Payload: []byte(`{"text":"hi"}`), Sources: []string{"message"}})
	if e != nil {
		t.Fatal(e)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "execution"}
	cmd := agentproto.Command{CommandID: "prompt", Kind: agentproto.CommandPromptSend, NativeBinding: &binding, Target: agentproto.Target{CWD: "/work", ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hi"}}}}
	dispatch := Dispatch{ID: "dispatch", Lane: "input", Mode: "start", Owner: surface, Generation: 1, Session: NativeSession{Backend: "opencode", Namespace: "history", CarrierID: "inc", CarrierGeneration: 1}, CommandID: "prompt", InputSequences: []int64{seq}}
	if e = lease.prepareNativeDispatch(ctx, dispatch, cmd); e != nil {
		t.Fatal(e)
	}
	if e = lease.MarkSending(ctx, dispatch.ID); e != nil {
		t.Fatal(e)
	}
	tr := acp.NewTranslator("instance", "/work")
	out, e := tr.TranslateCommand(cmd)
	if e != nil {
		t.Fatal(e)
	}
	var first struct {
		ID string `json:"id"`
	}
	if e = json.Unmarshal(out.OutboundToChild[0], &first); e != nil {
		t.Fatal(e)
	}
	ready, e := tr.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"sessionId":"session"}}`, first.ID)))
	if e != nil || len(ready.OutboundToChild) != 1 {
		t.Fatalf("ready %v", e)
	}
	var prompt struct {
		ID string `json:"id"`
	}
	if e = json.Unmarshal(ready.OutboundToChild[0], &prompt); e != nil {
		t.Fatal(e)
	}
	observed, e := tr.ObserveServer([]byte(`{"method":"session/update","params":{"sessionId":"session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}}}}`))
	if e != nil || len(observed.Events) == 0 {
		t.Fatalf("update %v", e)
	}
	event := observed.Events[0]
	event.NativeBinding = &binding
	event.Seq = 1
	hash, e := agentproto.RuntimeEventPayloadHash(event)
	if e != nil {
		t.Fatal(e)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "inc", Sequence: 1, PayloadHash: hash}
	if _, e = lease.ReceiveNativeEvent(ctx, event); e != nil {
		t.Fatal(e)
	}
	if state, _ := j.DispatchState(ctx, dispatch.ID); state != "sending" {
		t.Fatalf("accepted before frame: %s", state)
	}
	raw, _ := json.Marshal(cmd)
	sum := sha256.Sum256(raw)
	receipt := agentproto.RuntimeCommandReceipt{Version: 1, CarrierIncarnation: "inc", CommandID: cmd.CommandID, PayloadHash: hex.EncodeToString(sum[:]), PreparedFrames: 1, Frames: []agentproto.RuntimeFrameReceipt{{FrameID: 1, NativeRequestID: prompt.ID, NativeMethod: "session/prompt", Stage: "unknown"}}}
	if e = lease.recordCommandReceipt(ctx, receipt); e != nil {
		t.Fatal(e)
	}
	if e = lease.ResolveNativeDispatch(ctx, NativeEventRef{"inc", 1, hash}); e != nil {
		t.Fatal(e)
	}
	if state, _ := j.DispatchState(ctx, dispatch.ID); state != "accepted" {
		t.Fatalf("not accepted: %s", state)
	}
}

func TestInternalOpenCodeCatalogSessionRequiresAcceptedExactFrame(t *testing.T) {
	ctx := t.Context()
	j, e := OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	surface := Owner{"g", "s", "c", "u", "tenant", "app"}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendOpenCode, HistoryNamespace: "history"}
	lease, e := j.OpenNativeLease(ctx, testNativeAuthority{}, owner, surface, 1)
	if e != nil {
		t.Fatal(e)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner}
	cmd := agentproto.Command{CommandID: "models", Kind: agentproto.CommandModelList, NativeBinding: &binding, Target: agentproto.Target{CWD: "/work"}}
	dispatch := Dispatch{ID: "dispatch", Lane: "inspect", Mode: "start", Owner: surface, Generation: 1, Session: NativeSession{Backend: "opencode", Namespace: "history", CarrierID: "inc", CarrierGeneration: 1}, CommandID: cmd.CommandID}
	if e = lease.prepareNativeDispatch(ctx, dispatch, cmd); e != nil {
		t.Fatal(e)
	}
	if e = lease.MarkSending(ctx, dispatch.ID); e != nil {
		t.Fatal(e)
	}
	tr := acp.NewTranslator("instance", "/work")
	out, e := tr.TranslateCommand(cmd)
	if e != nil {
		t.Fatal(e)
	}
	var frame struct {
		ID string `json:"id"`
	}
	if e = json.Unmarshal(out.OutboundToChild[0], &frame); e != nil {
		t.Fatal(e)
	}
	observed, e := tr.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"sessionId":"helper"}}`, frame.ID)))
	if e != nil || len(observed.Events) != 1 {
		t.Fatal(e)
	}
	event := observed.Events[0]
	event.NativeBinding = &binding
	event.Seq = 1
	hash, e := agentproto.RuntimeEventPayloadHash(event)
	if e != nil {
		t.Fatal(e)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "inc", Sequence: 1, PayloadHash: hash}
	if _, e = lease.ReceiveNativeEvent(ctx, event); e != nil {
		t.Fatal(e)
	}
	if yes, e := j.IsInternalNativeSession(ctx, "opencode", "history", "helper"); e != nil || yes {
		t.Fatal("unverified helper treated as fact")
	}
	raw, _ := json.Marshal(cmd)
	sum := sha256.Sum256(raw)
	receipt := agentproto.RuntimeCommandReceipt{Version: 1, CarrierIncarnation: "inc", CommandID: cmd.CommandID, PayloadHash: hex.EncodeToString(sum[:]), PreparedFrames: 1, Frames: []agentproto.RuntimeFrameReceipt{{FrameID: 1, NativeRequestID: frame.ID, NativeMethod: "session/new", Stage: "written"}}}
	if e = lease.recordCommandReceipt(ctx, receipt); e != nil {
		t.Fatal(e)
	}
	if e = lease.ResolveNativeDispatch(ctx, NativeEventRef{"inc", 1, hash}); e != nil {
		t.Fatal(e)
	}
	if yes, e := j.IsInternalNativeSession(ctx, "opencode", "history", "helper"); e != nil || !yes {
		t.Fatalf("missing verified helper: %v %v", yes, e)
	}
	if yes, e := j.IsInternalNativeSession(ctx, "opencode", "other", "helper"); e != nil || yes {
		t.Fatal("namespace crossed")
	}
}
