package privatechat

import (
	"context"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// FileSnapshotsCleanupEligibility consults durable effects, including applied
// effects. Any unresolved exact reference vetoes deletion of that path.
func (j *Journal) FileSnapshotsCleanupEligibility(ctx context.Context, paths []string) (map[string]bool, error) {
	if j == nil {
		return nil, ErrJournalState
	}
	candidates := make(map[string]bool, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if path == "" {
			return nil, ErrJournalState
		}
		candidates[path] = true
	}
	if len(candidates) == 0 {
		return candidates, nil
	}
	rows, err := j.db.QueryContext(ctx, `SELECT e.id,e.body,e.state,e.receipt,EXISTS(SELECT 1 FROM private_effect_applied a WHERE a.effect_id=e.id) FROM private_effect e WHERE instr(CAST(e.body AS TEXT),'FileSHA256')>0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, state string
		var body, receipt []byte
		var applied bool
		if err = rows.Scan(&id, &body, &state, &receipt, &applied); err != nil {
			return nil, err
		}
		event, _, e := DecodeStoredEffect(body)
		if e != nil {
			return nil, e
		}
		cmd := event.DaemonCommand
		if cmd == nil || cmd.Kind != control.DaemonCommandPrivateSendFile || cmd.FileSHA256 == "" {
			continue
		}
		if _, ok := candidates[cmd.LocalPath]; !ok {
			continue
		}
		seen[cmd.LocalPath] = true
		var result struct {
			Version  int
			EffectID string
			Unknown  bool
		}
		if state != "confirmed" || !applied || json.Unmarshal(receipt, &result) != nil || result.Version != 1 || result.EffectID != id || result.Unknown {
			candidates[cmd.LocalPath] = false
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for path := range candidates {
		if !seen[path] {
			candidates[path] = false
		}
	}
	return candidates, nil
}
