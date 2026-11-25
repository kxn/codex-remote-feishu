package privatechat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestReviewSessionProofRequiresAcceptedExactOwner(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	n := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	l, err := j.OpenNativeLease(t.Context(), testNativeAuthority{}, n, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	b := agentproto.NativeTurnBinding{Version: 1, Owner: n, ExecutionID: "exec"}
	c := agentproto.Command{CommandID: "review", Kind: agentproto.CommandReviewStart, NativeBinding: &b, Target: agentproto.Target{ThreadID: "parent"}}
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	_, err = j.db.Exec(`INSERT INTO dispatch(id,gateway,tenant,app_id,surface,chat,user_id,generation,lane,mode,backend,namespace,session,carrier,carrier_generation,command_id,state,event_sequence) VALUES('d',?,?,?,?,?,?,1,'control','resume','codex','history','parent','inc',1,'review','accepted',2)`, o.GatewayID, o.TenantID, o.AppID, o.SurfaceID, o.ChatID, o.UserID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = j.db.Exec(`INSERT INTO native_commands(dispatch_id,command_id,payload,payload_hash) VALUES('d','review',?,?)`, raw, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	receipt, _ := json.Marshal(agentproto.RuntimeCommandReceipt{CommandID: "review", CarrierIncarnation: "inc", Frames: []agentproto.RuntimeFrameReceipt{{NativeRequestID: "create-rpc", NativeMethod: "thread/start", Stage: "written"}}})
	if _, err = j.db.Exec(`INSERT INTO native_command_receipts(command_id,incarnation,payload) VALUES('review','inc',?)`, receipt); err != nil {
		t.Fatal(err)
	}
	created := agentproto.Event{Kind: agentproto.EventReviewChildCreated, CommandID: "review", RequestID: "create-rpc", ThreadID: "child", Action: "review.start", NativeBinding: &b, Metadata: map[string]any{"parentThreadID": "parent", "stage": "thread/start"}}
	createdHash, err := agentproto.RuntimeEventPayloadHash(created)
	if err != nil {
		t.Fatal(err)
	}
	createdRaw, _ := json.Marshal(created)
	if _, err = j.db.Exec(`INSERT INTO native_events(incarnation,sequence,payload_hash,command_id,payload) VALUES('inc',1,?,'review',?)`, createdHash, createdRaw); err != nil {
		t.Fatal(err)
	}
	e := agentproto.Event{Kind: agentproto.EventNativeControlResult, CommandID: "review", RequestID: "rpc", ThreadID: "parent", TurnID: "turn", Action: "review.start", Status: "accepted", NativeBinding: &b, Metadata: map[string]any{"reviewThreadID": "child"}}
	hash, err := agentproto.RuntimeEventPayloadHash(e)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(e)
	_, err = j.db.Exec(`INSERT INTO native_events(incarnation,sequence,payload_hash,command_id,payload) VALUES('inc',2,?,'review',?)`, hash, raw)
	if err != nil {
		t.Fatal(err)
	}
	ref := NativeEventRef{Incarnation: "inc", Sequence: 2, PayloadHash: hash}
	if got, err := l.CommittedReviewSession(t.Context(), ref); err != nil || got != "child" {
		t.Fatalf("accepted child=%q err=%v", got, err)
	}
	badReceipt, _ := json.Marshal(agentproto.RuntimeCommandReceipt{CommandID: "review", CarrierIncarnation: "inc", Frames: []agentproto.RuntimeFrameReceipt{{NativeRequestID: "other", NativeMethod: "thread/start", Stage: "written"}}})
	if _, err = j.db.Exec(`UPDATE native_command_receipts SET payload=? WHERE command_id='review'`, badReceipt); err != nil {
		t.Fatal(err)
	}
	if _, err = l.CommittedReviewSession(t.Context(), ref); err == nil {
		t.Fatal("foreign create request accepted")
	}
	if _, err = j.db.Exec(`UPDATE native_command_receipts SET payload=? WHERE command_id='review'`, receipt); err != nil {
		t.Fatal(err)
	}
	subturn := agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "child", TurnID: "subturn", RootTurnID: "turn", NativeBinding: &b}
	subRaw, _ := json.Marshal(subturn)
	if _, err = j.db.Exec(`INSERT INTO native_events(incarnation,sequence,payload_hash,command_id,payload) VALUES('inc',3,'subhash','',?)`, subRaw); err != nil {
		t.Fatal(err)
	}
	if ok, err := j.isPrivateReviewTurn(t.Context(), l, subturn); err != nil || !ok {
		t.Fatalf("exact root subturn: %v %v", ok, err)
	}
	wrong := subturn
	wrong.ThreadID = "foreign-child"
	if ok, err := j.isPrivateReviewTurn(t.Context(), l, wrong); err != nil || ok {
		t.Fatalf("foreign child: %v %v", ok, err)
	}
	for _, state := range []string{"failed", "unknown", "sending"} {
		if _, err = j.db.Exec(`UPDATE dispatch SET state=? WHERE id='d'`, state); err != nil {
			t.Fatal(err)
		}
		if _, err = l.CommittedReviewSession(t.Context(), ref); err == nil {
			t.Fatalf("%s accepted", state)
		}
	}
	if _, err = j.db.Exec(`UPDATE dispatch SET state='accepted' WHERE id='d'`); err != nil {
		t.Fatal(err)
	}
	foreign := *l
	foreign.surface.UserID = "other"
	if _, err = foreign.CommittedReviewSession(t.Context(), ref); err == nil {
		t.Fatal("foreign owner accepted")
	}
}
