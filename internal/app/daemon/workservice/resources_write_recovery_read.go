package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
	"strings"
	"time"
)

type ResourceRecoveryViewTarget struct {
	ObservationOperationID string `json:"observation_operation_id"`
}
type ResourceRecoveryData struct {
	Resolved  *ResolvedResource            `json:"resolved,omitempty"`
	Documents []ResourceDocumentInspection `json:"documents,omitempty"`
	Sheet     *ResourceSheetInspection     `json:"sheet,omitempty"`
}
type ResourceRecoveryInspection struct {
	Observation work.ResourceWriteRecoveryObservation `json:"observation"`
	Data        ResourceRecoveryData                  `json:"data"`
}

func resourceRecoverySealed(op work.Operation) bool {
	for _, step := range op.Steps {
		if step.ID == "human_resource_recovery" {
			return true
		}
	}
	return false
}
func (s *Service) resourceRecoveryAuthority(tx *workstore.Tx, interactionID, viewID, kind, action string) (workstore.HumanInteraction, workstore.UIView, work.Operation, resourceWriteIntent, error) {
	var interaction workstore.HumanInteraction
	var view workstore.UIView
	var op work.Operation
	var intent resourceWriteIntent
	interaction, err := tx.GetHumanInteraction(interactionID)
	if err != nil {
		return interaction, view, op, intent, err
	}
	view, err = tx.GetUIView(viewID)
	if err != nil {
		return interaction, view, op, intent, err
	}
	if err = tx.ValidateViewInteraction(view, interaction); err != nil {
		return interaction, view, op, intent, err
	}
	if view.Kind != kind || interaction.Envelope.Values["team_view"] != view.ID || interaction.Envelope.Values["action"] != action {
		return interaction, view, op, intent, workstore.ErrScopeDenied
	}
	op, err = tx.GetOperation(view.TargetID)
	if err != nil {
		return interaction, view, op, intent, err
	}
	if op.ScopeID != view.ScopeID || !resourceWriteKind(op.Kind) || (op.State != work.OperationUnknown && op.State != work.OperationSending) || op.Revision != view.TargetRevision || resourceRecoverySealed(op) {
		return interaction, view, op, intent, workstore.ErrConflict
	}
	owner, err := tx.ResourceRecoveryOwner(op.ScopeID)
	if err != nil {
		return interaction, view, op, intent, err
	}
	if owner != interaction.PrincipalID || view.PrincipalID != owner {
		return interaction, view, op, intent, workstore.ErrScopeDenied
	}
	scope, err := tx.GetScope(op.ScopeID)
	if err != nil {
		return interaction, view, op, intent, err
	}
	if scope.Kind == work.ScopeProject {
		if err = tx.RequireProjectPrincipalAccess(scope.ID, owner, s.now()); err != nil {
			return interaction, view, op, intent, err
		}
	}
	body, err := tx.GetContent(op.ScopeID, op.RequestRef)
	if err != nil {
		return interaction, view, op, intent, err
	}
	err = json.Unmarshal(body.Data, &intent)
	if err != nil {
		return interaction, view, op, intent, err
	}
	if intent.Kind != op.Kind || intent.ScopeID != op.ScopeID || intent.Binding.RoleID != op.Actor.ID {
		return interaction, view, op, intent, workstore.ErrScopeDenied
	}
	binding, err := tx.GetRoleBotBinding(intent.Binding.RoleID)
	if err != nil {
		return interaction, view, op, intent, err
	}
	if binding != intent.Binding {
		return interaction, view, op, intent, workstore.ErrScopeDenied
	}
	return interaction, view, op, intent, nil
}

