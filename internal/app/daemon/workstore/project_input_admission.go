package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ProjectInputSource identifies a durable internal message and the facts used
// while materializing its prompt. The admission transaction rechecks them.
type ProjectInputSource struct {
	InputID         string
	InputRevision   int64
	MessageRevision int64
	TaskRevision    int64
	WasImplicit     bool
}
type ProjectSteerCandidate struct {
	ContextID string
	Input     work.Inbox
	Worker    work.WorkerSession
	Attempt   work.ExecutionAttempt
	Turn      NativeTurn
	Original  agentproto.Command
	Source    ProjectInputSource
}

// ProjectSteerCandidateFor binds only a unique business context: an explicit
// destination, the unique task context, or the member's unique main context.
func (t *Tx) ProjectSteerCandidateFor(id string, now time.Time) (ProjectSteerCandidate, error) {
	var result ProjectSteerCandidate
	input, err := t.GetInbox(id)
	if err != nil {
		return result, err
	}
	scope, err := t.GetScope(input.ScopeID)
	if err != nil {
		return result, err
	}
	if scope.Kind != work.ScopeProject || input.State != work.InboxPending || input.ContentState == work.InputContentPending || input.AvailableAt.After(now) || input.MessageID == "" {
		return result, ErrScopeDenied
	}
	msg, err := t.GetMessage(input.MessageID)
	if err != nil {
		return result, err
	}
	if _, err = t.authorizeProjectMessageSource(scope, input, msg, now); err != nil {
		return result, err
	}
	if err = t.requireProjectReady(scope.ProjectID, input.RecipientRoleID); err != nil {
		return result, err
	}
	if err = t.requireRoleInScope(scope.ID, input.RecipientRoleID); err != nil {
		return result, err
	}
	contexts, err := t.ListContexts(scope.ID)
	if err != nil {
		return result, err
	}
	var chosen *work.WorkContext
	for _, wc := range contexts {
		if wc.RoleID != input.RecipientRoleID || wc.Purpose == work.PurposeControl || wc.Purpose == work.PurposeReview {
			continue
		}
		if input.RecipientContextID != "" {
			if wc.ID != input.RecipientContextID {
				continue
			}
		} else if input.TaskID != "" {
			if wc.TaskID != input.TaskID {
				continue
			}
		} else if wc.TaskID != "" || wc.Workstream != "main" {
			continue
		}
		if wc.TaskID != input.TaskID || chosen != nil {
			return result, ErrScopeDenied
		}
		copy := wc
		chosen = &copy
	}
	if chosen == nil {
		return result, ErrNotFound
	}
	if msg.Source.Kind == work.ActorPrincipal && input.TaskID == "" && chosen.Workstream != "main" {
		return result, ErrScopeDenied
	}
	result.ContextID = chosen.ID
	worker, err := t.CurrentContextWorker(chosen.ID)
	if err != nil {
		return result, err
	}
	if worker.State != work.WorkerBusy || worker.Controller != work.ControllerAutomatic || (worker.Backend != work.BackendCodex && worker.Backend != work.BackendClaude) {
		return result, ErrScopeDenied
	}
	attempt, err := t.GetOccupiedAttemptForWorker(worker.ID)
	if err != nil {
		return result, err
	}
	if attempt.State != work.AttemptRunning || attempt.TaskID != input.TaskID {
		return result, ErrScopeDenied
	}
	var taskRevision int64
	if input.TaskID != "" {
		task, e := t.GetTask(input.TaskID)
		if e != nil {
			return result, e
		}
		if task.OwnerRoleID != worker.RoleID || task.RequirementsRevision != attempt.FrozenRequirementsRevision || task.AssignmentGeneration != attempt.FrozenAssignmentGeneration || task.State == work.TaskDone || task.State == work.TaskCancelled {
			return result, ErrConflict
		}
		taskRevision = task.Revision
	}
	admissions, err := t.ListAttemptInputAdmissions(attempt.ID)
	if err != nil {
		return result, err
	}
	for _, a := range admissions {
		if a.State == work.InputAdmissionRejected || a.State == work.InputAdmissionNotSent {
			for _, old := range a.InputIDs {
				if old == input.ID {
					return result, ErrScopeDenied
				}
			}
		}
	}
	pendingAdmission, err := t.HasPendingAttemptInputAdmissions(attempt.ID)
	if err != nil {
		return result, err
	}
	if pendingAdmission {
		return result, ErrConflict
	}
	turns, err := t.ListNativeTurns(attempt.ID)
	if err != nil {
		return result, err
	}
	var turn NativeTurn
	for _, v := range turns {
		if v.CarrierIncarnation == worker.CarrierIncarnation && v.SessionID == worker.NativeSessionID && v.Started && v.TerminalStatus == "" {
			if turn.TurnID != "" {
				return result, ErrConflict
			}
			turn = v
		}
	}
	if turn.TurnID == "" {
		return result, ErrNotFound
	}
	carrier, err := t.GetCarrier(worker.CarrierIncarnation)
	if err != nil {
		return result, err
	}
	if !carrier.Matches(worker) {
		return result, ErrConflict
	}
	original, err := t.NativeCommandPayload(attempt.CommandID, carrier.Incarnation)
	if err != nil {
		return result, err
	}
	implicit := input.RecipientContextID == ""
	result = ProjectSteerCandidate{ContextID: chosen.ID, Input: input, Worker: worker, Attempt: attempt, Turn: turn, Original: original, Source: ProjectInputSource{InputID: input.ID, InputRevision: input.Revision, MessageRevision: msg.Revision, TaskRevision: taskRevision, WasImplicit: implicit}}
	return result, nil
}

