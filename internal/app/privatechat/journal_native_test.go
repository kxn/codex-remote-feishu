package privatechat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type testNativeAuthority struct{}

func (testNativeAuthority) WithNativeReadProjection(ctx context.Context, _ agentproto.NativeOwner, _ Owner, _ int64, _, _ string, fn func(bool) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(true)
}

func (testNativeAuthority) WithNativeLease(ctx context.Context, owner agentproto.NativeOwner, _ Owner, _ int64, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := owner.Validate(); err != nil {
		return err
	}
	return fn()
}

type testJournalReader struct {
	snapshot agentproto.PrivateNativeLaunchSnapshot
	stateDir string
}

func (r testJournalReader) ReadNativeCommandReceipt(ctx context.Context, owner agentproto.NativeOwner, id string) (*agentproto.RuntimeCommandReceipt, error) {
	if owner != r.snapshot.Owner {
		return nil, ErrJournalConflict
	}
	return runtimejournal.ReadPrivateCommandReceipt(ctx, r.snapshot, r.stateDir, id)
}
func TestNativeCodexReplayCommitAndReceipt(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		name := "write_started"
		if unknown {
			name = "write_unknown"
		}
		t.Run(name, func(t *testing.T) { testNativeCodexReplay(t, unknown) })
	}
}
func testNativeCodexReplay(t *testing.T, unknown bool) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "private.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	surface := Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "opaque", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, owner, surface, 1)
	if err != nil {
		t.Fatal(err)
	}
	seq, _, err := j.Admit(ctx, Ingress{Owner: surface, Generation: 1, EventID: "message", Lane: "input", Payload: []byte(`{"text":"hi"}`), Sources: []string{"message"}})
	if err != nil {
		t.Fatal(err)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "private-turn"}
	command := agentproto.Command{CommandID: "start", Kind: agentproto.CommandPromptSend, NativeBinding: &binding, Target: agentproto.Target{CWD: "/work", ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hi"}}}}
	d := Dispatch{ID: "dispatch", Lane: "input", Mode: "start", Owner: surface, Generation: 1, Session: NativeSession{Backend: "codex", Namespace: "history", CarrierID: "inc", CarrierGeneration: 1}, CommandID: "start", InputSequences: []int64{seq}}
	if err = lease.prepareNativeDispatch(ctx, d, command); err != nil {
		t.Fatal(err)
	}
	if err = lease.MarkSending(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	translator := codex.NewTranslator("instance")
	frames, err := translator.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	var first struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(frames[0], &first); err != nil {
		t.Fatal(err)
	}
	observed, err := translator.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"thread":{"id":"session"},"model":"fixture-model"}}`, first.ID)))
	if err != nil || len(observed.OutboundToCodex) != 1 {
		t.Fatalf("thread response: %v", err)
	}
	turnFrame := append([]byte(nil), observed.OutboundToCodex[0]...)
	var second struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(observed.OutboundToCodex[0], &second); err != nil {
		t.Fatal(err)
	}
	observed, err = translator.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"turn":{"id":"turn"}}}`, second.ID)))
	if err != nil || len(observed.Events) != 1 {
		t.Fatalf("turn response: %v", err)
	}
	event := observed.Events[0]
	event.NativeBinding = &binding
	event.Seq = 1
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "inc", Sequence: 1, PayloadHash: hash}
	bad := event
	bad.NativeBinding = &agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "different"}
	if badReceipt, e := lease.ReceiveNativeEvent(ctx, bad); e == nil || badReceipt != nil {
		t.Fatal("forged event acknowledged")
	}
	ack, err := lease.ReceiveNativeEvent(ctx, event)
	if err != nil || ack == nil || ack.PayloadHash != hash {
		t.Fatalf("event receipt: %+v %v", ack, err)
	}
	state, err := j.DispatchState(ctx, d.ID)
	if err != nil || state != "sending" {
		t.Fatalf("accepted before frame proof: %s %v", state, err)
	}
	// Wrapper ACK was lost. Read its actual durable journal while its writer
	// remains open; child_write_started is uncertain transport, but the exact
	// native accepted event is stronger proof and does not require a resend.
	root := t.TempDir()
	cwd := t.TempDir()
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: filepath.Join(root, "wrapper.db")}
	wrapperJournal, e := runtimejournal.Open(ctx, snapshot.JournalPath, root, cwd, "inc")
	if e != nil {
		t.Fatal(e)
	}
	defer wrapperJournal.Close()
	if e = wrapperJournal.BindPrivateLaunchSnapshot(snapshot); e != nil {
		t.Fatal(e)
	}
	if _, _, e = wrapperJournal.PreparePrivateCommand(command); e != nil {
		t.Fatal(e)
	}
	frameID, e := wrapperJournal.PrepareFrame(command.CommandID, turnFrame)
	if e != nil {
		t.Fatal(e)
	}
	if e = wrapperJournal.StartFrame(frameID, turnFrame); e != nil {
		t.Fatal(e)
	}
	if unknown {
		if e = wrapperJournal.FinishFrame(frameID, errors.New("connection lost during write")); e != nil {
			t.Fatal(e)
		}
	}
	if e = lease.RecoverNativeCommandReceipt(ctx, testJournalReader{snapshot, root}, command.CommandID); e != nil {
		t.Fatal(e)
	}
	if err = lease.ResolveNativeDispatch(ctx, NativeEventRef{Incarnation: "inc", Sequence: 1, PayloadHash: hash}); err != nil {
		t.Fatal(err)
	}
	state, err = j.DispatchState(ctx, d.ID)
	if err != nil || state != "accepted" {
		t.Fatalf("native event not accepted: %s %v", state, err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	lease, err = j.OpenNativeLease(ctx, testNativeAuthority{}, owner, surface, 1)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := lease.ReceiveNativeEvent(ctx, event)
	if err != nil || replay == nil || replay.PayloadHash != hash {
		t.Fatalf("event replay: %+v %v", replay, err)
	}
	loaded, e := lease.NativeCommand(ctx, d.ID)
	if e != nil || loaded.CommandID != command.CommandID || loaded.NativeBinding == nil || *loaded.NativeBinding != binding {
		t.Fatalf("lost canonical command: %+v %v", loaded, e)
	}
	pending, err := lease.PendingNativeEvents(ctx, 0, 10)
	if err != nil || len(pending) != 1 || pending[0].Event.CommandID != "start" {
		t.Fatalf("projection after restart %+v %v", pending, err)
	}
}

func TestNativeOwnerOnlyCatalogAndHistoryCompletion(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendClaude} {
		t.Run(string(backend), func(t *testing.T) {
			ctx := t.Context()
			j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			surface := Owner{"g", "s", "c", "u", "tenant", "app"}
			owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: backend, HistoryNamespace: "history"}
			lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, owner, surface, 1)
			if err != nil {
				t.Fatal(err)
			}
			binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner}
			command := agentproto.Command{CommandID: "history", Kind: agentproto.CommandThreadHistoryRead, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "session"}}
			d := Dispatch{ID: "inspect", Lane: "inspect", Mode: "resume", Owner: surface, Generation: 1, Session: NativeSession{Backend: string(backend), Namespace: "history", CarrierID: "inc", CarrierGeneration: 1, SessionID: "session"}, CommandID: "history"}
			if err = lease.prepareNativeDispatch(ctx, d, command); err != nil {
				t.Fatal(err)
			}
			if err = lease.MarkSending(ctx, d.ID); err != nil {
				t.Fatal(err)
			}
			event := agentproto.Event{Kind: agentproto.EventThreadHistoryRead, CommandID: command.CommandID, ThreadID: "session", ThreadHistory: &agentproto.ThreadHistoryRecord{}, NativeBinding: &binding, Seq: 1}
			if backend == agentproto.BackendCodex {
				event.RequestID = "rpc"
			}
			hash, _ := agentproto.RuntimeEventPayloadHash(event)
			event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "inc", Sequence: 1, PayloadHash: hash}
			if _, err = lease.ReceiveNativeEvent(ctx, event); err != nil {
				t.Fatal(err)
			}
			if backend == agentproto.BackendCodex {
				state, _ := j.DispatchState(ctx, d.ID)
				if state != "sending" {
					t.Fatalf("accepted without proof: %s", state)
				}
				encoded, _ := json.Marshal(command)
				sum := sha256.Sum256(encoded)
				receipt := agentproto.RuntimeCommandReceipt{Version: 1, CarrierIncarnation: "inc", CommandID: command.CommandID, PayloadHash: hex.EncodeToString(sum[:]), PreparedFrames: 1, Frames: []agentproto.RuntimeFrameReceipt{{FrameID: 1, NativeRequestID: "rpc", NativeMethod: "thread/read", Stage: "unknown"}}}
				if err = lease.recordCommandReceipt(ctx, receipt); err != nil {
					t.Fatal(err)
				}
				if err = lease.ResolveNativeDispatch(ctx, NativeEventRef{"inc", 1, hash}); err != nil {
					t.Fatal(err)
				}
			}
			state, err := j.DispatchState(ctx, d.ID)
			if err != nil || state != "accepted" {
				t.Fatalf("history not complete: %s %v", state, err)
			}
		})
	}
}

