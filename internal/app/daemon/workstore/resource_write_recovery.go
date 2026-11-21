package workstore

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strconv"
)

// ResourceReadHasNoExternalWrite is an explicit list of read-only platform
// contracts. Local observations remain historical; archive never rewrites their
// unknown/sending state into a fabricated completion or cancellation.
func ResourceReadHasNoExternalWrite(kind string) bool {
	switch kind {
	case "resource.register", "resource.capture", "resource.comments.read", "resource.write.observe", "resource.write.inspect":
		return true
	}
	return false
}
func resourceRecoverableWrite(kind string) bool {
	switch kind {
	case "resource.document.create", "resource.document.append", "resource.document.update", "resource.sheet.write":
		return true
	}
	return false
}

// ResourceWriteWasRecovered recognizes only a confirmed real-human adoption of
// an independently read current object/range. The original write stays unknown.
func (t *Tx) ResourceWriteWasRecovered(op work.Operation) (bool, error) {
	if !resourceRecoverableWrite(op.Kind) || op.State != work.OperationUnknown {
		return false, nil
	}
	for _, step := range op.Steps {
		if step.ID != "human_resource_recovery" || step.State != work.OperationConfirmed {
			continue
		}
		audit, err := t.GetOperation(step.Receipt)
		if err != nil {
			return false, err
		}
		if audit.Kind != "resource.write.adopt" || audit.ScopeID != op.ScopeID || audit.State != work.OperationConfirmed || audit.Actor.Kind != work.ActorPrincipal || audit.TargetID != op.ID {
			return false, ErrScopeDenied
		}
		body, err := t.GetContent(op.ScopeID, audit.RequestRef)
		if err != nil {
			return false, err
		}
		if body.SHA256 != audit.RequestHash {
			return false, ErrScopeDenied
		}
		var record work.ResourceWriteRecoveryAdoption
		if err = json.Unmarshal(body.Data, &record); err != nil {
			return false, err
		}
		if record.OriginalOperationID != op.ID || record.OriginalRevision < 1 || record.OriginalRevision >= op.Revision || audit.IdempotencyKey != record.InteractionID {
			return false, ErrScopeDenied
		}
		observed, err := t.GetOperation(record.ObservationOperationID)
		if err != nil {
			return false, err
		}
		if observed.ScopeID != op.ScopeID || observed.Kind != "resource.write.inspect" || observed.TargetID != op.ID || observed.State != work.OperationConfirmed || observed.ExternalReceipt != record.ObservationRef {
			return false, ErrScopeDenied
		}
		if observed.Actor != audit.Actor {
			return false, ErrScopeDenied
		}
		readRequest, err := t.GetContent(op.ScopeID, observed.RequestRef)
		if err != nil {
			return false, err
		}
		if readRequest.SHA256 != observed.RequestHash {
			return false, ErrScopeDenied
		}
		var frozenRead work.ResourceWriteRecoveryObservation
		if err = json.Unmarshal(readRequest.Data, &frozenRead); err != nil {
			return false, err
		}
		content, err := t.GetContent(op.ScopeID, record.ObservationRef)
		if err != nil {
			return false, err
		}
		var observation work.ResourceWriteRecoveryObservation
		if err = json.Unmarshal(content.Data, &observation); err != nil {
			return false, err
		}
		if observation.OriginalOperationID != op.ID || observation.OriginalRevision != record.OriginalRevision || observation.Kind != op.Kind || observation.ReadOperationID != observed.ID || observation.Resource.ID != record.ResourceID || observation.Resource.ScopeID != op.ScopeID {
			return false, ErrScopeDenied
		}
		if frozenRead.OriginalOperationID != op.ID || frozenRead.OriginalRevision != record.OriginalRevision || frozenRead.Kind != op.Kind || frozenRead.Binding != observation.Binding || frozenRead.CandidateURL != observation.CandidateURL || frozenRead.InspectInteractionID != observation.InspectInteractionID || frozenRead.InspectViewID != observation.InspectViewID || frozenRead.ReadOperationID != observed.ID || observation.DataRef != observed.ID+":data" {
			return false, ErrScopeDenied
		}
		if op.Kind != "resource.document.create" && (frozenRead.Resource.ID != observation.Resource.ID || frozenRead.Resource.SourceIdentity != observation.Resource.SourceIdentity) {
			return false, ErrScopeDenied
		}
		request, err := t.GetContent(op.ScopeID, op.RequestRef)
		if err != nil {
			return false, err
		}
		var intent struct {
			Binding  work.RoleBotBinding `json:"binding"`
			Resource work.Resource       `json:"resource"`
		}
		if err = json.Unmarshal(request.Data, &intent); err != nil {
			return false, err
		}
		if observation.Binding != intent.Binding || observation.Resource.ConnectorIdentity != intent.Resource.ConnectorIdentity {
			return false, ErrScopeDenied
		}
		if _, err = t.GetContent(op.ScopeID, observation.DataRef); err != nil {
			return false, err
		}
		resource, err := t.GetResource(op.ScopeID, record.ResourceID)
		if err != nil {
			return false, err
		}
		if resource.ConnectorIdentity != observation.Resource.ConnectorIdentity || resource.SourceIdentity != observation.Resource.SourceIdentity || resource.CanonicalURI != observation.Resource.CanonicalURI {
			return false, ErrScopeDenied
		}
		if err = t.validateRecoveryCardProof(op, observation.InspectInteractionID, observation.InspectViewID, "resource.write.recovery", "resource.write.inspect", record.OriginalRevision, audit.Actor.ID); err != nil {
			return false, err
		}
		if op.Kind == "resource.document.create" {
			selection, e := t.GetHumanInteraction(observation.InspectInteractionID)
			if e != nil {
				return false, e
			}
			if selection.Envelope.Form["source_url"] != observation.CandidateURL {
				return false, ErrScopeDenied
			}
		}
		if err = t.validateRecoveryCardProof(op, record.InteractionID, record.ViewID, "resource.write.recovery.confirm", "resource.write.adopt", record.OriginalRevision, audit.Actor.ID); err != nil {
			return false, err
		}
		view, err := t.GetUIView(record.ViewID)
		if err != nil {
			return false, err
		}
		target, err := t.GetContent(op.ScopeID, view.ID+":resource_recovery")
		if err != nil {
			return false, err
		}
		var selected struct {
			ObservationOperationID string `json:"observation_operation_id"`
		}
		if err = json.Unmarshal(target.Data, &selected); err != nil {
			return false, err
		}
		if selected.ObservationOperationID != observed.ID {
			return false, ErrScopeDenied
		}
		if record.Source.Kind != "human_interaction" || record.Source.ID != record.InteractionID {
			return false, ErrScopeDenied
		}
		interaction, err := t.GetHumanInteraction(record.InteractionID)
		if err != nil {
			return false, err
		}
		version, err := strconv.ParseInt(record.Source.Version, 10, 64)
		if err != nil || version < 1 || version > interaction.Revision {
			return false, ErrScopeDenied
		}
		evidence, err := t.GetContent(op.ScopeID, record.Source.EvidenceRef)
		if err != nil {
			return false, err
		}
		actual, _ := json.Marshal(interaction.Envelope)
		var saved work.HumanInteractionEnvelope
		if err = json.Unmarshal(evidence.Data, &saved); err != nil {
			return false, err
		}
		frozen, _ := json.Marshal(saved)
		if string(actual) != string(frozen) {
			return false, ErrScopeDenied
		}
		owner, err := t.ResourceRecoveryOwner(op.ScopeID)
		if err != nil {
			return false, err
		}
		if owner != audit.Actor.ID {
			return false, ErrScopeDenied
		}
		return true, nil
	}
	return false, nil
}
func (t *Tx) ResourceCreationWasReplaced(op work.Operation) (bool, error) {
	return t.ResourceWriteWasRecovered(op)
}
func (t *Tx) validateRecoveryCardProof(op work.Operation, interactionID, viewID, kind, action string, revision int64, principal string) error {
	interaction, err := t.GetHumanInteraction(interactionID)
	if err != nil {
		return err
	}
	view, err := t.GetUIView(viewID)
	if err != nil {
		return err
	}
	if interaction.PrincipalID != principal || view.PrincipalID != principal || view.ScopeID != op.ScopeID || view.Kind != kind || view.TargetID != op.ID || view.TargetRevision != revision || view.ExternalMessageID == "" || view.ExternalMessageID != interaction.Envelope.MessageID || view.ChatID != interaction.Envelope.ChatID || view.Tenant != interaction.Envelope.Tenant || view.Binding.GatewayRef != interaction.Gateway.GatewayRef || view.Binding.AppID != interaction.Gateway.AppID || view.Binding.IdentityGeneration != interaction.Gateway.BotGeneration || interaction.Envelope.Values["team_view"] != view.ID || interaction.Envelope.Values["action"] != action {
		return ErrScopeDenied
	}
	mapping, err := t.GetPlatformMessageMapping(view.Tenant, view.ChatID, view.ExternalMessageID)
	if err != nil {
		return err
	}
	if mapping.ScopeID != op.ScopeID || mapping.OperationID != view.OperationID || mapping.RoleID != view.Binding.RoleID || mapping.BotGeneration != view.Binding.IdentityGeneration {
		return fmt.Errorf("resource recovery lacks matching real publication receipt")
	}
	return nil
}

func (t *Tx) ResourceRecoveryOwner(scopeID string) (string, error) {
	scope, err := t.GetScope(scopeID)
	if err != nil {
		return "", err
	}
	if scope.Kind == work.ScopePersonal {
		return scope.PrincipalID, nil
	}
	if scope.Kind != work.ScopeProject {
		return "", ErrScopeDenied
	}
	ops, err := t.ListOperations(scope.ID)
	if err != nil {
		return "", err
	}
	owner := ""
	for _, op := range ops {
		if op.Kind == "project.provision" && op.TargetID == scope.ProjectID && op.Actor.Kind == work.ActorPrincipal {
			if owner != "" && owner != op.Actor.ID {
				return "", ErrScopeDenied
			}
			owner = op.Actor.ID
		}
	}
	if owner == "" {
		return "", ErrNotFound
	}
	return owner, nil
}