func (t *Tx) authorizeProjectInputAdmission(src ProjectInputSource, attempt work.ExecutionAttempt) (work.Actor, work.SourceRef, error) {
	var actor work.Actor
	input, err := t.GetInbox(src.InputID)
	if err != nil {
		return actor, work.SourceRef{}, err
	}
	if input.Revision != src.InputRevision || input.State != work.InboxPending || input.ScopeID != attempt.ScopeID || (input.RecipientContextID != attempt.ContextID && !(src.WasImplicit && input.RecipientContextID == "")) || input.TaskID != attempt.TaskID {
		return actor, work.SourceRef{}, ErrConflict
	}
	scope, err := t.GetScope(input.ScopeID)
	if err != nil {
		return actor, work.SourceRef{}, err
	}
	if src.WasImplicit {
		contexts, e := t.ListContexts(input.ScopeID)
		if e != nil {
			return actor, work.SourceRef{}, e
		}
		count := 0
		for _, wc := range contexts {
			if wc.RoleID != input.RecipientRoleID || wc.Purpose == work.PurposeControl || wc.Purpose == work.PurposeReview {
				continue
			}
			if input.TaskID != "" && wc.TaskID == input.TaskID || input.TaskID == "" && wc.TaskID == "" && wc.Workstream == "main" {
				count++
			}
		}
		if count != 1 {
			return actor, work.SourceRef{}, ErrConflict
		}
	}

	if scope.Kind != work.ScopeProject {
		return actor, work.SourceRef{}, ErrScopeDenied
	}
	if err = t.requireProjectReady(scope.ProjectID, input.RecipientRoleID); err != nil {
		return actor, work.SourceRef{}, err
	}
	if err = t.requireRoleInScope(scope.ID, input.RecipientRoleID); err != nil {
		return actor, work.SourceRef{}, err
	}
	msg, err := t.GetMessage(input.MessageID)
	if err != nil {
		return actor, work.SourceRef{}, err
	}
	if msg.Revision != src.MessageRevision {
		return actor, work.SourceRef{}, ErrConflict
	}
	if _, err = t.authorizeProjectMessageSource(scope, input, msg, time.Now().UTC()); err != nil {
		return actor, work.SourceRef{}, err
	}
	if msg.Source.Kind == work.ActorPrincipal {
		wc, e := t.GetContext(attempt.ContextID)
		if e != nil {
			return actor, work.SourceRef{}, e
		}
		if input.TaskID == "" && wc.Workstream != "main" {
			return actor, work.SourceRef{}, ErrScopeDenied
		}
	}
	if input.TaskID != "" {
		task, e := t.GetTask(input.TaskID)
		if e != nil {
			return actor, work.SourceRef{}, e
		}
		if task.Revision != src.TaskRevision || task.OwnerRoleID != input.RecipientRoleID || task.RequirementsRevision != attempt.FrozenRequirementsRevision || task.AssignmentGeneration != attempt.FrozenAssignmentGeneration || task.State == work.TaskDone || task.State == work.TaskCancelled {
			return actor, work.SourceRef{}, ErrConflict
		}
	}
	return msg.Source, work.SourceRef{Kind: "message", ID: msg.ID, Version: strconv.FormatInt(msg.Revision, 10), EvidenceRef: msg.BodyRef}, nil
}

