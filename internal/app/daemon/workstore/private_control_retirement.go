package workstore

import "github.com/kxn/codex-remote-feishu/internal/core/work"

// Only unclaimed copies are retired. Their original inputs remain in their
// conversation, and an already running native turn still requires settlement.
func (t *Tx) CancelObsoletePrivateControlInputs() error {
	inputs, err := listRecords[work.Inbox](t, "inbox", ` WHERE state='pending' AND coalesce(json_extract(payload,'$.control_of_input_id'),'')<>'' AND EXISTS(SELECT 1 FROM scopes s WHERE s.id=json_extract(inbox.payload,'$.scope_id') AND json_extract(s.payload,'$.kind')='personal')`)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		previous := input.Revision
		input.State = work.InboxCancelled
		input.Revision++
		if err = t.PutInbox(input, previous); err != nil {
			return err
		}
	}
	return nil
}
