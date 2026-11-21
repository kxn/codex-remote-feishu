package workprovision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// RecoveryInteractionTarget is persisted with the recovery card. Callback
// fields cannot choose another operation, step or generation.
type RecoveryInteractionTarget struct {
	OperationID      string `json:"operation_id"`
	StepID           string `json:"step_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	HomeDocumentID   string `json:"home_document_id,omitempty"`
}

func (s *Service) validateRecoveryInteraction(tx *workstore.Tx, state snapshot, recovery Recovery, after time.Time) error {
	interaction, err := tx.GetHumanInteraction(recovery.InteractionID)
	if err != nil {
		return err
	}
	view, err := tx.GetUIView(recovery.ViewID)
	if err != nil {
		return err
	}
	if view.Kind != "project.recovery" || view.TargetID != state.operation.ID || view.TargetRevision != recovery.ExpectedRevision || interaction.PrincipalID != state.operation.Actor.ID || state.operation.Actor.Kind != work.ActorPrincipal {
		return workstore.ErrScopeDenied
	}
	if err = tx.ValidateViewInteraction(view, interaction); err != nil {
		return err
	}
	if err = tx.RequireProjectPrincipalAccess(state.project.ScopeID, interaction.PrincipalID, s.now()); err != nil {
		return err
	}
	viewID, _ := interaction.Envelope.Values["team_view"].(string)
	action, _ := interaction.Envelope.Values["action"].(string)
	if viewID != view.ID || action != "recovery.resolve" || interaction.Envelope.ReceivedAt.Before(after) {
		return workstore.ErrScopeDenied
	}
	body, err := tx.GetContent(view.ScopeID, view.ID+":recovery-target")
	if err != nil {
		return err
	}
	var frozen RecoveryInteractionTarget
	if err = json.Unmarshal(body.Data, &frozen); err != nil {
		return err
	}
	if frozen.OperationID != recovery.OperationID || frozen.StepID != recovery.StepID || frozen.ExpectedRevision != recovery.ExpectedRevision {
		return workstore.ErrScopeDenied
	}
	if recovery.StepID == "home.index" {
		if recovery.ExternalID != frozen.HomeDocumentID {
			return workstore.ErrScopeDenied
		}
		return nil
	}
	input, _ := interaction.Envelope.Form["external_id"].(string)
	secondary, _ := interaction.Envelope.Form["secondary_id"].(string)
	if recovery.CandidateURL != "" {
		if strings.TrimSpace(input) != recovery.CandidateURL {
			return workstore.ErrScopeDenied
		}
	} else if strings.TrimSpace(input) != recovery.ExternalID {
		return workstore.ErrScopeDenied
	}
	if strings.TrimSpace(secondary) != recovery.SecondaryID {
		return workstore.ErrScopeDenied
	}
	return nil
}

type RecoveryDocumentResolver interface {
	ResolveRecoveryDocument(context.Context, string) (HomeDocument, error)
}

func (p *FeishuPlatform) ResolveRecoveryDocument(ctx context.Context, link string) (HomeDocument, error) {
	parsed, err := url.Parse(link)
	if err != nil || parsed.Scheme != "https" {
		return HomeDocument{}, fmt.Errorf("请提供飞书文档的实际链接")
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.HasSuffix(host, ".feishu.cn") && !strings.HasSuffix(host, ".larksuite.com") {
		return HomeDocument{}, fmt.Errorf("需要实际飞书文档链接")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 {
		return HomeDocument{}, fmt.Errorf("无法识别文档链接")
	}
	token := parts[1]
	switch parts[0] {
	case "wiki":
		node, err := p.documents.ResolveWikiNode(ctx, feishu.ProjectWikiNodeToken(token))
		if err != nil {
			return HomeDocument{}, err
		}
		if node.ObjType != "docx" {
			return HomeDocument{}, fmt.Errorf("选中的Wiki页面不是Docx文档")
		}
		token = node.ObjToken
	case "docx":
	default:
		return HomeDocument{}, fmt.Errorf("需要Docx或Wiki文档链接")
	}
	return p.ReadDocument(ctx, token)
}
