package work

import (
	"fmt"
	"time"
)

type SharingKind string

const (
	SharingMemory   SharingKind = "memory"
	SharingPractice SharingKind = "practice"
)

type SharingState string

const (
	SharingActive  SharingState = "active"
	SharingRevoked SharingState = "revoked"
)

// SharingTarget freezes the actual destination, object and version. It is a
// record of an explicit instruction, never permission inferred from chat age.
type SharingTarget struct {
	Kind           SharingKind     `json:"kind"`
	ObjectID       string          `json:"object_id"`
	ObjectVersion  string          `json:"object_version"`
	MemoryScope    *MemoryScope    `json:"memory_scope,omitempty"`
	KnowledgeOwner *KnowledgeOwner `json:"knowledge_owner,omitempty"`
}

func (v SharingTarget) Validate() error {
	if err := required(v.ObjectID, v.ObjectVersion); err != nil {
		return err
	}
	switch v.Kind {
	case SharingMemory:
		if v.MemoryScope == nil || v.KnowledgeOwner != nil {
			return fmt.Errorf("memory sharing needs exact memory owner")
		}
		return v.MemoryScope.Validate()
	case SharingPractice:
		if v.KnowledgeOwner == nil || v.MemoryScope != nil {
			return fmt.Errorf("practice sharing needs exact knowledge owner")
		}
		return v.KnowledgeOwner.Validate()
	default:
		return fmt.Errorf("invalid sharing target")
	}
}

type SharingGrant struct {
	ID                string        `json:"id"`
	SourceScopeID     string        `json:"source_scope_id"`
	ContentRef        string        `json:"content_ref"`
	ContentSHA256     string        `json:"content_sha256"`
	Target            SharingTarget `json:"target"`
	AuthorizedBy      Actor         `json:"authorized_by"`
	AuthorizingRunRef RunRef        `json:"authorizing_run_ref"`
	SourceInboxID     string        `json:"source_inbox_id"`
	SourceMessageID   string        `json:"source_message_id"`
	State             SharingState  `json:"state"`
	Revision          int64         `json:"revision"`
	CreatedAt         time.Time     `json:"created_at"`
}

func (v SharingGrant) Validate() error {
	if err := required(v.ID, v.SourceScopeID, v.ContentRef, v.ContentSHA256, v.SourceInboxID, v.SourceMessageID); err != nil {
		return err
	}
	if v.AuthorizedBy.Kind != ActorPrincipal {
		return fmt.Errorf("sharing requires a human instruction")
	}
	if err := v.AuthorizedBy.Validate(); err != nil {
		return err
	}
	if err := v.AuthorizingRunRef.Validate(); err != nil {
		return err
	}
	if err := v.Target.Validate(); err != nil {
		return err
	}
	if !oneOf(v.State, SharingActive, SharingRevoked) {
		return fmt.Errorf("invalid sharing state")
	}
	return positive("sharing revision", v.Revision)
}
