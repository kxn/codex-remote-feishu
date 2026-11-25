package runtimejournal

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestEphemeralReviewInspectionRequiresExactJournalProof(t *testing.T) {
	for _, tc := range []struct {
		name, frame, createdRequest, child string
		want                               bool
	}{
		{"valid", `{"id":"create","method":"thread/start","params":{"ephemeral":true}}`, "create", "child", true},
		{"selected-subdirectory", `{"id":"create","method":"thread/start","params":{"ephemeral":true}}`, "create", "child", true},
		{"wrong-cwd", `{"id":"create","method":"thread/start","params":{"ephemeral":true}}`, "create", "child", false},
		{"not-ephemeral", `{"id":"create","method":"thread/start","params":{"ephemeral":false}}`, "create", "child", false},
		{"wrong-request", `{"id":"create","method":"thread/start","params":{"ephemeral":true}}`, "other", "child", false},
		{"wrong-child", `{"id":"create","method":"thread/start","params":{"ephemeral":true}}`, "create", "other", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, cwd := t.TempDir(), t.TempDir()
			path := filepath.Join(root, "native.db")
			owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "o", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "i", HostID: "h", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
			s, err := Open(t.Context(), path, root, cwd, "inc")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: path}
			if err = s.BindPrivateLaunchSnapshot(snapshot); err != nil {
				t.Fatal(err)
			}
			binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "exec"}
			selectedCWD := cwd
			if tc.name == "selected-subdirectory" || tc.name == "wrong-cwd" {
				selectedCWD = filepath.Join(cwd, "project")
			}
			command := agentproto.Command{CommandID: "review", Kind: agentproto.CommandReviewStart, Target: agentproto.Target{ThreadID: "parent", CWD: selectedCWD}, NativeBinding: &binding}
			if _, _, err = s.PreparePrivateCommand(command); err != nil {
				t.Fatal(err)
			}
			frameCWD := selectedCWD
			if tc.name == "wrong-cwd" {
				frameCWD = cwd
			}
			frame := []byte(strings.Replace(tc.frame, `"ephemeral":`, `"cwd":`+strconv.Quote(frameCWD)+`,"ephemeral":`, 1))
			id, err := s.PrepareFrame(command.CommandID, frame)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.StartFrame(id, frame); err != nil {
				t.Fatal(err)
			}
			if err = s.FinishFrame(id, nil); err != nil {
				t.Fatal(err)
			}
			reviewFrame := []byte(`{"id":"review-rpc","method":"review/start","params":{"threadId":"child","delivery":"inline"}}`)
			reviewID, err := s.PrepareFrame(command.CommandID, reviewFrame)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.StartFrame(reviewID, reviewFrame); err != nil {
				t.Fatal(err)
			}
			if err = s.FinishFrame(reviewID, nil); err != nil {
				t.Fatal(err)
			}
			events := []agentproto.Event{{Kind: agentproto.EventReviewChildCreated, CommandID: "review", RequestID: tc.createdRequest, ThreadID: tc.child, Action: "review.start", NativeBinding: &binding, Metadata: map[string]any{"parentThreadID": "parent", "stage": "thread/start"}}, {Kind: agentproto.EventNativeControlResult, CommandID: "review", RequestID: "review-rpc", ThreadID: "parent", TurnID: "turn", Action: "review.start", Status: "accepted", NativeBinding: &binding, Metadata: map[string]any{"reviewThreadID": "child"}}, {Kind: agentproto.EventTurnCompleted, ThreadID: "child", TurnID: "turn", Status: "completed"}}
			if _, err = s.PersistEvents(events); err != nil {
				t.Fatal(err)
			}
			proof, err := s.PrivateEphemeralReviewProof(owner, "child")
			if tc.want {
				if err != nil || proof == nil {
					t.Fatalf("proof=%+v err=%v", proof, err)
				}
				if _, err = s.db.Exec(`UPDATE frames SET native_id='other' WHERE id=?`, reviewID); err != nil {
					t.Fatal(err)
				}
				if proof, err = s.PrivateEphemeralReviewProof(owner, "child"); err == nil && proof != nil {
					t.Fatal("mismatched second request accepted")
				}
				if _, err = s.db.Exec(`UPDATE frames SET native_id='review-rpc' WHERE id=?`, reviewID); err != nil {
					t.Fatal(err)
				}
				wrongChild := []byte(`{"id":"review-rpc","method":"review/start","params":{"threadId":"other","delivery":"inline"}}`)
				if _, err = s.db.Exec(`UPDATE private_review_frames SET payload=? WHERE frame_id=?`, wrongChild, reviewID); err != nil {
					t.Fatal(err)
				}
				if _, err = s.db.Exec(`UPDATE frames SET hash=? WHERE id=?`, hash(wrongChild), reviewID); err != nil {
					t.Fatal(err)
				}
				if _, err = s.PrivateEphemeralReviewProof(owner, "child"); err == nil {
					t.Fatal("review frame targeting another child accepted")
				}
				if _, err = s.db.Exec(`UPDATE frames SET hash=? WHERE id=?`, hash(reviewFrame), reviewID); err != nil {
					t.Fatal(err)
				}
				if _, err = s.db.Exec(`UPDATE private_review_frames SET payload=? WHERE frame_id=?`, []byte(`{"tampered":true}`), reviewID); err != nil {
					t.Fatal(err)
				}
				if _, err = s.PrivateEphemeralReviewProof(owner, "child"); err == nil {
					t.Fatal("tampered review frame accepted")
				}
				if _, err = s.db.Exec(`UPDATE private_review_frames SET payload=? WHERE frame_id=?`, reviewFrame, reviewID); err != nil {
					t.Fatal(err)
				}
				if _, err = s.db.Exec(`UPDATE private_review_frames SET payload=? WHERE frame_id=?`, []byte(`{"tampered":true}`), id); err != nil {
					t.Fatal(err)
				}
				if _, err = s.PrivateEphemeralReviewProof(owner, "child"); err == nil {
					t.Fatal("tampered frame accepted")
				}
			} else if err == nil && proof != nil {
				t.Fatalf("invalid proof=%+v", proof)
			}
		})
	}
}
