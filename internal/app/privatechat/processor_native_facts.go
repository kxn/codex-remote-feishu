package privatechat

import (
	"context"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// validateNativeProjectionFact checks that state-changing local projections
// are based on the exact event that settled this carrier's command.
func (j *Journal) validateNativeProjectionFact(ctx context.Context, l *NativeLease, ref NativeEventRef, event agentproto.Event) error {
	expected := ""
	switch event.Kind {
	case agentproto.EventNativeTurnInputAccepted:
		expected = "accepted"
	case agentproto.EventNativeTurnInputRejected:
		expected = "failed"
	case agentproto.EventNativeShellRPCResult:
		if event.Status == "acknowledged" {
			expected = "accepted"
		} else if event.Status == "failed" {
			expected = "failed"
		} else {
			return ErrJournalConflict
		}
	case agentproto.EventNativeControlResult:
		switch event.Action {
		case "thread.compact.start", "review.start":
			if event.Status == "accepted" {
				expected = "accepted"
			} else if event.Status == "failed" {
				expected = "failed"
			} else {
				return ErrJournalConflict
			}
		case "request.response.written":
			expected = "sent"
		}
	case agentproto.EventSystemError:
		if event.Problem != nil && event.Problem.Operation == "thread.compact.start" && event.Problem.Stage == "thread_resume_response" {
			expected = "failed"
		}
	case agentproto.EventNativeCommandRejected:
		expected = "failed"
	case agentproto.EventNativeShellItemObserved:
		var raw []byte
		if err := j.db.QueryRowContext(ctx, `SELECT n.payload FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id WHERE n.command_id=? AND d.carrier=? AND d.state='accepted'`, event.CommandID, l.native.CarrierIncarnation).Scan(&raw); err != nil {
			return err
		}
		var command agentproto.Command
		if json.Unmarshal(raw, &command) != nil || command.Kind != agentproto.CommandThreadShellCommand || command.ShellCommand.OperationID == "" || event.Metadata["shellOperationID"] != command.ShellCommand.OperationID || event.ThreadID != command.Target.ThreadID || event.ItemID == "" || event.TurnID == "" {
			return ErrJournalConflict
		}
		return nil
	}
	if expected == "" {
		return nil
	}
	var state string
	var sequence int64
	if err := j.db.QueryRowContext(ctx, `SELECT d.state,d.event_sequence FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id WHERE n.command_id=? AND d.carrier=?`, event.CommandID, l.native.CarrierIncarnation).Scan(&state, &sequence); err != nil {
		return err
	}
	if expected == "sent" && state == "resolved" {
		state = "sent"
	}
	if state != expected || sequence != int64(ref.Sequence) {
		return ErrJournalConflict
	}
	return nil
}
