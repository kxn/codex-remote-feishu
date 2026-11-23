package privatechat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

var ErrEffectSuperseded = errors.New("private command's automatic input is no longer enabled")

// PrepareEffectDispatch is the application host's dispatch entry point. It reads
// the committed command and exact queue selection; the host supplies only the
// authenticated execution identity, never replacement text or guessed inputs.
// Neither preparation nor an idempotent retry is permission to resend bytes:
// MarkSending must succeed once before transport is invoked.
func (l *NativeLease) PrepareEffectDispatch(ctx context.Context, effectID string, binding agentproto.NativeTurnBinding) (Dispatch, agentproto.Command, error) {
	var d Dispatch
	var command agentproto.Command
	if l == nil || effectID == "" || binding.Owner != l.native {
		return d, command, ErrJournalState
	}
	err := l.with(ctx, func() error {
		tx, err := l.journal.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		o := l.surface
		var body []byte
		var effectState, lane string
		err = tx.QueryRowContext(ctx, `SELECT body,state,lane FROM private_effect WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, effectID, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, l.generation).Scan(&body, &effectState, &lane)
		if err != nil {
			return err
		}
		if lane != "background" || (effectState != "prepared" && effectState != "sending" && effectState != "unknown") {
			return ErrJournalState
		}
		event, inputs, target, err := DecodeStoredEffectTarget(body)
		if err != nil {
			return err
		}
		if event.SurfaceSessionID != o.SurfaceID || event.Command == nil || event.Command.Execution != nil || event.Command.NativeBinding != nil || event.Command.NativeInputAdmission != nil {
			return ErrJournalState
		}
		if target == nil || *target != l.native.InstanceID {
			return ErrJournalConflict
		}
		command = *event.Command
		command.NativeBinding = &binding
		d = Dispatch{ID: effectID + ":native", Owner: o, Generation: l.generation, CommandID: command.CommandID, Lane: "control", Mode: "none", Session: NativeSession{Backend: string(l.native.Backend), Namespace: l.native.HistoryNamespace, CarrierID: l.native.CarrierIncarnation, CarrierGeneration: l.native.InstanceGeneration, SessionID: command.Target.ThreadID}}
		if d.Session.SessionID != "" {
			d.Mode = "resume"
		}
		switch command.Kind {
		case agentproto.CommandPromptSend:
			d.Lane = "input"
			if command.Target.ExecutionMode == agentproto.PromptExecutionModeStartNew {
				d.Mode = "start"
			}
		case agentproto.CommandTurnSteer, agentproto.CommandThreadShellCommand:
			d.Lane = "input"
		case agentproto.CommandThreadsRefresh, agentproto.CommandThreadHistoryRead, agentproto.CommandModelList, agentproto.CommandThreadRead, agentproto.CommandThreadGoalGet:
			d.Lane = "inspect"
		}
		if d.Lane == "input" && (inputs == nil || len(inputs.Items) == 0) {
			return ErrJournalState
		}
		if inputs != nil {
			if inputs.InstanceID != l.native.InstanceID {
				return ErrJournalConflict
			}
			// Once prepared, the frozen dispatch is immutable even if a subsequent
			// setting changed. This does not permit a previously sent command to run again.
			_, _, _, existing := readDispatch(ctx, tx, d.ID)
			if existing != nil && !errors.Is(existing, sql.ErrNoRows) {
				return existing
			}
			d.InputSequences, err = effectInputSequences(ctx, tx, o, l.generation, inputs, errors.Is(existing, sql.ErrNoRows))
			if err != nil {
				return err
			}
			if command.Kind == agentproto.CommandTurnSteer {
				ids := make([]string, 0, len(inputs.Items))
				for _, item := range inputs.Items {
					ids = append(ids, item.QueueItemID)
				}
				command.NativeInputAdmission = &agentproto.NativeInputAdmissionCommand{Version: agentproto.NativeInputAdmissionProtocolVersion, ID: d.ID, InputIDs: ids}
			}
		}
		if err = l.validateNativeDispatch(d, command); err != nil {
			return err
		}
		if err = prepareDispatchWithCauseTx(ctx, tx, d, true); err != nil {
			return err
		}
		raw, err := json.Marshal(command)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(raw)
		if err = storeNativeCommandTx(ctx, tx, d, raw, hex.EncodeToString(hash[:])); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return Dispatch{}, agentproto.Command{}, err
	}
	return d, command, nil
}

func effectInputSequences(ctx context.Context, tx *sql.Tx, o Owner, generation int64, inputs *orchestrator.FrozenCommandInputs, checkAutomatic bool) ([]int64, error) {
	var checkpoint orchestrator.PrivateCheckpoint
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT data FROM private_checkpoint WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&raw); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		return nil, err
	}
	seenItems, seqs := map[string]bool{}, map[int64]bool{}
	for _, item := range inputs.Items {
		if item.QueueItemID == "" || seenItems[item.QueueItemID] {
			return nil, ErrJournalState
		}
		seenItems[item.QueueItemID] = true
		switch item.SourceKind {
		case state.QueueItemSourceUser:
			if len(item.SourceMessageIDs) == 0 {
				return nil, ErrJournalState
			}
			for _, source := range item.SourceMessageIDs {
				// A reaction refers to a source; it does not produce that source.
				// More than one producer is an ownership error, not a cue to use latest.
				var count int
				var seq sql.NullInt64
				err := tx.QueryRowContext(ctx, `SELECT count(*),min(i.seq) FROM ingress i JOIN private_applied a ON a.seq=i.seq WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND EXISTS (SELECT 1 FROM json_each(i.sources) WHERE value=?)`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, source).Scan(&count, &seq)
				if err != nil {
					return nil, err
				}
				if count != 1 || !seq.Valid {
					return nil, fmt.Errorf("source %q has no unique applied producer: %w", source, ErrJournalConflict)
				}
				seqs[seq.Int64] = true
			}
		case state.QueueItemSourceAutoWhip:
			// SourceMessageIDs may point to the original turn for reply routing;
			// automatic continuation never consumes that human input again.
			if checkAutomatic && !checkpoint.AutoWhip.Enabled {
				return nil, ErrEffectSuperseded
			}
		case state.QueueItemSourceAutoContinue:
			if item.AutoContinueEpisodeID == "" {
				return nil, ErrJournalState
			}
			if checkAutomatic && (!checkpoint.AutoContinue.Enabled || checkpoint.AutoContinue.Episode == nil || checkpoint.AutoContinue.Episode.EpisodeID != item.AutoContinueEpisodeID) {
				return nil, ErrEffectSuperseded
			}
		default:
			return nil, ErrJournalState
		}
	}
	// This sorts only consumption references, never native prompt segments.
	out := make([]int64, 0, len(seqs))
	for seq := range seqs {
		out = append(out, seq)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}