// MarkProjectAdmissionNotSent records the transport's local pre-send proof;
// no native receipt is implied. Call only for a typed proof that transport was
// never invoked, never for a disconnect or ambiguous write error.
func (t *Tx) MarkProjectAdmissionNotSent(commandID, reason string) error {
	return t.atomic(func() error {
		v, err := t.GetAttemptInputAdmissionByCommand(commandID)
		if err != nil {
			return err
		}
		if v.State != work.InputAdmissionSending {
			return ErrConflict
		}
		op, err := t.GetOperation(commandID)
		if err != nil {
			return err
		}
		project := false
		for _, step := range op.Steps {
			if step.ID == "project_message_source" {
				project = true
			}
		}
		if !project || op.State != work.OperationSending {
			return ErrScopeDenied
		}
		for _, id := range v.InputIDs {
			input, e := t.GetInbox(id)
			if e != nil {
				return e
			}
			if input.State != work.InboxClaimed || input.ClaimedAttemptID != v.AttemptID {
				return ErrConflict
			}
			rev := input.Revision
			input.Revision++
			input.State = work.InboxPending
			input.ClaimedAttemptID = ""
			if e = t.PutInbox(input, rev); e != nil {
				return e
			}
		}
		rev := op.Revision
		op.Revision++
		op.State = work.OperationTerminal
		op.LastError = reason
		op.Steps = append(op.Steps, work.OperationStep{ID: "transport_not_sent", State: work.OperationConfirmed, Receipt: reason})
		if _, _, err = t.PutOperation(op, rev); err != nil {
			return err
		}
		v.State = work.InputAdmissionNotSent
		v.Revision++
		v.UpdatedAt = time.Now().UTC()
		_, err = t.tx.ExecContext(t.ctx, "UPDATE attempt_input_admissions SET payload=? WHERE id=?", mustJSON(v), v.ID)
		return err
	})
}

// RecoverProjectAdmissionsAfterRestart treats every prior in-flight project
// send as uncertain: the process may have crashed on either side of the write.
func (t *Tx) RecoverProjectAdmissionsAfterRestart() error {
	scopes, err := t.ListScopes()
	if err != nil {
		return err
	}
	for _, scope := range scopes {
		if scope.Kind != work.ScopeProject {
			continue
		}
		attempts, e := t.ListAttempts(scope.ID)
		if e != nil {
			return e
		}
		for _, attempt := range attempts {
			admissions, e := t.ListAttemptInputAdmissions(attempt.ID)
			if e != nil {
				return e
			}
			for _, a := range admissions {
				if a.State != work.InputAdmissionSending {
					continue
				}
				op, e := t.GetOperation(a.CommandID)
				if e != nil {
					return e
				}
				project := false
				for _, step := range op.Steps {
					if step.ID == "project_message_source" {
						project = true
					}
				}
				if project {
					if e = t.MarkAttemptInputAdmissionUnknown(a.CommandID, "daemon restarted during project input delivery; native receipt or verified stop required"); e != nil {
						return e
					}
				}
			}
		}
	}
	return nil
}

