package daemon

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateReactionHandlerCaptionIncludesFrozenMediaCluster(t *testing.T) {
	for _, backend := range []work.Backend{work.BackendCodex, work.BackendClaude} {
		t.Run(string(backend), func(t *testing.T) {
			f := newReactionActionFixture(t, backend, true)
			id := f.reaction(t, "caption-thumbs-up", "ThumbsUp", f.source.SenderIDs, time.Now())
			sends := 0
			f.relay.send = func(_ string, command agentproto.Command) error {
				sends++
				if command.Kind != agentproto.CommandTurnSteer || command.NativeInputAdmission == nil || len(command.NativeInputAdmission.InputIDs) != 3 {
					t.Fatal("caption reaction lost staged image cluster")
				}
				images := 0
				for _, input := range command.Prompt.Inputs {
					if input.Type == agentproto.InputLocalImage {
						images++
					}
				}
				if images != 2 {
					t.Fatalf("actual image inputs=%d", images)
				}
				return f.event(agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: command.CommandID, RequestID: "caption-rpc", ThreadID: command.Target.ThreadID, TurnID: command.Target.TurnID, Execution: command.Execution, Status: "accepted"})
			}
			if err := f.a.runTeamPrivateReactionActions(t.Context()); err != nil {
				t.Fatal(err)
			}
			if sends != 1 {
				t.Fatalf("actual sends=%d", sends)
			}
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				action, e := tx.GetPrivateReactionAction(id)
				if e != nil {
					return e
				}
				if len(action.InputIDs) != 3 || action.InputIDs[2] != f.late.ID {
					t.Fatal("source did not freeze caption after both images")
				}
				for _, id := range action.InputIDs {
					input, e := tx.GetInbox(id)
					if e != nil {
						return e
					}
					if input.State != work.InboxClaimed {
						t.Fatal("cluster not claimed together")
					}
				}
				return nil
			})
		})
	}
}
func TestPrivateReactionShellKeepsLocalMediaReferencesAndSettlesWholeCluster(t *testing.T) {
	f := newReactionActionFixture(t, work.BackendCodex, true)
	id := f.reaction(t, "caption-applause", "APPLAUSE", f.source.SenderIDs, time.Now())
	sends := 0
	f.relay.send = func(_ string, command agentproto.Command) error {
		sends++
		if command.Kind != agentproto.CommandThreadShellCommand || command.NativeInputAdmission != nil || command.Execution.AttemptID != "" {
			t.Fatal("shell borrowed model input ownership")
		}
		var bundle struct {
			Attachments []struct{ Type, Path string } `json:"attachments"`
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(command.ShellCommand.Payload, "<queued_input_bundle_v1>\n"), "\n</queued_input_bundle_v1>\n")
		if err := json.Unmarshal([]byte(raw), &bundle); err != nil {
			return err
		}
		if len(bundle.Attachments) != 2 {
			t.Fatalf("local image references=%d", len(bundle.Attachments))
		}
		for _, image := range bundle.Attachments {
			data, err := os.ReadFile(image.Path)
			if err != nil || !bytes.Equal(data, steerImageBytes) || image.Type != "image" {
				t.Fatal("shell payload lost original local image bytes")
			}
		}
		if err := f.event(agentproto.Event{Kind: agentproto.EventNativeShellRPCResult, CommandID: command.CommandID, RequestID: "shell-rpc", ThreadID: command.Target.ThreadID, Execution: command.Execution, Status: "acknowledged"}); err != nil {
			return err
		}
		pickerTx(t, f.a, func(tx *workstore.Tx) error {
			action, e := tx.GetPrivateReactionAction(id)
			if e != nil {
				return e
			}
			for _, id := range action.InputIDs {
				input, e := tx.GetInbox(id)
				if e != nil {
					return e
				}
				if input.State != work.InboxPending || input.ShellOperationID == "" {
					t.Fatal("RPC acknowledgement consumed input before actual read")
				}
			}
			return nil
		})
		return f.event(agentproto.Event{Kind: agentproto.EventNativeShellItemObserved, Action: "completed", CommandID: command.CommandID, ThreadID: command.Target.ThreadID, TurnID: "actual-independent-shell-turn", ItemID: "actual-shell-item", Execution: command.Execution, Status: "completed", Metadata: map[string]any{"shellOperationID": command.ShellCommand.OperationID, "source": "userShell", "exitCode": 0, "userShellOutput": command.ShellCommand.Payload}})
	}
	if err := f.a.runTeamPrivateReactionActions(t.Context()); err != nil {
		t.Fatal(err)
	}
	if sends != 1 {
		t.Fatalf("shell native sends=%d", sends)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		action, e := tx.GetPrivateReactionAction(id)
		if e != nil {
			return e
		}
		if len(action.InputIDs) != 3 {
			t.Fatal("caption source lost frozen image group")
		}
		for _, id := range action.InputIDs {
			input, e := tx.GetInbox(id)
			if e != nil {
				return e
			}
			if input.State != work.InboxProcessed || input.ClaimedAttemptID != "" {
				t.Fatal("actual shell did not settle whole independent cluster")
			}
		}
		ids, e := tx.EffectiveAttemptInputIDs("steer-attempt")
		if e != nil {
			return e
		}
		if len(ids) != 1 {
			t.Fatal("shell group contaminated model attempt")
		}
		return nil
	})
}