func TestNativeOutcomeNeverTreatsUnknownAsRejection(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendClaude} {
		owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "id", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: backend, HistoryNamespace: "history"}
		binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "run"}
		command := agentproto.Command{CommandID: "steer", Kind: agentproto.CommandTurnSteer, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "session", TurnID: "turn"}}
		event := agentproto.Event{Kind: agentproto.EventNativeTurnInputRejected, CommandID: "steer", RequestID: "rpc", ThreadID: "session", TurnID: "turn", Status: "unknown"}
		if _, _, ok := nativeEventOutcome(command, event); ok {
			t.Fatal("unknown marked definitive")
		}
		event.Status = "failed"
		outcome, _, ok := nativeEventOutcome(command, event)
		if !ok || outcome != "failed" {
			t.Fatalf("explicit rejection not recognized: %s %v", outcome, ok)
		}
		event.TurnID = "other"
		if _, _, ok = nativeEventOutcome(command, event); ok {
			t.Fatal("wrong turn accepted")
		}
	}
}

func TestNativeOAuthOutcomeRequiresExactResponse(t *testing.T) {
	owner := agentproto.NativeOwner{Backend: agentproto.BackendCodex}
	binding := agentproto.NativeTurnBinding{Owner: owner}
	command := agentproto.Command{CommandID: "oauth", Kind: agentproto.CommandMCPOAuthLogin, NativeBinding: &binding, MCP: agentproto.MCPCommand{OAuthLogin: &agentproto.MCPOAuthLoginCommand{ServerName: "docs", ThreadID: "thread"}}}
	event := agentproto.Event{CommandID: "oauth", RequestID: "rpc", ThreadID: "thread", Kind: agentproto.EventMCPOAuthLoginURLReady, MCPOAuthLogin: &agentproto.MCPOAuthLoginEvent{ServerName: "docs", ThreadID: "thread", AuthorizationURL: "https://example.test/authorize"}}
	if outcome, method, ok := nativeEventOutcome(command, event); !ok || outcome != "accepted" || method != "mcpServer/oauth/login" {
		t.Fatalf("native response not accepted: %q %q %v", outcome, method, ok)
	}
	event.MCPOAuthLogin.ServerName = "other"
	if _, _, ok := nativeEventOutcome(command, event); ok {
		t.Fatal("different server accepted")
	}
	event.MCPOAuthLogin.ServerName = "docs"
	event.RequestID = ""
	if _, _, ok := nativeEventOutcome(command, event); ok {
		t.Fatal("response without request identity accepted")
	}
	event.RequestID = "rpc"
	binding.Owner.Backend = agentproto.BackendClaude
	if _, _, ok := nativeEventOutcome(command, event); ok {
		t.Fatal("Codex response accepted for Claude")
	}
}

