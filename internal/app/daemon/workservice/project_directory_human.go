package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
	"net/url"
	"strconv"
	"strings"
)

type ProjectDirectoryInteractionTarget struct {
	ProjectID                   string `json:"project_id"`
	ExpectedProjectRevision     int64  `json:"expected_project_revision"`
	ExpectedDirectoryGeneration int64  `json:"expected_directory_generation"`
}

func directorySourceContainsURL(tx *workstore.Tx, message work.Message, container string) error {
	reject := func() error {
		return &workcmd.Error{Code: "invalid_argument", Message: "the selected directory URL must appear in the actual current human message or saved directory form"}
	}
	if message.PlatformKey == nil {
		return reject()
	}
	key := message.PlatformKey
	ingress, err := tx.GetIngressByPlatformMessage(key.Tenant, key.ChatID, key.MessageID)
	if err != nil {
		return err
	}
	if ingress.MessageID != message.ID || ingress.Envelope.SenderType != "user" || !rawDirectoryURLContains(ingress.Envelope.RawContent, container) {
		return reject()
	}
	return nil
}
func validateSelectedDirectoryURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return fmt.Errorf("请选择实际飞书资料目录链接")
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.HasSuffix(host, ".feishu.cn") && !strings.HasSuffix(host, ".larksuite.com") && !strings.HasSuffix(host, ".larkoffice.com") {
		return fmt.Errorf("请选择实际飞书资料目录链接")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if !(len(parts) == 3 && parts[0] == "drive" && parts[1] == "folder" || len(parts) == 2 && parts[0] == "wiki" || len(parts) == 3 && parts[0] == "wiki" && parts[1] == "space") {
		return fmt.Errorf("链接须是已有 Drive 文件夹、Wiki 父节点或明确选定的已有空间根")
	}
	return nil
}

// SelectProjectDirectoryFromInteraction is a trusted human entry, not a worker
// DTO with a self-asserted principal. It remains usable while initial creation
// is waiting on a platform call and no normal project worker may run yet.
func (s *Service) SelectProjectDirectoryFromInteraction(ctx context.Context, interactionID, viewID string) (Result, error) {
	var result Result
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		interaction, err := tx.GetHumanInteraction(interactionID)
		if err != nil {
			return err
		}
		view, err := tx.GetUIView(viewID)
		if err != nil {
			return err
		}
		if err = tx.ValidateViewInteraction(view, interaction); err != nil {
			return err
		}
		if view.Kind != "project.directory" || interaction.Envelope.Values["team_view"] != view.ID || interaction.Envelope.Values["action"] != "project.directory.select" {
			return workstore.ErrScopeDenied
		}
		container, ok := interaction.Envelope.Form["document_container"].(string)
		if !ok {
			return fmt.Errorf("请选择新资料目录")
		}
		if err = validateSelectedDirectoryURL(container); err != nil {
			return err
		}
		sourceBody, err := tx.GetContent(view.ScopeID, view.ID+":directory_target")
		if err != nil {
			return err
		}
		var target ProjectDirectoryInteractionTarget
		if err = json.Unmarshal(sourceBody.Data, &target); err != nil {
			return err
		}
		if target.ProjectID != view.TargetID || target.ExpectedProjectRevision != view.TargetRevision {
			return workstore.ErrScopeDenied
		}
		id := stableID("project-directory-human", interaction.ID)
		hash := stableID("project-directory-choice", view.ID, container)
		if old, e := tx.GetOperation(id); e == nil {
			if old.RequestHash != hash {
				return workstore.ErrIdempotencyConflict
			}
			return json.Unmarshal([]byte(old.ExternalReceipt), &result)
		} else if e != workstore.ErrNotFound {
			return e
		}
		evidenceData, err := json.Marshal(interaction.Envelope)
		if err != nil {
			return err
		}
		evidence := work.NewContent(id+":human-evidence", view.ScopeID, "application/json", evidenceData, s.now())
		if err = tx.PutContent(evidence); err != nil {
			return err
		}
		body := work.NewContent(id+":body", view.ScopeID, "text/plain", []byte("用户明确选择项目资料目录："+container), s.now())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		message := work.Message{ID: id + ":source", ScopeID: view.ScopeID, ConversationID: id, Source: work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}, BodyRef: body.ID, DerivedFrom: &work.SourceRef{Kind: "human_interaction", ID: interaction.ID, Version: strconv.FormatInt(interaction.Revision, 10), EvidenceRef: evidence.ID}, Revision: 1, CreatedAt: s.now()}
		if _, _, err = tx.AcceptMessage(message, nil); err != nil {
			return err
		}
		if err = tx.RequireProjectPrincipalAccess(view.ScopeID, interaction.PrincipalID, s.now()); err != nil {
			return err
		}
		req := ProjectDirectoryRequest{Key: interaction.ID, ProjectID: target.ProjectID, ExpectedProjectRevision: target.ExpectedProjectRevision, ExpectedDirectoryGeneration: target.ExpectedDirectoryGeneration, DocumentContainer: container, SourceMessageID: message.ID}
		raw, err := json.Marshal(req)
		if err != nil {
			return err
		}
		request := work.NewContent(id+":request", view.ScopeID, "application/json", raw, s.now())
		if err = tx.PutContent(request); err != nil {
			return err
		}
		op := work.Operation{ID: id, ScopeID: view.ScopeID, Actor: message.Source, Kind: "project.directory.select", TargetID: target.ProjectID, TargetGeneration: target.ExpectedDirectoryGeneration + 1, IdempotencyKey: interaction.ID, RequestRef: request.ID, RequestHash: hash, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if _, _, err = tx.PutOperation(op, 0); err != nil {
			return err
		}
		op.State = work.OperationSending
		op.Revision++
		if _, _, err = tx.PutOperation(op, 1); err != nil {
			return err
		}
		object, revision, err := s.applyProjectDirectorySelection(tx, req, view.ScopeID, view.Binding.RoleID, message)
		if err != nil {
			return err
		}
		result = Result{Status: "selection_saved", OperationID: id, ObjectID: object, Revision: revision}
		receipt, err := json.Marshal(result)
		if err != nil {
			return err
		}
		op.State = work.OperationConfirmed
		op.Revision++
		op.ExternalReceipt = string(receipt)
		op.UpdatedAt = s.now()
		_, _, err = tx.PutOperation(op, 2)
		return err
	})
	return result, err
}

func rawDirectoryURLContains(raw, value string) bool {
	var data any
	if json.Unmarshal([]byte(raw), &data) != nil {
		return false
	}
	var contains func(any) bool
	contains = func(v any) bool {
		switch v := v.(type) {
		case string:
			return strings.Contains(v, value)
		case []any:
			for _, item := range v {
				if contains(item) {
					return true
				}
			}
		case map[string]any:
			for _, item := range v {
				if contains(item) {
					return true
				}
			}
		}
		return false
	}
	return contains(data)
}