// InspectResourceWriteRecovery performs only platform reads after the real
// human selects the object. A separate confirmation, after seeing this saved
// observation, is required before sealing the original unknown operation.
func (s *Service) InspectResourceWriteRecovery(ctx context.Context, interactionID, viewID string) (ResourceRecoveryInspection, error) {
	var result ResourceRecoveryInspection
	var original, readOp work.Operation
	var intent resourceWriteIntent
	var connector ResourceConnector
	perform := false
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		interaction, view, op, frozen, err := s.resourceRecoveryAuthority(tx, interactionID, viewID, "resource.write.recovery", "resource.write.inspect")
		if err != nil {
			return err
		}
		original = op
		intent = frozen
		id := stableID("resource-recovery-read", interaction.ID)
		if existing, e := tx.GetOperation(id); e == nil {
			if existing.State != work.OperationConfirmed {
				return fmt.Errorf("previous inspection is unfinished; open a fresh recovery form for another read")
			}
			content, e := tx.GetContent(op.ScopeID, existing.ExternalReceipt)
			if e != nil {
				return e
			}
			if e = json.Unmarshal(content.Data, &result.Observation); e != nil {
				return e
			}
			data, e := tx.GetContent(op.ScopeID, result.Observation.DataRef)
			if e != nil {
				return e
			}
			return workcmd.DecodePayload(data.Data, &result.Data)
		} else if e != workstore.ErrNotFound {
			return e
		}
		connector, err = s.resolveResourceConnector(ctx, intent.Binding)
		if err != nil {
			return err
		}
		if connector.Identity() != intent.Resource.ConnectorIdentity {
			return workstore.ErrScopeDenied
		}
		selected := ""
		resource := intent.Resource
		if intent.Create != nil {
			selected, _ = interaction.Envelope.Form["source_url"].(string)
			if selected == "" {
				return fmt.Errorf("请明确粘贴您选择改用的已有文档链接")
			}
			resource.ID = ""
		}
		result.Observation = work.ResourceWriteRecoveryObservation{OriginalOperationID: op.ID, OriginalRevision: op.Revision, Kind: op.Kind, Binding: intent.Binding, Resource: resource, CandidateURL: selected, InspectInteractionID: interaction.ID, InspectViewID: view.ID, ReadOperationID: id, DataRef: id + ":data", ObservedAt: s.now()}
		raw, _ := json.Marshal(result.Observation)
		request := work.NewContent(id+":request", op.ScopeID, "application/json", raw, s.now())
		if err = tx.PutContent(request); err != nil {
			return err
		}
		readOp = work.Operation{ID: id, ScopeID: op.ScopeID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}, Kind: "resource.write.inspect", TargetID: op.ID, TargetGeneration: op.TargetGeneration, IdempotencyKey: interaction.ID, RequestRef: request.ID, RequestHash: request.SHA256, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if _, _, err = tx.PutOperation(readOp, 0); err != nil {
			return err
		}
		readOp.State = work.OperationSending
		readOp.Revision++
		if _, _, err = tx.PutOperation(readOp, 1); err != nil {
			return err
		}
		perform = true
		return nil
	})
	if err != nil || !perform {
		return result, err
	}
	if intent.Create != nil {
		resolved, e := connector.Resolve(ctx, result.Observation.CandidateURL)
		err = e
		result.Data.Resolved = &resolved
		if err == nil && !strings.HasPrefix(resolved.SourceIdentity, "docx:") {
			err = fmt.Errorf("请改用实际 Docx 文档，不能以目录或其他类型代替")
		}
		if err == nil {
			result.Observation.Resource = work.Resource{ID: stableID("resource", original.ScopeID, connector.Identity(), resolved.SourceIdentity), ScopeID: original.ScopeID, ConnectorIdentity: connector.Identity(), SourceIdentity: resolved.SourceIdentity, CanonicalURI: resolved.CanonicalURI, CollectionID: intent.Create.CollectionID, Purpose: intent.Create.Purpose, Revision: 1}
			result.Observation.ObservationRange = "human-selected existing document metadata; not a receipt for the old creation"
		}
	}
	if err == nil && intent.Create == nil {
		writer, ok := connector.(ResourceWriteConnector)
		if !ok {
			err = fmt.Errorf("connector cannot inspect the original exact write target")
		} else if intent.Sheet != nil {
			value, e := writer.ReadResourceSheet(ctx, intent.Resource, ReadResourceSheetRequest{ResourceID: intent.Resource.ID, SheetID: intent.Sheet.SheetID, Range: intent.Sheet.Range})
			result.Data.Sheet = &value
			result.Observation.ObservationRange = intent.Sheet.SheetID + "!" + intent.Sheet.Range
			err = e
		} else {
			blocks := []string{}
			if intent.Append != nil {
				blocks = append(blocks, intent.Append.ParentBlockID)
			}
			if intent.Update != nil {
				seen := map[string]bool{}
				for _, edit := range intent.Update.Edits {
					if !seen[edit.BlockID] {
						blocks = append(blocks, edit.BlockID)
						seen[edit.BlockID] = true
					}
				}
			}
			revision := 0
			for _, block := range blocks {
				value, e := writer.InspectResourceDocument(ctx, intent.Resource, InspectResourceDocumentRequest{ResourceID: intent.Resource.ID, BlockID: block, DocumentRevision: revision})
				if e != nil {
					err = e
					break
				}
				if revision == 0 {
					revision = value.RequestedRevision
				}
				result.Data.Documents = append(result.Data.Documents, value)
			}
			result.Observation.ObservationRange = "exact affected Docx blocks at observed revision " + fmt.Sprint(revision) + "; previews are partial, not a whole-document snapshot"
		}
	}
	callErr := err
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = s.store.Within(receiptCtx, func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(readOp.ID)
		if e != nil {
			return e
		}
		if op.Revision != readOp.Revision {
			return workstore.ErrConflict
		}
		if callErr == nil && intent.Create != nil {
			resources, e := tx.ListResources(original.ScopeID)
			if e != nil {
				return e
			}
			for _, r := range resources {
				if r.ConnectorIdentity == result.Observation.Resource.ConnectorIdentity && r.SourceIdentity == result.Observation.Resource.SourceIdentity {
					result.Observation.Resource = r
					break
				}
			}
		}
		data, e := json.Marshal(result.Data)
		if e != nil {
			return e
		}
		if e = tx.PutContent(work.NewContent(result.Observation.DataRef, original.ScopeID, "application/json", data, s.now())); e != nil {
			return e
		}
		raw, e := json.Marshal(result.Observation)
		if e != nil {
			return e
		}
		evidence := work.NewContent(op.ID+":observation", original.ScopeID, "application/json", raw, s.now())
		if e = tx.PutContent(evidence); e != nil {
			return e
		}
		op.ExternalReceipt = evidence.ID
		expected := op.Revision
		op.Revision++
		op.UpdatedAt = s.now()
		op.State = work.OperationConfirmed
		if callErr != nil {
			op.State = work.OperationTerminal
			op.LastError = callErr.Error()
		}
		_, _, e = tx.PutOperation(op, expected)
		return e
	})
	if err != nil {
		return result, err
	}
	return result, callErr
}