// authorizeProjectMessageSource distinguishes trusted role output from an SDK
// group message. A principal actor without the exact ingress provenance fails.
func (t *Tx) authorizeProjectMessageSource(scope work.WorkScope, input work.Inbox, msg work.Message, now time.Time) (work.SourceRef, error) {
	if msg.ScopeID != scope.ID || input.EventID != "message:"+msg.ID || msg.RecalledEvidenceRef != "" {
		return work.SourceRef{}, ErrScopeDenied
	}
	source := work.SourceRef{Kind: "message", ID: msg.ID, Version: strconv.FormatInt(msg.Revision, 10), EvidenceRef: msg.BodyRef}
	if msg.Source.Kind == work.ActorRole {
		operations, err := t.ListOperations(scope.ID)
		if err != nil {
			return source, err
		}
		for _, op := range operations {
			if op.Kind != "message.send" || op.State != work.OperationConfirmed || op.Actor != msg.Source || op.TargetID != msg.ID || op.RequestRef == "" {
				continue
			}
			body, err := t.GetContent(scope.ID, op.RequestRef)
			if err != nil {
				return source, err
			}
			var request struct {
				RecipientRoleID    string `json:"recipient_role_id"`
				RecipientContextID string `json:"recipient_context_id"`
				TaskID             string `json:"task_id"`
				Body               string `json:"body"`
			}
			if json.Unmarshal(body.Data, &request) != nil || request.RecipientRoleID != input.RecipientRoleID || request.RecipientContextID != "" && request.RecipientContextID != input.RecipientContextID || request.TaskID != "" && request.TaskID != input.TaskID {
				continue
			}
			messageBody, err := t.GetContent(scope.ID, msg.BodyRef)
			if err != nil {
				return source, err
			}
			if string(messageBody.Data) != request.Body {
				continue
			}
			return source, nil
		}
		return source, ErrScopeDenied
	}
	if msg.Source.Kind == work.ActorPrincipal && msg.DerivedFrom != nil && msg.DerivedFrom.Kind == "private_share" {
		if err := t.authorizePrivateProjectShare(scope, input, msg, now); err != nil {
			return source, err
		}
		return source, nil
	}
	if msg.Source.Kind != work.ActorPrincipal || msg.PlatformKey == nil {
		return work.SourceRef{}, ErrScopeDenied
	}

	raw, err := t.GetIngressByPlatformMessage(msg.PlatformKey.Tenant, msg.PlatformKey.ChatID, msg.PlatformKey.MessageID)
	if err != nil {
		return source, err
	}
	if raw.State != work.IngressProcessed || raw.MessageID != msg.ID || raw.Route.Kind != work.ScopeProject || raw.Route.ScopeID != scope.ID || raw.Route.ProjectID != scope.ProjectID || raw.Envelope.ChatType != "group" || raw.Envelope.SenderType != "user" {
		return source, ErrScopeDenied
	}
	identity, _ := json.Marshal([]string{raw.ID, input.RecipientRoleID})
	sum := sha256.Sum256(identity)
	if input.ID != "inbox:"+hex.EncodeToString(sum[:]) {
		return source, ErrScopeDenied
	}
	if input.TaskID != "" {
		task, e := t.GetTask(input.TaskID)
		if e != nil {
			return source, e
		}
		if task.OwnerRoleID != input.RecipientRoleID || task.State == work.TaskDone || task.State == work.TaskCancelled || task.State == work.TaskWaiting {
			return source, ErrConflict
		}
		mapping, e := t.GetPlatformMessageMapping(raw.Envelope.Tenant, raw.Envelope.ChatID, raw.Envelope.MessageID)
		if e != nil {
			return source, e
		}
		if mapping.IngressID != raw.ID || mapping.ScopeID != scope.ID || mapping.TaskID != input.TaskID || mapping.ChannelGeneration != raw.Route.BindingGeneration {
			return source, ErrScopeDenied
		}
		conversation, e := t.GetTaskConversation(scope.ID, input.TaskID)
		if e != nil {
			return source, e
		}
		if conversation.Tenant != raw.Envelope.Tenant || conversation.ChatID != raw.Envelope.ChatID || conversation.RootMessageID != mapping.RootMessageID || conversation.ChannelGeneration != mapping.ChannelGeneration {
			return source, ErrScopeDenied
		}
	}

	channel, err := t.GetProjectChannelBinding(raw.Envelope.Tenant, raw.Envelope.ChatID)
	if err != nil {
		return source, err
	}
	if !channel.Active || channel.ProjectID != scope.ProjectID || channel.BindingGeneration != raw.Route.BindingGeneration {
		return source, ErrScopeDenied
	}
	principal, _, err := t.ResolvePrincipal(work.IdentityEvidence{Tenant: raw.Envelope.Tenant, AppID: raw.Envelope.AppID, Generation: raw.Gateway.BotGeneration, UnionDomain: raw.Gateway.UnionDomain, IDs: raw.Envelope.SenderIDs, ObservationRef: raw.ID})
	if err != nil {
		return source, err
	}
	if principal.ID != msg.Source.ID {
		return source, ErrScopeDenied
	}
	observed := false
	for _, o := range msg.Observations {
		if o.AppID == raw.Gateway.AppID && o.IdentityGeneration == raw.Gateway.BotGeneration {
			observed = true
		}
	}
	if !observed {
		return source, ErrScopeDenied
	}
	if err = t.RequireProjectPrincipalAccess(scope.ID, principal.ID, now); err != nil {
		return source, err
	}
	return source, nil
}

// ListProjectPendingInArrivalOrder keeps persisted insertion order as the tie
// breaker; IDs are opaque hashes and must never be interpreted as time.
func (t *Tx) ListProjectPendingInArrivalOrder(scopeID, roleID string) ([]work.Inbox, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT i.payload,coalesce(m.payload,'') FROM inbox i LEFT JOIN messages m ON m.id=json_extract(i.payload,'$.message_id') AND m.scope_id=i.scope_id WHERE i.scope_id=? AND i.role_id=? AND i.state='pending' AND coalesce(json_extract(i.payload,'$.shell_operation_id'),'')='' ORDER BY i.rowid`, scopeID, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type ordered struct {
		input    work.Inbox
		accepted time.Time
	}
	var values []ordered
	for rows.Next() {
		var raw, body string
		var v ordered
		if err = rows.Scan(&raw, &body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &v.input); err != nil {
			return nil, err
		}
		v.accepted = v.input.AvailableAt
		if body != "" {
			var msg work.Message
			if err = json.Unmarshal([]byte(body), &msg); err != nil {
				return nil, err
			}
			if !msg.CreatedAt.IsZero() {
				v.accepted = msg.CreatedAt
			}
		}
		values = append(values, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(values, func(i, j int) bool { return values[i].accepted.Before(values[j].accepted) })
	items := make([]work.Inbox, 0, len(values))
	for _, v := range values {
		items = append(items, v.input)
	}
	return items, nil
}
