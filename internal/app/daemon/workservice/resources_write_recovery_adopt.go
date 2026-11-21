package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strconv"
)

// AdoptResourceWriteRecovery is a separate actual human decision. It confirms
// adopting the displayed observation, never the old uncertain platform call.
func (s *Service) AdoptResourceWriteRecovery(ctx context.Context, interactionID, viewID string) (ResourceWriteResult, error) {
	var result ResourceWriteResult
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		id := stableID("resource-recovery-adopt", interactionID)
		if old, e := tx.GetOperation(id); e == nil {
			interaction, e := tx.GetHumanInteraction(interactionID)
			if e != nil {
				return e
			}
			if old.Actor != (work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}) || interaction.Envelope.Values["team_view"] != viewID {
				return workstore.ErrScopeDenied
			}
			return json.Unmarshal([]byte(old.ExternalReceipt), &result)
		} else if e != workstore.ErrNotFound {
			return e
		}
		interaction, view, original, intent, err := s.resourceRecoveryAuthority(tx, interactionID, viewID, "resource.write.recovery.confirm", "resource.write.adopt")
		if err != nil {
			return err
		}
		targetBody, err := tx.GetContent(view.ScopeID, view.ID+":resource_recovery")
		if err != nil {
			return err
		}
		var target ResourceRecoveryViewTarget
		if err = json.Unmarshal(targetBody.Data, &target); err != nil {
			return err
		}
		readOp, err := tx.GetOperation(target.ObservationOperationID)
		if err != nil {
			return err
		}
		if readOp.Kind != "resource.write.inspect" || readOp.ScopeID != original.ScopeID || readOp.TargetID != original.ID || readOp.State != work.OperationConfirmed {
			return workstore.ErrScopeDenied
		}
		observed, err := tx.GetContent(original.ScopeID, readOp.ExternalReceipt)
		if err != nil {
			return err
		}
		var observation work.ResourceWriteRecoveryObservation
		if err = json.Unmarshal(observed.Data, &observation); err != nil {
			return err
		}
		if observation.OriginalOperationID != original.ID || observation.OriginalRevision != original.Revision || observation.ReadOperationID != readOp.ID || observation.Binding != intent.Binding || observation.Resource.ConnectorIdentity != intent.Resource.ConnectorIdentity {
			return workstore.ErrScopeDenied
		}
		inspect, err := tx.GetHumanInteraction(observation.InspectInteractionID)
		if err != nil {
			return err
		}
		if inspect.PrincipalID != interaction.PrincipalID {
			return workstore.ErrScopeDenied
		}
		if intent.Create != nil {
			if observation.Resource.SourceIdentity == "" || observation.Resource.ID == "" {
				return fmt.Errorf("existing document identity was not actually read")
			}
			resources, err := tx.ListResources(original.ScopeID)
			if err != nil {
				return err
			}
			exists := false
			for _, resource := range resources {
				if resource.ConnectorIdentity == observation.Resource.ConnectorIdentity && resource.SourceIdentity == observation.Resource.SourceIdentity {
					if resource.ID != observation.Resource.ID {
						return workstore.ErrConflict
					}
					observation.Resource = resource
					exists = true
					break
				}
			}
			if !exists {
				if err = tx.PutResource(observation.Resource, 0); err != nil {
					return err
				}
			}
			if err = tx.AddResourceCollection(original.ScopeID, observation.Resource.ID, intent.Create.CollectionID); err != nil {
				return err
			}
		} else {
			resource, err := tx.GetResource(original.ScopeID, observation.Resource.ID)
			if err != nil {
				return err
			}
			if resource.SourceIdentity != intent.Resource.SourceIdentity || resource.ConnectorIdentity != intent.Resource.ConnectorIdentity {
				return workstore.ErrScopeDenied
			}
		}
		evidenceData, err := json.Marshal(interaction.Envelope)
		if err != nil {
			return err
		}
		evidence := work.NewContent(id+":human", original.ScopeID, "application/json", evidenceData, s.now())
		if err = tx.PutContent(evidence); err != nil {
			return err
		}
		source := work.SourceRef{Kind: "human_interaction", ID: interaction.ID, Version: strconv.FormatInt(interaction.Revision, 10), EvidenceRef: evidence.ID}
		record := work.ResourceWriteRecoveryAdoption{OriginalOperationID: original.ID, OriginalRevision: original.Revision, ObservationOperationID: readOp.ID, ObservationRef: observed.ID, InteractionID: interaction.ID, ViewID: view.ID, ResourceID: observation.Resource.ID, Source: source}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		request := work.NewContent(id+":request", original.ScopeID, "application/json", raw, s.now())
		if err = tx.PutContent(request); err != nil {
			return err
		}
		audit := work.Operation{ID: id, ScopeID: original.ScopeID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}, Kind: "resource.write.adopt", TargetID: original.ID, TargetGeneration: original.TargetGeneration, IdempotencyKey: interaction.ID, RequestRef: request.ID, RequestHash: request.SHA256, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if _, _, err = tx.PutOperation(audit, 0); err != nil {
			return err
		}
		audit.State = work.OperationSending
		audit.Revision++
		if _, _, err = tx.PutOperation(audit, 1); err != nil {
			return err
		}
		result = ResourceWriteResult{Result: Result{Status: "adopted_current_state", OperationID: audit.ID, ObjectID: observation.Resource.ID, Revision: observation.Resource.Revision}, RecoveryOperationID: audit.ID}
		receipt, err := json.Marshal(result)
		if err != nil {
			return err
		}
		audit.State = work.OperationConfirmed
		audit.Revision++
		audit.ExternalReceipt = string(receipt)
		if _, _, err = tx.PutOperation(audit, 2); err != nil {
			return err
		}
		expected := original.Revision
		original.Revision++
		original.UpdatedAt = s.now()
		original.State = work.OperationUnknown
		original.Steps = append(original.Steps, work.OperationStep{ID: "human_resource_recovery", State: work.OperationConfirmed, Receipt: audit.ID})
		if _, _, err = tx.PutOperation(original, expected); err != nil {
			return err
		}
		valid, err := tx.ResourceWriteWasRecovered(original)
		if err != nil {
			return err
		}
		if !valid {
			return workstore.ErrScopeDenied
		}
		return nil
	})
	return result, err
}
