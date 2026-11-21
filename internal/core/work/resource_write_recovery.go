package work

import "time"

const (
	ResourceDocumentUpdateOperation       = "resource.document.update-text"
	ResourceDocumentUpdateLegacyOperation = "resource.document.update"
)

func ResourceWriteCanBeInspected(kind string) bool {
	switch kind {
	case "resource.document.create", "resource.document.append", ResourceDocumentUpdateOperation, ResourceDocumentUpdateLegacyOperation, "resource.sheet.write":
		return true
	}
	return false
}

// ResourceWriteRecoveryObservation freezes the actual read selected by a real
// human. DataRef holds bounded platform evidence, not an original write receipt.
type ResourceWriteRecoveryObservation struct {
	OriginalOperationID  string         `json:"original_operation_id"`
	OriginalRevision     int64          `json:"original_revision"`
	Kind                 string         `json:"kind"`
	Binding              RoleBotBinding `json:"binding"`
	ConnectorIdentity    string         `json:"connector_identity,omitempty"`
	Resource             Resource       `json:"resource"`
	CandidateURL         string         `json:"candidate_url,omitempty"`
	InspectInteractionID string         `json:"inspect_interaction_id"`
	InspectViewID        string         `json:"inspect_view_id"`
	ReadOperationID      string         `json:"read_operation_id"`
	DataRef              string         `json:"data_ref"`
	ObservationRange     string         `json:"observation_range"`
	ObservedAt           time.Time      `json:"observed_at"`
}
type ResourceWriteRecoveryAdoption struct {
	OriginalOperationID    string    `json:"original_operation_id"`
	OriginalRevision       int64     `json:"original_revision"`
	ObservationOperationID string    `json:"observation_operation_id"`
	ObservationRef         string    `json:"observation_ref"`
	InteractionID          string    `json:"interaction_id"`
	ViewID                 string    `json:"view_id"`
	ResourceID             string    `json:"resource_id"`
	Source                 SourceRef `json:"source"`
}