type testQueryReader struct {
	snapshot agentproto.PrivateNativeLaunchSnapshot
	stateDir string
}

func (r testQueryReader) ReadNativeQueryProof(ctx context.Context, owner agentproto.NativeOwner, id string) (NativeQueryProof, error) {
	if owner != r.snapshot.Owner {
		return NativeQueryProof{}, ErrJournalConflict
	}
	v, err := runtimejournal.ReadPrivateQueryLink(ctx, r.snapshot, r.stateDir, id)
	return NativeQueryProof{WaiterID: v.WaiterID, Incarnation: v.Incarnation, SourceCommandID: v.SourceCommandID, SourceCommandHash: v.SourceCommandHash, RequestID: v.RequestID, SourceReceipt: v.SourceReceipt}, err
}
func TestNativeSharedQueryRequiresDurableLink(t *testing.T) {
	ctx := t.Context()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	surface := Owner{"g", "s", "c", "u", "tenant", "app"}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, owner, surface, 1)
	if err != nil {
		t.Fatal(err)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner}
	first := agentproto.Command{CommandID: "first", Kind: agentproto.CommandThreadsRefresh, NativeBinding: &binding}
	second := first
	second.CommandID = "second"
	for _, cmd := range []agentproto.Command{first, second} {
		d := Dispatch{ID: "d-" + cmd.CommandID, Lane: "inspect", Mode: "start", Owner: surface, Generation: 1, Session: NativeSession{Backend: "codex", Namespace: "history", CarrierID: "inc", CarrierGeneration: 1}, CommandID: cmd.CommandID}
		if err = lease.prepareNativeDispatch(ctx, d, cmd); err != nil {
			t.Fatal(err)
		}
		if err = lease.MarkSending(ctx, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	root, cwd := t.TempDir(), t.TempDir()
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: filepath.Join(root, "runtime.db")}
	w, err := runtimejournal.Open(ctx, snapshot.JournalPath, root, cwd, "inc")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err = w.BindPrivateLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, _, err = w.PreparePrivateCommand(first); err != nil {
		t.Fatal(err)
	}
	if _, _, err = w.PreparePrivateCommand(second); err != nil {
		t.Fatal(err)
	}
	frame := []byte(`{"id":"shared-rpc","method":"thread/list"}`)
	id, err := w.PrepareFrame(first.CommandID, frame)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.StartFrame(id, frame); err != nil {
		t.Fatal(err)
	}
	if err = w.FinishFrame(id, nil); err != nil {
		t.Fatal(err)
	}
	link, err := w.RegisterPrivateQueryLink(second.CommandID, first.CommandID, "shared-rpc")
	if err != nil {
		t.Fatal(err)
	}
	event := agentproto.Event{Kind: agentproto.EventNativeQueryCompleted, Action: "thread.list", Status: "completed", CommandID: second.CommandID, RequestID: "shared-rpc", NativeBinding: &binding, Seq: 1, Metadata: map[string]any{"sourceCommandId": first.CommandID, "sourceCommandHash": link.SourceCommandHash}}
	hash, _ := agentproto.RuntimeEventPayloadHash(event)
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "inc", Sequence: 1, PayloadHash: hash}
	if _, err = lease.ReceiveNativeEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	state, _ := j.DispatchState(ctx, "d-second")
	if state != "sending" {
		t.Fatal("metadata alone completed query")
	}
	pending, e := lease.PendingNativeEvents(ctx, 0, 10)
	if e != nil || len(pending) != 0 {
		t.Fatalf("unverified event projected: %+v %v", pending, e)
	}
	if err = lease.RecoverNativeQueryProof(ctx, testQueryReader{snapshot, root}, second.CommandID); err != nil {
		t.Fatal(err)
	}
	if err = lease.ResolveNativeDispatch(ctx, NativeEventRef{"inc", 1, hash}); err != nil {
		t.Fatal(err)
	}
	state, _ = j.DispatchState(ctx, "d-second")
	if state != "accepted" {
		t.Fatalf("linked query not completed: %s", state)
	}
	pending, e = lease.PendingNativeEvents(ctx, 0, 10)
	if e != nil || len(pending) != 1 {
		t.Fatalf("resolved event not projectable: %+v %v", pending, e)
	}
}

