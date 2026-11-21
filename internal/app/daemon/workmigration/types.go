// Package workmigration makes the legacy-to-work authority transition durable.
// Missing identities and native state remain auditable, isolated evidence.
package workmigration

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SnapshotVersion = 1

type Backup struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type Snapshot struct {
	ID                string         `json:"id"`
	Version           int            `json:"version"`
	SourceFingerprint string         `json:"source_fingerprint"`
	Backups           []Backup       `json:"backups"`
	Records           []LegacyRecord `json:"records"`
}
type VerifiedOwner struct {
	Identity    work.IdentityScope `json:"identity"`
	PrincipalID string             `json:"principal_id"`
	RoleID      string             `json:"role_id"`
	EvidenceRef string             `json:"evidence_ref"`
}
type LegacyQueue struct {
	OriginalID string    `json:"original_id"`
	Text       string    `json:"text"`
	SourceRef  string    `json:"source_ref"`
	ReceivedAt time.Time `json:"received_at"`
}
type LegacyNative struct {
	Backend    work.Backend `json:"backend"`
	Namespace  string       `json:"namespace"`
	SessionID  string       `json:"session_id"`
	InstanceID string       `json:"instance_id,omitempty"`
	HostID     string       `json:"host_id,omitempty"`
	CWD        string       `json:"cwd,omitempty"`
	// Read-only facts, never a declaration that the native process is idle.
	Facts json.RawMessage `json:"facts,omitempty"`
}
type LegacyRecord struct {
	ChatType string          `json:"chat_type,omitempty"`
	Key      string          `json:"key"`
	UnitKey  string          `json:"unit_key"` // one old private context or group binding
	Kind     string          `json:"kind"`     // surface, queue, native, cron, preview, config
	Raw      json.RawMessage `json:"raw"`
	Owner    *VerifiedOwner  `json:"owner,omitempty"`
	Queue    *LegacyQueue    `json:"queue,omitempty"`
	Native   *LegacyNative   `json:"native,omitempty"`
}
type NativeFence struct {
	RecordKey      string          `json:"record_key"`
	Classification string          `json:"classification"` // stopped, transferred, unknown
	Evidence       json.RawMessage `json:"evidence"`
}
type FenceReceipt struct {
	MigrationID         string `json:"migration_id"`
	SourceFingerprint   string `json:"source_fingerprint"`
	LegacyWritersClosed bool   `json:"legacy_writers_closed"`
	QueuesCaptured      bool   `json:"queues_captured"`
	IngressClosed       bool   `json:"ingress_closed"`
	CronDisabled        bool   `json:"cron_disabled"`
	// Reconciled means each in-flight item has a recorded disposition and its
	// uncertain native namespace/location is fenced; it never means all stopped.
	InflightReconciled bool            `json:"inflight_reconciled"`
	Native             []NativeFence   `json:"native"`
	Evidence           json.RawMessage `json:"evidence"`
}
type Host interface {
	Fence(context.Context, Snapshot) (FenceReceipt, error)
}

// HostAttribution comes from a trusted host channel or local launch ledger,
// never from a self-reported legacy Hello or an Agent assertion.
type HostAttribution struct {
	HostID   string          `json:"host_id"`
	Evidence json.RawMessage `json:"evidence"`
}
type Options struct {
	StateDir string
	Clock    func() time.Time
}
type Status struct {
	ID                  string `json:"id"`
	SourceFingerprint   string `json:"source_fingerprint"`
	Phase               string `json:"phase"`
	Revision            int64  `json:"revision"`
	AuthorityGeneration int64  `json:"authority_generation"`
	Imported            int    `json:"imported"`
	Quarantined         int    `json:"quarantined"`
	Pending             int    `json:"pending"`
}
