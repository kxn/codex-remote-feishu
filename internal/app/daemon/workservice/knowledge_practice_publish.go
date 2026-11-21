package workservice

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PracticePublishRequest struct {
	Key                    string                  `json:"key"`
	Kind                   work.KnowledgeOwnerKind `json:"kind"`
	ID                     string                  `json:"id"`
	Version                string                  `json:"version"`
	Purpose                string                  `json:"purpose"`
	Applicability          string                  `json:"applicability"`
	Exclusions             string                  `json:"exclusions"`
	Inputs                 []string                `json:"inputs"`
	Steps                  []string                `json:"steps"`
	Outputs                []string                `json:"outputs"`
	Escalation             string                  `json:"escalation"`
	RequiredCapabilities   []string                `json:"required_capabilities"`
	Content                string                  `json:"content"`
	SharingSourceMessageID string                  `json:"sharing_source_message_id"`
}

type PracticePublishResult struct {
	Result
	Owner          work.KnowledgeOwner `json:"owner"`
	Version        string              `json:"version"`
	SharingGrantID string              `json:"sharing_grant_id,omitempty"`
}

func (s *Service) PracticePublish(ctx context.Context, token, runRef string, req PracticePublishRequest) (PracticePublishResult, error) {
	var result PracticePublishResult
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		owner := work.KnowledgeOwner{Kind: req.Kind}
		switch owner.Kind {
		case work.KnowledgeRole:
			owner.RoleID = c.Worker.RoleID
		case work.KnowledgeTeam:
			if scope.Kind != work.ScopeProject {
				return workauth.ErrUnauthorized
			}
			owner.TeamID = scope.TeamID
		case work.KnowledgeGlobal:
		default:
			return fmt.Errorf("explicit practice library kind is required")
		}
		result.Owner = owner
		result.Version = req.Version
		target := stableID("practice", string(owner.Kind), owner.TeamID, owner.RoleID, req.ID, req.Version)
		result.SharingGrantID = stableID("share", target, req.SharingSourceMessageID)
		result.Result, err = s.mutate(tx, c, "practice.publish", req.Key, target, req, func() (string, int64, error) {
			if strings.TrimSpace(req.Content) == "" {
				return "", 0, fmt.Errorf("practice content is required")
			}
			message, input, err := currentHumanInstruction(tx, c, req.SharingSourceMessageID)
			if err != nil {
				return "", 0, err
			}
			content := work.NewContent(stableID("body", target), scope.ID, "text/markdown; charset=utf-8", []byte(req.Content), s.now())
			if err := tx.PutContent(content); err != nil {
				return "", 0, err
			}
			grantID := stableID("share", target, message.ID)
			grant := work.SharingGrant{ID: grantID, SourceScopeID: scope.ID, ContentRef: content.ID, ContentSHA256: content.SHA256, Target: work.SharingTarget{Kind: work.SharingPractice, ObjectID: req.ID, ObjectVersion: req.Version, KnowledgeOwner: &owner}, AuthorizedBy: message.Source, AuthorizingRunRef: c.Attempt.RunRef, SourceInboxID: input.ID, SourceMessageID: message.ID, State: work.SharingActive, Revision: 1, CreatedAt: s.now()}
			if err := tx.PutSharingGrant(grant, 0); err != nil {
				return "", 0, err
			}
			source := work.SourceRef{Kind: "message", ID: message.ID, Version: strconv.FormatInt(message.Revision, 10), EvidenceRef: message.BodyRef}
			definition := work.PracticeDefinition{StorageScopeID: scope.ID, Owner: owner, Revision: 1, PracticeRef: work.PracticeRef{ID: req.ID, Version: req.Version}, Purpose: req.Purpose, Applicability: req.Applicability, Exclusions: req.Exclusions, Inputs: req.Inputs, Steps: req.Steps, Outputs: req.Outputs, Escalation: req.Escalation, RequiredCapabilities: req.RequiredCapabilities, ContentRef: content.ID, Source: source, SharingGrantID: grantID}
			if err := tx.PutPractice(definition, 0); err != nil {
				return "", 0, err
			}
			result.SharingGrantID = grantID
			return req.ID, 1, nil
		})
		return err
	})
	return result, err
}
