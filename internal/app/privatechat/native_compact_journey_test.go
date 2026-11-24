package privatechat

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestPrivateCompactAssociatesRealNativeItemAndFailure(t *testing.T) {
	for _, mode := range []string{"success", "failure", "delayed-old", "idle-reject", "idle-reject-resume-unknown"} {
		t.Run(mode, func(t *testing.T) {
			failure := mode == "failure"
			ctx := context.Background()
			root, cwd := t.TempDir(), t.TempDir()
			j, err := OpenJournal(filepath.Join(root, "private.db"))
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
			p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
			if err != nil {
				t.Fatal(err)
			}
			if err = p.RegisterNativeHello(ctx, lease, nativeHello(owner)); err != nil {
				t.Fatal(err)
			}
			attach := privateAction(surface, control.ActionAttachInstance)
			attach.InstanceID = owner.InstanceID
			if _, err = p.app.Action(attach); err != nil {
				t.Fatal(err)
			}
			seq, _, err := j.Admit(ctx, Ingress{Owner: surface, Generation: 1, EventID: "compact", Lane: "control", Payload: []byte(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "compact-execution"}
			command := agentproto.Command{CommandID: "compact", Kind: agentproto.CommandThreadCompactStart, NativeBinding: &binding, Origin: agentproto.Origin{Surface: surface.SurfaceID}, Target: agentproto.Target{ThreadID: "thread"}}
			d := Dispatch{ID: "dispatch", Owner: surface, Generation: 1, CommandID: command.CommandID, Lane: "control", Mode: "resume", Session: NativeSession{Backend: "codex", Namespace: "history", CarrierID: "inc", CarrierGeneration: 1, SessionID: "thread"}, InputSequences: []int64{seq}}
			if err = lease.prepareNativeDispatch(ctx, d, command); err != nil {
				t.Fatal(err)
			}
			tx, err := j.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = preparePrivateCompactTx(ctx, tx, lease, command); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if err = lease.MarkSending(ctx, d.ID); err != nil {
				t.Fatal(err)
			}
			snap := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: filepath.Join(root, "native.db")}
			native, err := runtimejournal.Open(ctx, snap.JournalPath, root, cwd, "inc")
			if err != nil {
				t.Fatal(err)
			}
			defer native.Close()
			if err = native.BindPrivateLaunchSnapshot(snap); err != nil {
				t.Fatal(err)
			}
			if _, _, err = native.PreparePrivateCommand(command); err != nil {
				t.Fatal(err)
			}
			var delayed []agentproto.Event
			if mode == "delayed-old" {
				delayed, err = native.PersistEvents([]agentproto.Event{{Kind: agentproto.EventItemCompleted, ThreadID: "thread", TurnID: "old-turn", ItemKind: "context_compaction"}})
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "idle-reject" || mode == "idle-reject-resume-unknown" {
				if _, err = native.PersistEvents([]agentproto.Event{{Kind: agentproto.EventTurnStarted, ThreadID: "thread", TurnID: "active"}}); err != nil {
					t.Fatal(err)
				}
				resume := []byte(`{"id":"resume","method":"thread/resume"}`)
				resumeID, e := native.PrepareFrame(command.CommandID, resume)
				if e != nil {
					t.Fatal(e)
				}
				if e = native.StartFrame(resumeID, resume); e != nil {
					t.Fatal(e)
				}
				var writeErr error
				if mode == "idle-reject-resume-unknown" {
					writeErr = fmt.Errorf("uncertain resume")
				}
				if e = native.FinishFrame(resumeID, writeErr); e != nil {
					t.Fatal(e)
				}
			}
			tr := codex.NewTranslator("instance")
			if _, err = tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread"}}`)); err != nil {
				t.Fatal(err)
			}
			frames, err := tr.TranslateCommand(command)
			if err != nil || len(frames) != 1 {
				t.Fatalf("frames=%d err=%v", len(frames), err)
			}
			var frame struct {
				ID string `json:"id"`
			}
			if err = json.Unmarshal(frames[0], &frame); err != nil {
				t.Fatal(err)
			}
			fid, err := native.PrepareFrame(command.CommandID, frames[0])
			if err != nil {
				t.Fatal(err)
			}
			if err = native.StartFrame(fid, frames[0]); mode == "idle-reject" || mode == "idle-reject-resume-unknown" {
				if err == nil {
					t.Fatal("active turn allowed compact")
				}
				rejected, e := native.RejectPrivatePreparedFrame(fid, err)
				if mode == "idle-reject-resume-unknown" {
					if e == nil || len(rejected) != 0 {
						t.Fatal("uncertain resume was declared safely rejected")
					}
					return
				}
				if e != nil {
					t.Fatal(e)
				}
				if e = lease.RecoverNativeCommandReceipt(ctx, testJournalReader{snap, root}, command.CommandID); e != nil {
					t.Fatal(e)
				}
				if _, e = lease.ReceiveNativeEvent(ctx, rejected[0]); e != nil {
					t.Fatal(e)
				}
				var status string
				if e = j.db.QueryRow(`SELECT state FROM dispatch WHERE id='dispatch'`).Scan(&status); e != nil || status != "failed" {
					t.Fatalf("dispatch=%s err=%v", status, e)
				}
				return
			} else if err != nil {
				t.Fatal(err)
			}
			if _, err = native.FinishPrivateFrame(fid, nil); err != nil {
				t.Fatal(err)
			}
			if err = lease.RecoverNativeCommandReceipt(ctx, testJournalReader{snap, root}, command.CommandID); err != nil {
				t.Fatal(err)
			}
			for _, event := range delayed {
				if _, err = lease.ReceiveNativeEvent(ctx, event); err != nil {
					t.Fatal(err)
				}
			}
			observe := func(raw string) {
				t.Helper()
				r, e := tr.ObserveServer([]byte(raw))
				if e != nil {
					t.Fatal(e)
				}
				for _, ev := range r.Events {
					if ev.CommandID != "" {
						ev.NativeBinding = &binding
					}
					evs, e := native.PersistEvents([]agentproto.Event{ev})
					if e != nil {
						t.Fatal(e)
					}
					for _, v := range evs {
						if _, e = lease.ReceiveNativeEvent(ctx, v); e != nil {
							t.Fatalf("receive %s: %v", v.Kind, e)
						}
					}
				}
			}
			// Native start can precede RPC acknowledgement and the identifying item.
			observe(`{"method":"turn/started","params":{"threadId":"thread","turn":{"id":"compact-turn"}}}`)
			observe(fmt.Sprintf(`{"id":%q,"result":{}}`, frame.ID))
			if mode == "delayed-old" {
				var state, turn string
				if err = j.db.QueryRow(`SELECT state,turn_id FROM private_compact_operations WHERE command_id='compact'`).Scan(&state, &turn); err != nil || state != "pending" || turn != "" {
					t.Fatalf("old event bound compact: state=%s turn=%s err=%v", state, turn, err)
				}
			}
			if !failure {
				observe(`{"method":"item/completed","params":{"threadId":"thread","turnId":"compact-turn","item":{"id":"compact-item","type":"contextCompaction","status":"completed"}}}`)
			}
			status := "completed"
			if failure {
				status = "failed"
			}
			observe(fmt.Sprintf(`{"method":"turn/completed","params":{"threadId":"thread","turn":{"id":"compact-turn","status":%q}}}`, status))
			var opState, turn string
			if err = j.db.QueryRow(`SELECT state,turn_id FROM private_compact_operations WHERE command_id='compact'`).Scan(&opState, &turn); err != nil || opState != "completed" || turn != "compact-turn" {
				t.Fatalf("compact state=%s turn=%s err=%v", opState, turn, err)
			}
			if mode == "delayed-old" {
				return
			}
			events, _, err := native.Replay(0, 20)
			if err != nil {
				t.Fatal(err)
			}
			var seen []agentproto.Event
			for _, event := range events {
				ref := NativeEventRef{Incarnation: "inc", Sequence: event.Seq, PayloadHash: event.Journal.PayloadHash}
				if _, err = p.ApplyNative(ctx, lease, ref, captureRequestEvents{&seen}); err != nil {
					t.Fatalf("apply %s seq=%d: %v", event.Kind, event.Seq, err)
				}
			}
		})
	}
}
