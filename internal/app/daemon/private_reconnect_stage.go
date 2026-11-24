package daemon

import (
	"encoding/json"
	"path/filepath"
	"reflect"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// VerifyReconnectStage compares a previously confirmed local stage receipt to
// the original admitted action. It performs no filesystem or network I/O.
func (r privateHostReducer) VerifyReconnectStage(original control.Action, effect privatechat.Effect, raw json.RawMessage, pending *state.PendingTextInputRecord) (bool, error) {
	ev, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return false, err
	}
	cmd := ev.DaemonCommand
	if cmd == nil || cmd.Kind != control.DaemonCommandPrivateStageInput || cmd.SurfaceSessionID != original.SurfaceSessionID || !filepath.IsAbs(cmd.ThreadCWD) || filepath.Clean(cmd.ThreadCWD) != cmd.ThreadCWD {
		return false, nil
	}
	var action control.Action
	if json.Unmarshal([]byte(cmd.Text), &action) != nil || !samePrivateStagedInbound(action, original) {
		return false, nil
	}
	var receipt privateStageReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.Version != 1 || receipt.EffectID != effect.ID || receipt.SurfaceID != original.SurfaceSessionID || len(receipt.Files) == 0 {
		return false, nil
	}
	files := make([]control.ActionFileAttachment, 0, len(receipt.Files))
	for _, f := range receipt.Files {
		if f.Target == "" || !filepath.IsLocal(f.Target) || filepath.Clean(f.Target) != f.Target || f.SHA256 == "" {
			return false, nil
		}
		files = append(files, control.ActionFileAttachment{SourceMessageID: f.SourceMessageID, LocalPath: filepath.Join(cmd.ThreadCWD, f.Target), FileName: f.Name})
	}
	inputs := append([]agentproto.Input(nil), original.Inputs...)
	if prompt := inboundFileAttachmentPrompt(files); prompt != "" {
		inputs = append([]agentproto.Input{{Type: agentproto.InputText, Text: prompt}}, inputs...)
	}
	return pending != nil && reflect.DeepEqual(inputs, pending.Inputs), nil
}

// samePrivateStagedInbound compares the entire immutable action, ignoring only
// lifecycle projection fields set by classifyInboundAction at initial receipt.
func samePrivateStagedInbound(staged, original control.Action) bool {
	if staged.Inbound != nil && ((staged.Inbound.LifecycleVerdict != "" && staged.Inbound.LifecycleVerdict != control.InboundLifecycleCurrent) || staged.Inbound.LifecycleReason != "") {
		return false
	}
	normalize := func(a *control.Action) {
		if a.Inbound == nil {
			a.Inbound = &control.ActionInboundMeta{}
		} else {
			clone := *a.Inbound
			a.Inbound = &clone
		}
		a.Inbound.LifecycleVerdict = ""
		a.Inbound.LifecycleReason = ""
	}
	normalize(&staged)
	normalize(&original)
	return reflect.DeepEqual(staged, original)
}
