package workstore

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"reflect"
	"strings"
)

func (t *Tx) validateReactionAdmission(source PrivateActionSource, command agentproto.Command, inputIDs []string) error {
	if source.ReactionID == "" {
		if command.Kind == agentproto.CommandThreadShellCommand {
			return ErrScopeDenied
		}
		return nil
	}
	action, err := t.GetPrivateReactionAction(source.ReactionID)
	if err != nil {
		return err
	}
	if command.Execution == nil || *command.Execution != action.Execution || command.Target.ThreadID != action.SessionID || command.Target.TurnID != action.TurnID {
		return ErrScopeDenied
	}
	if !reflect.DeepEqual(inputIDs, action.InputIDs) {
		return ErrScopeDenied
	}
	anchor, err := t.GetInbox(action.InputID)
	if err != nil {
		return err
	}
	cluster, err := t.PendingInputMediaCluster(anchor)
	if err != nil {
		return err
	}
	var actualIDs []string
	for _, input := range cluster {
		actualIDs = append(actualIDs, input.ID)
	}
	if !reflect.DeepEqual(actualIDs, action.InputIDs) {
		return ErrConflict
	}
	if action.Emoji == "THUMBSUP" && command.Kind == agentproto.CommandTurnSteer {
		return nil
	}
	return ErrScopeDenied
}

func validPrivateShellBundle(payload string) bool {
	const start = "<queued_input_bundle_v1>\n"
	const end = "\n</queued_input_bundle_v1>\n"
	if !strings.HasPrefix(payload, start) || !strings.HasSuffix(payload, end) {
		return false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(payload, start), end)
	var bundle struct {
		Schema      string            `json:"schema"`
		Text        string            `json:"text"`
		Attachments []json.RawMessage `json:"attachments"`
	}
	return json.Unmarshal([]byte(raw), &bundle) == nil && bundle.Schema == "queued_input_bundle.v1" && (strings.TrimSpace(bundle.Text) != "" || len(bundle.Attachments) > 0)
}
