package privatechat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// prepareNativeDispatch freezes the exact controller-selected source sequences
// and canonical native command in one commit. No queue prefix is inferred.
func (l *NativeLease) prepareNativeDispatch(ctx context.Context, d Dispatch, command agentproto.Command) error {
	if err := l.validateNativeDispatch(d, command); err != nil {
		return err
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	return l.with(ctx, func() error {
		tx, e := l.journal.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = prepareDispatchTx(ctx, tx, d); e != nil {
			return e
		}
		if e = storeNativeCommandTx(ctx, tx, d, raw, digest); e != nil {
			return e
		}
		return tx.Commit()
	})
}
func nativeExecutionRequired(kind agentproto.CommandKind) bool {
	switch kind {
	case agentproto.CommandPromptSend, agentproto.CommandTurnSteer, agentproto.CommandTurnInterrupt, agentproto.CommandRequestRespond, agentproto.CommandThreadShellCommand, agentproto.CommandReviewStart:
		return true
	}
	return false
}

func (l *NativeLease) validateNativeDispatch(d Dispatch, command agentproto.Command) error {
	if d.Lane == "" {
		d.Lane = "input"
	}
	if l == nil || d.Owner != l.surface || d.Generation != l.generation || command.Execution != nil || command.NativeBinding == nil || command.NativeBinding.Owner != l.native || command.NativeBinding.Validate(nativeExecutionRequired(command.Kind)) != nil || command.CommandID != d.CommandID || d.Session.CarrierID != l.native.CarrierIncarnation || d.Session.CarrierGeneration != l.native.InstanceGeneration || d.Session.Backend != string(l.native.Backend) || d.Session.Namespace != l.native.HistoryNamespace {
		return ErrJournalState
	}
	if command.Kind == agentproto.CommandPromptSend {
		if d.Lane != "input" || command.Target.ExecutionMode == agentproto.PromptExecutionModeStartNew && (d.Mode != "start" || command.Target.ThreadID != "") || command.Target.ExecutionMode == agentproto.PromptExecutionModeResumeExisting && (d.Mode != "resume" || command.Target.ThreadID != d.Session.SessionID) || command.Target.ExecutionMode != agentproto.PromptExecutionModeStartNew && command.Target.ExecutionMode != agentproto.PromptExecutionModeResumeExisting {
			return ErrJournalState
		}
	}
	if command.Kind == agentproto.CommandTurnInterrupt && (d.Lane != "control" || d.Mode != "resume" || command.Target.ThreadID != d.Session.SessionID || command.Target.TurnID == "") {
		return ErrJournalState
	}
	if command.Kind == agentproto.CommandTurnSteer {
		if d.Lane != "input" || d.Mode != "resume" || command.Target.ThreadID != d.Session.SessionID || command.Target.TurnID == "" || command.NativeInputAdmission == nil || command.NativeInputAdmission.Version != agentproto.NativeInputAdmissionProtocolVersion || command.NativeInputAdmission.ID != d.ID || len(command.NativeInputAdmission.InputIDs) == 0 {
			return ErrJournalState
		}
	}

	return nil
}

func storeNativeCommandTx(ctx context.Context, tx *sql.Tx, d Dispatch, raw []byte, digest string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO native_commands(dispatch_id,command_id,payload,payload_hash) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, d.ID, d.CommandID, raw, digest); err != nil {
		return err
	}
	var oldRaw []byte
	var oldHash, oldID string
	if err := tx.QueryRowContext(ctx, `SELECT command_id,payload,payload_hash FROM native_commands WHERE dispatch_id=?`, d.ID).Scan(&oldID, &oldRaw, &oldHash); err != nil {
		return err
	}
	if oldID != d.CommandID || oldHash != digest || string(oldRaw) != string(raw) {
		return ErrJournalConflict
	}
	return nil
}

// PrepareHostInspection is the only dispatch not derived from a private core
// effect. It permits an authenticated owner's read-only runtime inventory only.
func (l *NativeLease) PrepareHostInspection(ctx context.Context, d Dispatch, command agentproto.Command) error {
	if command.Kind != agentproto.CommandRuntimeInspect || d.Lane != "inspect" || len(d.InputSequences) != 0 || command.NativeBinding == nil || command.NativeBinding.ExecutionID != "" {
		return ErrJournalState
	}
	return l.prepareNativeDispatch(ctx, d, command)
}

// PrepareHostStop is restricted to a host-selected exact accepted native turn;
// the host must have persisted its stop intent and proved the turn binding.
func (l *NativeLease) PrepareHostStop(ctx context.Context, d Dispatch, command agentproto.Command) error {
	if command.Kind != agentproto.CommandTurnInterrupt || d.Lane != "control" || len(d.InputSequences) != 0 || command.NativeBinding == nil || command.NativeBinding.Validate(true) != nil || command.Target.ThreadID == "" || command.Target.TurnID == "" {
		return ErrJournalState
	}
	if err := l.validateNativeDispatch(d, command); err != nil {
		return err
	}
	proven, err := l.BindingForAcceptedTurn(ctx, command.Target.ThreadID, command.Target.TurnID)
	if err != nil {
		return err
	}
	if proven == nil || *proven != *command.NativeBinding {
		return ErrJournalConflict
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	return l.with(ctx, func() error {
		tx, e := l.journal.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = prepareDispatchWithCauseTx(ctx, tx, d, true); e != nil {
			return e
		}
		if e = storeNativeCommandTx(ctx, tx, d, raw, hex.EncodeToString(sum[:])); e != nil {
			return e
		}
		return tx.Commit()
	})
}

// PrepareHostExit registers only an owner-wide wrapper exit command; native
// quiescence and OS process exit must still be proved separately.
func (l *NativeLease) PrepareHostExit(ctx context.Context, d Dispatch, command agentproto.Command) error {
	if command.Kind != agentproto.CommandProcessExit || d.Lane != "inspect" || len(d.InputSequences) != 0 || command.NativeBinding == nil || command.NativeBinding.ExecutionID != "" || command.Target.ThreadID != "" || command.Target.TurnID != "" {
		return ErrJournalState
	}
	return l.prepareNativeDispatch(ctx, d, command)
}
