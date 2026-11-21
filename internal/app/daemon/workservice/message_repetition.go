package workservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Repetition detection is deliberately factual: same sender/recipient, exact
// text, conversation and task revision, with no newer human instruction. It
// does not pretend to judge the merit of different technical arguments.
func (s *Service) guardRepeatedMessage(tx *workstore.Tx, c Caller, req SendMessageRequest) (string, error) {
	conversationTaskID := req.RelatedTaskID
	if conversationTaskID == "" {
		conversationTaskID = req.TaskID
	}
	humanRevision, err := tx.ConversationHumanRevision(c.Worker.ScopeID, req.ConversationID, conversationTaskID)
	if err != nil {
		return "", err
	}
	taskRevision := int64(0)
	if req.TaskID != "" {
		task, e := tx.GetTask(req.TaskID)
		if e != nil {
			return "", e
		}
		if task.ScopeID != c.Worker.ScopeID {
			return "", workstore.ErrScopeDenied
		}
		taskRevision = task.Revision
	}
	fingerprint := struct {
		Sender, Recipient, Context, Conversation, Task, Body string
		HumanRevision, TaskRevision                          int64
		RelatedTaskID                                        string `json:",omitempty"`
		RelatedTaskRevision                                  int64  `json:",omitempty"`
	}{c.Worker.RoleID, req.RecipientRoleID, req.RecipientContextID, req.ConversationID, req.TaskID, strings.TrimSpace(req.Body), humanRevision, taskRevision, req.RelatedTaskID, 0}
	if req.RelatedTaskID != "" {
		related, err := tx.GetTask(req.RelatedTaskID)
		if err != nil {
			return "", err
		}
		fingerprint.RelatedTaskRevision = related.Revision
	}
	raw, err := json.Marshal(fingerprint)
	if err != nil {
		return "", err
	}
	id := stableID("exchange", c.Worker.ScopeID, string(raw))
	op, err := tx.GetOperation(id)
	if errors.Is(err, workstore.ErrNotFound) {
		content := work.NewContent(id+":request", c.Worker.ScopeID, "application/json", raw, s.now())
		if err = tx.PutContent(content); err != nil {
			return "", err
		}
		op = work.Operation{ID: id, ScopeID: c.Worker.ScopeID, Actor: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, Kind: "collaboration.exchange", TargetID: req.RecipientRoleID, TargetGeneration: 1, IdempotencyKey: id, RequestHash: content.SHA256, RequestRef: content.ID, State: work.OperationConfirmed, ExternalReceipt: id + ":recorded", Steps: []work.OperationStep{{ID: "delivery_1", State: work.OperationConfirmed, Receipt: req.Key}}, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		_, _, err = tx.PutOperation(op, 0)
		return "", err
	}
	if err != nil {
		return "", err
	}
	noticeID := stableID("coordination_block", id)
	if len(op.Steps) >= 3 {
		return noticeID, nil
	}
	previous := op.Revision
	op.Revision++
	op.UpdatedAt = s.now()
	op.Steps = append(op.Steps, work.OperationStep{ID: fmt.Sprintf("delivery_%d", len(op.Steps)+1), State: work.OperationConfirmed, Receipt: req.Key})
	if _, _, err = tx.PutOperation(op, previous); err != nil {
		return "", err
	}
	if len(op.Steps) < 3 {
		return "", nil
	}
	scope, err := tx.GetScope(c.Worker.ScopeID)
	if err != nil {
		return "", err
	}
	recipient := c.Worker.RoleID
	if scope.Kind == work.ScopeProject {
		project, e := tx.GetProject(scope.ProjectID)
		if e != nil {
			return "", e
		}
		recipient = project.LeaderRoleID
	}
	text := "同一事项的相同消息已经反复发送，期间没有新的人工要求或任务状态变化。本次未再次唤醒接收者，请负责人检查对方是否仍在处理、是否缺少信息或需要调整分工。不要仅换消息编号继续重复；有新信息时可正常发送。\n原始交接记录：" + id + "\n任务：" + conversationTaskID + "\n发送角色：" + c.Worker.RoleID + "\n接收角色：" + req.RecipientRoleID
	body := work.NewContent(noticeID+":body", scope.ID, "text/plain; charset=utf-8", []byte(text), s.now())
	if err = tx.PutContent(body); err != nil {
		return "", err
	}
	message := work.Message{ID: noticeID, ScopeID: scope.ID, ConversationID: req.ConversationID, Source: work.Actor{Kind: work.ActorSystem, ID: "collaboration-repetition"}, BodyRef: body.ID, Revision: 1, CreatedAt: s.now()}
	input := work.Inbox{ID: noticeID + ":inbox", ScopeID: scope.ID, RelatedTaskID: conversationTaskID, RecipientRoleID: recipient, EventID: noticeID, MessageID: noticeID, State: work.InboxPending, ContentState: work.InputContentReady, AvailableAt: s.now(), Revision: 1}
	_, _, err = tx.AcceptMessage(message, []work.Inbox{input})
	return noticeID, err
}
