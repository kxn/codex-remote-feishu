package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaPrivateShellOperationsV1 = `CREATE TABLE IF NOT EXISTS private_shell_operations(id TEXT PRIMARY KEY,command_id TEXT NOT NULL UNIQUE,payload TEXT NOT NULL CHECK(json_valid(payload)));`

type PrivateShellOperation struct {
	Acknowledged       bool                        `json:"acknowledged,omitempty"`
	TurnStartedSource  *work.SourceRef             `json:"turn_started_source,omitempty"`
	TurnTerminalSource *work.SourceRef             `json:"turn_terminal_source,omitempty"`
	ID                 string                      `json:"id"`
	CommandID          string                      `json:"command_id"`
	InputID            string                      `json:"input_id"`
	InputIDs           []string                    `json:"input_ids"`
	ScopeID            string                      `json:"scope_id"`
	ContextID          string                      `json:"context_id"`
	PrincipalID        string                      `json:"principal_id"`
	Binding            work.RoleBotBinding         `json:"binding"`
	Execution          agentproto.ExecutionBinding `json:"execution"`
	FrozenExecution    agentproto.ExecutionBinding `json:"frozen_execution"`
	Incarnation        string                      `json:"incarnation"`
	SessionID          string                      `json:"session_id"`
	ActualTurnID       string                      `json:"actual_turn_id,omitempty"`
	ItemID             string                      `json:"item_id,omitempty"`
	State              string                      `json:"state"`
	ReceiptRef         string                      `json:"receipt_ref,omitempty"`
	SourceRef          work.SourceRef              `json:"source_ref"`
	LastError          string                      `json:"last_error,omitempty"`
	Revision           int64                       `json:"revision"`
	CreatedAt          time.Time                   `json:"created_at"`
	UpdatedAt          time.Time                   `json:"updated_at"`
}

type BeginPrivateShellOperationRequest struct {
	ID       string
	Source   PrivateActionSource
	InputID  string
	InputIDs []string
	Command  agentproto.Command
	Now      time.Time
}

func (t *Tx) GetPrivateShellOperation(id string) (v PrivateShellOperation, err error) {
	var raw string
	err = t.tx.QueryRowContext(t.ctx, "SELECT payload FROM private_shell_operations WHERE id=? OR command_id=?", id, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &v)
	}
	return
}
func (t *Tx) savePrivateShellOperation(v PrivateShellOperation, expected int64) error {
	if v.Revision != expected+1 {
		return ErrConflict
	}
	if expected == 0 {
		_, err := t.tx.ExecContext(t.ctx, "INSERT INTO private_shell_operations(id,command_id,payload) VALUES(?,?,?)", v.ID, v.CommandID, mustJSON(v))
		return err
	}
	result, err := t.tx.ExecContext(t.ctx, "UPDATE private_shell_operations SET payload=? WHERE id=? AND json_extract(payload,'$.revision')=?", mustJSON(v), v.ID, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrConflict
	}
	return err
}

