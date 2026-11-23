package privatechat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// prepareNativeImageCleanup is called only while applying an exact durable
// terminal under its authenticated lease. The journal joins accepted commands
// to their frozen source ingress; no path comes from the terminal event.
func (j *Journal) prepareNativeImageCleanup(ctx context.Context, l *NativeLease, event agentproto.Event, primary string) error {
	if event.Kind != agentproto.EventTurnCompleted || event.NativeBinding == nil || event.NativeBinding.Owner != l.native || event.ThreadID == "" || event.TurnID == "" || primary == "" {
		return ErrJournalConflict
	}
	tx, e := j.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, `SELECT d.id,n.payload,i.event_id,i.payload FROM dispatch d JOIN native_commands n ON n.dispatch_id=d.id JOIN dispatch_input di ON di.dispatch_id=d.id JOIN ingress i ON i.seq=di.seq WHERE d.tenant=? AND d.app_id=? AND d.gateway=? AND d.surface=? AND d.chat=? AND d.user_id=? AND d.generation=? AND d.carrier=? AND d.state='accepted' AND json_extract(n.payload,'$.nativeBinding.executionId')=? AND i.tenant=d.tenant AND i.app_id=d.app_id AND i.gateway=d.gateway AND i.surface=d.surface AND i.chat=d.chat AND i.user_id=d.user_id AND i.generation=d.generation`, l.surface.TenantID, l.surface.AppID, l.surface.GatewayID, l.surface.SurfaceID, l.surface.ChatID, l.surface.UserID, l.generation, l.native.CarrierIncarnation, event.NativeBinding.ExecutionID)
	if e != nil {
		return e
	}
	type source struct {
		dispatch, eventID string
		command           agentproto.Command
		action            control.Action
	}
	var sources []source
	for rows.Next() {
		var d, id string
		var commandRaw, actionRaw []byte
		if e = rows.Scan(&d, &commandRaw, &id, &actionRaw); e != nil {
			rows.Close()
			return e
		}
		var x source
		x.dispatch = d
		x.eventID = id
		if e = json.Unmarshal(commandRaw, &x.command); e != nil {
			rows.Close()
			return e
		}
		if e = json.Unmarshal(actionRaw, &x.action); e != nil {
			rows.Close()
			return e
		}
		sources = append(sources, x)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, x := range sources {
		if x.command.NativeBinding == nil || *x.command.NativeBinding != *event.NativeBinding {
			return ErrJournalConflict
		}
		if x.command.CommandID != primary && !(x.command.Kind == agentproto.CommandTurnSteer && x.command.Target.ThreadID == event.ThreadID && x.command.Target.TurnID == event.TurnID) {
			continue
		}
		effectID := strings.TrimSuffix(x.dispatch, ":native")
		paths := map[string]bool{}
		if x.action.LocalPath != "" {
			paths[x.action.LocalPath] = true
		}
		for _, input := range x.action.Inputs {
			if input.Path != "" {
				paths[input.Path] = true
			}
		}
		for _, input := range x.action.SteerInputs {
			if input.Path != "" {
				paths[input.Path] = true
			}
		}
		for _, input := range x.command.Prompt.Inputs {
			if input.Path == "" || !paths[input.Path] {
				continue
			}
			if !strings.HasSuffix(x.dispatch, ":native") || x.action.MessageID == "" {
				// Historical dispatches without exact effect/source provenance are
				// retained; terminal projection must still finish normally.
				continue
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO private_staging_cleanup(effect_id,path,event_id,message_id,sha256,state) VALUES(?,?,?,?,'','pending') ON CONFLICT(effect_id,path) DO NOTHING`, effectID, input.Path, x.eventID, x.action.MessageID)
			if e != nil {
				return e
			}
			var eventID, messageID, hash string
			e = tx.QueryRowContext(ctx, `SELECT event_id,message_id,sha256 FROM private_staging_cleanup WHERE effect_id=? AND path=?`, effectID, input.Path).Scan(&eventID, &messageID, &hash)
			if e != nil {
				return e
			}
			if eventID != x.eventID || messageID != x.action.MessageID || hash != "" {
				return ErrJournalConflict
			}
		}
	}
	return tx.Commit()
}