func TestNativeInterruptAcknowledgementIsNotQuiescence(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendClaude} {
		t.Run(string(backend), func(t *testing.T) {
			ctx := t.Context()
			j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			surface := Owner{"g", "s", "c", "u", "tenant", "app"}
			owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: backend, HistoryNamespace: "history"}
			lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, owner, surface, 1)
			if err != nil {
				t.Fatal(err)
			}
			seq, _, err := j.Admit(ctx, Ingress{Owner: surface, Generation: 1, EventID: "stop", Lane: "control", Payload: []byte(`{"stop":true}`)})
			if err != nil {
				t.Fatal(err)
			}
			binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "run"}
			command := agentproto.Command{CommandID: "stop-command", Kind: agentproto.CommandTurnInterrupt, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "session", TurnID: "turn"}}
			d := Dispatch{ID: "stop-dispatch", Lane: "control", Mode: "resume", Owner: surface, Generation: 1, Session: NativeSession{Backend: string(backend), Namespace: "history", CarrierID: "inc", CarrierGeneration: 1, SessionID: "session"}, CommandID: command.CommandID, InputSequences: []int64{seq}}
			if err = lease.prepareNativeDispatch(ctx, d, command); err != nil {
				t.Fatal(err)
			}
			if err = lease.MarkSending(ctx, d.ID); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(command)
			sum := sha256.Sum256(raw)
			method := "turn/interrupt"
			if backend == agentproto.BackendClaude {
				method = "interrupt"
			}
			proof := agentproto.RuntimeCommandReceipt{Version: 1, CarrierIncarnation: "inc", CommandID: command.CommandID, PayloadHash: hex.EncodeToString(sum[:]), PreparedFrames: 1, Frames: []agentproto.RuntimeFrameReceipt{{FrameID: 1, NativeRequestID: "rpc", NativeMethod: method, Stage: "unknown"}}}
			if err = lease.recordCommandReceipt(ctx, proof); err != nil {
				t.Fatal(err)
			}
			event := agentproto.Event{Kind: agentproto.EventNativeControlResult, CommandID: command.CommandID, NativeBinding: &binding, Seq: 1, RequestID: "rpc", ThreadID: "session", TurnID: "turn", Action: "turn.interrupt", Status: "accepted"}
			hash, _ := agentproto.RuntimeEventPayloadHash(event)
			event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "inc", Sequence: 1, PayloadHash: hash}
			if _, err = lease.ReceiveNativeEvent(ctx, event); err != nil {
				t.Fatal(err)
			}
			state, err := j.DispatchState(ctx, d.ID)
			if err != nil || state != "accepted" {
				t.Fatalf("control receipt not accepted: %s %v", state, err)
			}
			// This state records only the control RPC, not a stopped-turn or transfer proof.
			if got := nativeExecutionRequired(agentproto.CommandTurnInterrupt); !got {
				t.Fatal("stop lost exact execution binding")
			}
		})
	}
}
