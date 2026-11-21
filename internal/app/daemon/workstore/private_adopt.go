package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"time"
)

type AdoptPrivateInstructionRequest struct {
	Key     string
	Source  PrivateActionSource
	Title   string
	Summary string
	Now     time.Time
}
type PrivateInstructionAdoption struct {
	ID               string `json:"id"`
	PersonalScopeID  string `json:"personal_scope_id"`
	ProjectScopeID   string `json:"project_scope_id"`
	SourceMessageID  string `json:"source_message_id"`
	ProjectContextID string `json:"project_context_id"`
	TaskID           string `json:"task_id"`
	MessageID        string `json:"message_id"`
	InboxID          string `json:"inbox_id"`
	RequestHash      string `json:"request_hash"`
}

func (t *Tx) AdoptPrivateInstruction(req AdoptPrivateInstructionRequest) (result PrivateInstructionAdoption, created bool, err error) {
	err = t.atomic(func() error {
		if req.Source.MessageID == "" || req.Source.RunRef.RunID == "" || strings.TrimSpace(req.Key) == "" || strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Summary) == "" || req.Now.IsZero() {
			return fmt.Errorf("explicit project instruction needs admitted human source, title and minimized summary")
		}
		personal, _, e := t.ValidatePrivateAction(req.Source)
		if e != nil {
			return e
		}
		attempt, e := t.GetAttemptByRunRef(req.Source.RunRef.RunID)
		if e != nil {
			return e
		}
		front, e := t.GetContext(attempt.ContextID)
		if e != nil {
			return e
		}
		if front.ScopeID != personal.ID || front.ProjectViewScopeID == "" || front.ProjectContextID == "" {
			return ErrScopeDenied
		}
		if e = t.RequireProjectPrincipalAccess(front.ProjectViewScopeID, personal.PrincipalID, req.Now); e != nil {
			return e
		}
		projectScope, e := t.GetScope(front.ProjectViewScopeID)
		if e != nil {
			return e
		}
		if e = t.requireProjectReady(projectScope.ProjectID, personal.RoleID); e != nil {
			return e
		}
		context, e := t.GetContext(front.ProjectContextID)
		if e != nil {
			return e
		}
		if context.ScopeID != projectScope.ID || context.RoleID != personal.RoleID || context.Purpose == work.PurposeControl {
			return ErrScopeDenied
		}
		sum := sha256.Sum256([]byte(mustJSON([]string{req.Key, req.Title, req.Summary, front.ID, front.ProjectContextID})))
		hash := hex.EncodeToString(sum[:])
		var previous string
		e = t.tx.QueryRowContext(t.ctx, "SELECT payload FROM private_instruction_adoptions WHERE personal_scope_id=? AND project_scope_id=? AND source_message_id=?", personal.ID, projectScope.ID, req.Source.MessageID).Scan(&previous)
		if e == nil {
			if e = json.Unmarshal([]byte(previous), &result); e != nil {
				return e
			}
			if result.RequestHash != hash {
				return ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		identity := sha256.Sum256([]byte(mustJSON([]string{"private-adoption", personal.ID, projectScope.ID, req.Source.MessageID})))
		id := "private-adoption:" + hex.EncodeToString(identity[:])
		result = PrivateInstructionAdoption{ID: id, PersonalScopeID: personal.ID, ProjectScopeID: projectScope.ID, SourceMessageID: req.Source.MessageID, ProjectContextID: id + ":context", TaskID: id + ":task", MessageID: id + ":message", InboxID: id + ":input", RequestHash: hash}
		content := work.NewContent(id+":body", projectScope.ID, "text/plain; charset=utf-8", []byte(req.Summary), req.Now)
		if e = t.PutContent(content); e != nil {
			return e
		}
		// The public reference is opaque. Only this private mapping retains the source
		// message; project content APIs cannot follow it into personal history.
		provenance := work.SourceRef{Kind: "private_instruction_adoption", ID: id, Version: "1", EvidenceRef: id}
		message := work.Message{ID: result.MessageID, ScopeID: projectScope.ID, ConversationID: context.Workstream, Source: work.Actor{Kind: work.ActorPrincipal, ID: personal.PrincipalID}, BodyRef: content.ID, DerivedFrom: &provenance, Revision: 1, CreatedAt: req.Now}
		if e = t.putMessage(message, 0); e != nil {
			return e
		}
		business := work.SourceRef{Kind: "message", ID: message.ID, Version: "1", EvidenceRef: content.ID}
		task := work.Task{ID: result.TaskID, ScopeID: projectScope.ID, Title: req.Title, OwnerRoleID: personal.RoleID, State: work.TaskTodo, RequirementsRevision: 1, AssignmentGeneration: 1, BusinessSource: &business, Revision: 1, CreatedAt: req.Now, UpdatedAt: req.Now}
		if e = t.PutTask(task, 0); e != nil {
			return e
		}
		taskContext := privateAdoptionTaskContext(result.ProjectContextID, task, req.Now)
		if e = t.PutContext(taskContext, 0); e != nil {
			return e
		}
		input := work.Inbox{ID: result.InboxID, ScopeID: projectScope.ID, TaskID: task.ID, RecipientRoleID: personal.RoleID, RecipientContextID: taskContext.ID, MessageID: message.ID, EventID: id, State: work.InboxPending, ContentState: work.InputContentReady, AvailableAt: req.Now, Revision: 1}
		if e = t.PutInbox(input, 0); e != nil {
			return e
		}
		_, e = t.tx.ExecContext(t.ctx, "INSERT INTO private_instruction_adoptions(id,personal_scope_id,project_scope_id,source_message_id,target_task_id,payload) VALUES(?,?,?,?,?,?)", id, personal.ID, projectScope.ID, req.Source.MessageID, task.ID, mustJSON(result))
		created = e == nil
		return e
	})
	return
}

func privateAdoptionTaskContext(id string, task work.Task, now time.Time) work.WorkContext {
	return work.WorkContext{ID: id, ScopeID: task.ScopeID, RoleID: task.OwnerRoleID, TaskID: task.ID, Workstream: "task:" + task.ID, Revision: 1, CreatedAt: now, UpdatedAt: now}
}
