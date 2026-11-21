package work

import (
	"fmt"
	"time"
)

// SourceRef identifies the authority and its exact adopted version. A URL by
// itself is not a version. EvidenceRef points at the read/receipt that observed it.
type SourceRef struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Version     string `json:"version"`
	EvidenceRef string `json:"evidence_ref"`
}

func (v SourceRef) Validate() error { return required(v.Kind, v.ID, v.Version, v.EvidenceRef) }

type Resource struct {
	ID                string `json:"id"`
	ScopeID           string `json:"scope_id"`
	CollectionID      string `json:"collection_id"`
	ConnectorIdentity string `json:"connector_identity"`
	SourceIdentity    string `json:"source_identity"`
	CanonicalURI      string `json:"canonical_uri"`
	Purpose           string `json:"purpose"`
	Revision          int64  `json:"revision"`
}

func (v Resource) Validate() error {
	if err := required(v.ID, v.ScopeID, v.CollectionID, v.ConnectorIdentity, v.SourceIdentity, v.CanonicalURI, v.Purpose); err != nil {
		return err
	}
	return positive("resource revision", v.Revision)
}

type VersionKind string

const (
	VersionNativeHistorical  VersionKind = "native_historical"
	VersionImmutableSnapshot VersionKind = "immutable_snapshot"
	VersionObservedDataset   VersionKind = "observed_dataset"
	VersionCurrentReference  VersionKind = "current_reference"
)

type Consistency string

const (
	ConsistencyAtomic   Consistency = "atomic"
	ConsistencyObserved Consistency = "observed"
	ConsistencyCurrent  Consistency = "current"
)

type ResourceVersion struct {
	ID               string      `json:"id"`
	ResourceID       string      `json:"resource_id"`
	ScopeID          string      `json:"scope_id"`
	Kind             VersionKind `json:"kind"`
	Source           SourceRef   `json:"source"`
	ContentRef       string      `json:"content_ref,omitempty"`
	ContentHash      string      `json:"content_hash,omitempty"`
	ObservationRange string      `json:"observation_range,omitempty"`
	Consistency      Consistency `json:"consistency"`
	Complete         bool        `json:"complete"`
	Saved            bool        `json:"saved"`
	StartedAt        time.Time   `json:"started_at"`
	CapturedAt       time.Time   `json:"captured_at"`
}

func (v ResourceVersion) Validate() error {
	if err := required(v.ID, v.ResourceID, v.ScopeID); err != nil {
		return err
	}
	if err := v.Source.Validate(); err != nil {
		return err
	}
	if !oneOf(v.Consistency, ConsistencyAtomic, ConsistencyObserved, ConsistencyCurrent) {
		return fmt.Errorf("invalid consistency")
	}
	if !v.StartedAt.IsZero() && v.CapturedAt.Before(v.StartedAt) {
		return fmt.Errorf("capture precedes start")
	}
	switch v.Kind {
	case VersionImmutableSnapshot:
		if err := required(v.ContentRef, v.ContentHash); err != nil {
			return err
		}
		if !v.Saved || !v.Complete || v.Consistency != ConsistencyAtomic {
			return fmt.Errorf("immutable snapshot requires saved complete atomic content")
		}
	case VersionObservedDataset:
		if err := required(v.ObservationRange); err != nil {
			return err
		}
		if v.Consistency == ConsistencyCurrent {
			return fmt.Errorf("dataset requires observation consistency")
		}
	case VersionNativeHistorical:
		if !v.Saved || v.Consistency == ConsistencyCurrent {
			return fmt.Errorf("historical version requires saved evidence")
		}
	case VersionCurrentReference:
		if v.ContentHash != "" || v.Consistency != ConsistencyCurrent {
			return fmt.Errorf("current reference cannot claim immutable content")
		}
	default:
		return fmt.Errorf("invalid version kind")
	}
	return nil
}

type RetentionClass string

const (
	RetentionTemporary RetentionClass = "temporary"
	RetentionRetained  RetentionClass = "retained"
	RetentionPermanent RetentionClass = "permanent"
)

type DeliveryRef struct {
	ResourceID string `json:"resource_id"`
	VersionID  string `json:"version_id,omitempty"`
	Current    bool   `json:"current"`
}

func (v DeliveryRef) Validate() error {
	if err := required(v.ResourceID); err != nil {
		return err
	}
	if v.Current == (v.VersionID != "") {
		return fmt.Errorf("delivery needs exactly one of current or fixed version")
	}
	return nil
}

type Delivery struct {
	ID                   string         `json:"id"`
	ScopeID              string         `json:"scope_id"`
	Audience             []Actor        `json:"audience"`
	Purpose              string         `json:"purpose"`
	Summary              string         `json:"summary"`
	NeededAction         string         `json:"needed_action,omitempty"`
	References           []DeliveryRef  `json:"references"`
	Retention            RetentionClass `json:"retention"`
	ConversationTarget   string         `json:"conversation_target"`
	SourceAttemptID      string         `json:"source_attempt_id"`
	NotificationOutboxID string         `json:"notification_outbox_id"`
	Revision             int64          `json:"revision"`
	CreatedAt            time.Time      `json:"created_at"`
}

func (v Delivery) Validate() error {
	if err := required(v.ID, v.ScopeID, v.Purpose, v.Summary, v.ConversationTarget, v.SourceAttemptID, v.NotificationOutboxID); err != nil {
		return err
	}
	if len(v.Audience) == 0 {
		return fmt.Errorf("delivery needs audience")
	}
	for _, a := range v.Audience {
		if err := a.Validate(); err != nil {
			return err
		}
	}
	for _, ref := range v.References {
		if err := ref.Validate(); err != nil {
			return err
		}
	}
	if !oneOf(v.Retention, RetentionTemporary, RetentionRetained, RetentionPermanent) {
		return fmt.Errorf("invalid retention class")
	}
	return positive("delivery revision", v.Revision)
}
func ValidateDeliveryVersions(delivery Delivery, versions []ResourceVersion) error {
	if err := delivery.Validate(); err != nil {
		return err
	}
	byID := map[string]ResourceVersion{}
	for _, v := range versions {
		if _, ok := byID[v.ID]; ok {
			return fmt.Errorf("duplicate version")
		}
		byID[v.ID] = v
	}
	for _, ref := range delivery.References {
		if ref.Current {
			continue
		}
		version, ok := byID[ref.VersionID]
		if !ok {
			return fmt.Errorf("missing delivery version")
		}
		if err := version.Validate(); err != nil {
			return err
		}
		if version.ResourceID != ref.ResourceID || version.ScopeID != delivery.ScopeID || !version.Saved || version.Kind == VersionCurrentReference {
			return fmt.Errorf("delivery version is unsaved, dynamic, or outside scope")
		}
	}
	return nil
}
