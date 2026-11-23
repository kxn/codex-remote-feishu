package workdisplay

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func EquivalentFinalAlreadyPublished(tx *workstore.Tx, v *workstore.PrivateExecutionDisplay) (bool, error) {
	p := projection{tx: tx, display: v}
	return p.finalAlreadyPublished(workservice.PrivateResultHash(v.Pending))
}

// FinishPreparedPreview projects the already accepted terminal event exactly
// once after a preview decision has been frozen outside the journal transaction.
func FinishPreparedPreview(tx *workstore.Tx, id string) (string, error) {
	v, err := tx.GetPrivateExecutionDisplay(id)
	if err != nil {
		return "", err
	}
	if v.PreviewTerminal == nil || (v.PreviewState != "ready" && v.PreviewState != "skipped") {
		return "", nil
	}
	carrier, err := tx.GetCarrier(v.Incarnation)
	if err != nil {
		return "", err
	}
	event := *v.PreviewTerminal
	v.PreviewTerminal = nil
	p := projection{tx: tx, display: &v, event: event, backend: carrier.Backend, now: v.PreviewTerminalAt}
	if err := p.finish(); err != nil {
		return "", err
	}
	v.PreviewSource, v.PreviewBody = "", ""
	v.PreviewDiff, v.TurnDiff = nil, nil
	v.PreviewTerminalAt = time.Time{}
	old := v.Revision
	v.Revision++
	if err := tx.PutPrivateExecutionDisplay(v, old); err != nil {
		return "", err
	}
	return v.Binding.AttemptID, nil
}