// Begin reserves original inputs without claiming them into a model attempt.
// The immutable source and frozen active tuple authorize only this one send.
func (t *Tx) BeginPrivateShellOperation(req BeginPrivateShellOperationRequest) (v PrivateShellOperation, created bool, err error) {
	err = t.atomic(func() error {
		old, e := t.GetPrivateShellOperation(req.ID)
		if e == nil {
			op, e := t.GetOperation(old.CommandID)
			if e != nil {
				return e
			}
			raw, e := json.Marshal(req.Command)
			if e != nil {
				return e
			}
			if req.Source.PersonalScopeID != old.ScopeID || req.Source.ReactionID != old.SourceRef.ID || old.InputID != req.InputID || old.CommandID != req.Command.CommandID || work.NewContent("check", old.ScopeID, "application/json", raw, req.Now).SHA256 != op.RequestHash {
				return ErrIdempotencyConflict
			}
			v = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		scope, source, e := t.ValidatePrivateAction(req.Source)
		if e != nil {
			return e
		}
		if e = t.AssertPersonalMigrationOpen(scope.ID); e != nil {
			return e
		}
		action, e := t.GetPrivateReactionAction(req.Source.ReactionID)
		if e != nil {
			return e
		}
		command := req.Command
		owner := action.Execution
		owner.AttemptID = ""
		owner.RunRef = ""
		if req.ID == "" || req.Now.IsZero() || scope.Kind != work.ScopePersonal || action.Emoji != "APPLAUSE" || action.State != "prepared" || req.InputID != action.InputID || command.CommandID == "" || command.Kind != agentproto.CommandThreadShellCommand || command.ShellCommand.OperationID != req.ID || command.Execution == nil || *command.Execution != owner || command.Target.ThreadID != action.SessionID || command.Target.TurnID != action.TurnID || command.NativeInputAdmission != nil || command.ShellCommand.Command != "" || !validPrivateShellBundle(command.ShellCommand.Payload) {
			return ErrScopeDenied
		}
		ids := req.InputIDs
		if len(ids) == 0 {
			ids = []string{req.InputID}
		}
		if !reflect.DeepEqual(ids, action.InputIDs) {
			return ErrScopeDenied
		}
		var frozenInputs []work.Inbox
		seen := map[string]bool{}
		found := false
		for _, id := range ids {
			if seen[id] {
				return ErrScopeDenied
			}
			seen[id] = true
			found = found || id == req.InputID
			input, e := t.GetInbox(id)
			if e != nil {
				return e
			}
			if input.ScopeID != scope.ID || input.RecipientContextID != action.ContextID || input.RecipientRoleID != scope.RoleID || input.State != work.InboxPending || input.ShellOperationID != "" || input.ClaimedAttemptID != "" || input.ControlOfInputID != "" || input.SessionGoalIntentID != "" || input.AutomationEpisodeID != "" || input.ContentState == work.InputContentPending || (id != req.InputID && !input.AwaitingText) {
				return ErrScopeDenied
			}
			message, e := t.GetMessage(input.MessageID)
			if e != nil {
				return e
			}
			frozenInputs = append(frozenInputs, input)
			if message.Source != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) || message.RecalledEvidenceRef != "" {
				return ErrScopeDenied
			}
		}
		if !found {
			return ErrScopeDenied
		}
		{
			input, e := t.GetInbox(req.InputID)
			if e != nil {
				return e
			}
			cluster, e := t.PendingInputMediaCluster(input)
			if e != nil {
				return e
			}
			if len(cluster) != len(ids) {
				return ErrScopeDenied
			}
			for _, item := range cluster {
				if !seen[item.ID] {
					return ErrScopeDenied
				}
			}
		}
		if within, e := t.MediaBatchWithinBudget(frozenInputs); e != nil {
			return e
		} else if !within {
			return ErrScopeDenied
		}
		if e = t.validatePrivateShellPayloadSources(command.ShellCommand.Payload, scope.ID, frozenInputs); e != nil {
			return e
		}
		raw, e := json.Marshal(command)
		if e != nil {
			return e
		}
		body := work.NewContent(req.ID+":body", scope.ID, "application/json", raw, req.Now)
		if e = t.PutContent(body); e != nil {
			return e
		}
		op := work.Operation{ID: command.CommandID, ScopeID: scope.ID, ContextID: action.ContextID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, Kind: "native.private.shell", TargetID: owner.WorkerSessionID, TargetGeneration: owner.SessionGeneration, IdempotencyKey: req.ID, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationSending, Revision: 1, CreatedAt: req.Now, UpdatedAt: req.Now, Steps: []work.OperationStep{{ID: "human_source", State: work.OperationConfirmed, Receipt: mustJSON(source)}}}
		if _, _, e = t.PutOperation(op, 0); e != nil {
			return e
		}
		v = PrivateShellOperation{ID: req.ID, CommandID: command.CommandID, InputID: req.InputID, InputIDs: append([]string(nil), ids...), ScopeID: scope.ID, ContextID: action.ContextID, PrincipalID: scope.PrincipalID, Binding: action.Binding, Execution: owner, FrozenExecution: action.Execution, Incarnation: action.Incarnation, SessionID: action.SessionID, State: "sending", SourceRef: source, Revision: 1, CreatedAt: req.Now, UpdatedAt: req.Now}
		if e = t.savePrivateShellOperation(v, 0); e != nil {
			return e
		}
		for _, id := range ids {
			input, e := t.GetInbox(id)
			if e != nil {
				return e
			}
			before := input.Revision
			input.Revision++
			input.ShellOperationID = v.ID
			if e = t.PutInbox(input, before); e != nil {
				return e
			}
		}
		if e = t.ValidatePrivateShellCommand(command, v.Incarnation); e != nil {
			return e
		}
		if e = t.FinishPrivateReactionAction(action.ID, ""); e != nil {
			return e
		}
		created = true
		return nil
	})
	return
}

// ValidatePrivateShellCommand is the sole narrow exception permitting an
// authenticated human operation to register an owner-only native command.
func (t *Tx) ValidatePrivateShellCommand(command agentproto.Command, incarnation string) error {
	v, err := t.GetPrivateShellOperation(command.CommandID)
	if err != nil {
		return err
	}
	if v.State != "sending" || v.Incarnation != incarnation || command.Execution == nil || *command.Execution != v.Execution || command.ShellCommand.OperationID != v.ID || command.Kind != agentproto.CommandThreadShellCommand {
		return ErrScopeDenied
	}
	if _, e := t.GetNativeCommand(v.CommandID); e == nil {
		return ErrConflict
	} else if !errors.Is(e, ErrNotFound) {
		return e
	}
	op, err := t.GetOperation(v.CommandID)
	if err != nil {
		return err
	}
	if op.Kind != "native.private.shell" || op.Actor != (work.Actor{Kind: work.ActorPrincipal, ID: v.PrincipalID}) || op.ScopeID != v.ScopeID || op.ContextID != v.ContextID || op.TargetID != v.Execution.WorkerSessionID || op.TargetGeneration != v.Execution.SessionGeneration {
		return ErrScopeDenied
	}
	canonical, err := t.NativeCommandPayload(command.CommandID, incarnation)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(canonical, command) {
		return ErrIdempotencyConflict
	}
	action, err := t.GetPrivateReactionAction(v.SourceRef.ID)
	if err != nil {
		return err
	}
	if action.Execution != v.FrozenExecution || action.Incarnation != v.Incarnation || action.InputID != v.InputID || action.PrincipalID != v.PrincipalID || action.Emoji != "APPLAUSE" {
		return ErrScopeDenied
	}
	if _, _, err = t.ValidatePrivateReactionAction(action.ID); err != nil {
		return err
	}
	for _, id := range v.InputIDs {
		input, e := t.GetInbox(id)
		if e != nil {
			return e
		}
		if input.ShellOperationID != v.ID || input.State != work.InboxPending {
			return ErrConflict
		}
	}
	return t.privateShellSendAllowed(v)
}

func (t *Tx) MarkPrivateShellOperationUnknown(id, reason string) error {
	return t.changePrivateShellSendState(id, reason, false)
}
func (t *Tx) RejectPrivateShellBeforeSend(id, reason string) error {
	return t.changePrivateShellSendState(id, reason, true)
}
func (t *Tx) changePrivateShellSendState(id, reason string, unsent bool) error {
	return t.atomic(func() error {
		v, err := t.GetPrivateShellOperation(id)
		if err != nil {
			return err
		}
		if v.State == "completed" || v.State == "rejected" || v.State == "running" {
			return nil
		}
		if unsent {
			if _, err = t.GetNativeCommand(v.CommandID); err == nil {
				return fmt.Errorf("registered shell write is not proven unsent")
			} else if !errors.Is(err, ErrNotFound) {
				return err
			}
		}
		before := v.Revision
		v.Revision++
		v.LastError = reason
		v.UpdatedAt = time.Now().UTC()
		v.State = "unknown"
		if unsent {
			v.State = "rejected"
		}
		if err = t.savePrivateShellOperation(v, before); err != nil {
			return err
		}
		op, err := t.GetOperation(v.CommandID)
		if err != nil {
			return err
		}
		old := op.Revision
		op.Revision++
		op.LastError = reason
		op.UpdatedAt = v.UpdatedAt
		op.State = work.OperationUnknown
		if unsent {
			op.State = work.OperationTerminal
		}
		if _, _, err = t.PutOperation(op, old); err != nil {
			return err
		}
		if unsent {
			return t.releasePrivateShellInputs(v)
		}
		return nil
	})
}
func (t *Tx) releasePrivateShellInputs(v PrivateShellOperation) error {
	for _, id := range v.InputIDs {
		input, err := t.GetInbox(id)
		if err != nil {
			return err
		}
		if input.ShellOperationID != v.ID {
			return ErrConflict
		}
		before := input.Revision
		input.Revision++
		input.ShellOperationID = ""
		if err = t.PutInbox(input, before); err != nil {
			return err
		}
	}
	return nil
}
