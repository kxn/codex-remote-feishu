// Package worksettings shares Bot and context preference use cases between
// authenticated private commands, UI selections and admitted human instructions.
package worksettings

import (
	"context"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

var ErrConflict = errors.New("settings fingerprint changed")
var ErrUnavailable = errors.New("settings owner is unavailable")

type Profile struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Backend        agentproto.Backend `json:"backend"`
	Available      bool               `json:"available"`
	FixedModel     string             `json:"fixed_model,omitempty"`
	FixedReasoning string             `json:"fixed_reasoning,omitempty"`
}
type Snapshot struct {
	Settings           state.BotCapabilitySettingsRecord `json:"settings"`
	Fingerprint        string                            `json:"fingerprint"`
	Profiles           []Profile                         `json:"profiles"`
	Catalog            *agentproto.ModelCatalogSnapshot  `json:"catalog,omitempty"`
	CatalogBackend     agentproto.Backend                `json:"catalog_backend,omitempty"`
	CatalogProfileID   string                            `json:"catalog_profile_id,omitempty"`
	CatalogEvidence    string                            `json:"catalog_evidence,omitempty"`
	CatalogUnavailable string                            `json:"catalog_unavailable,omitempty"`
}
type Evidence struct {
	PrincipalID string         `json:"principal_id"`
	Source      work.SourceRef `json:"source"`
	OperationID string         `json:"operation_id"`
	AcceptedAt  time.Time      `json:"accepted_at"`
}
type Applied struct {
	Fingerprint     string `json:"fingerprint"`
	DesiredRevision int64  `json:"desired_revision"`
	Evidence        string `json:"evidence"`
}

// Port owns the existing BotCapabilitySettings source. Apply must compare the
// fingerprint under that owner's lock, persist first, and project afterward.
type Port interface {
	Snapshot(context.Context, work.RoleBotBinding) (Snapshot, error)
	ApplyBot(context.Context, work.RoleBotBinding, string, state.BotCapabilitySettingsRecord, Evidence) (Applied, error)
}
type Authorizer func(context.Context, string, work.RoleBotBinding, work.SourceRef) error
type Service struct {
	store     *workstore.Store
	port      Port
	authorize Authorizer
	now       func() time.Time
}

func New(store *workstore.Store, port Port, authorize Authorizer, now func() time.Time) (*Service, error) {
	if store == nil || port == nil || authorize == nil {
		return nil, ErrUnavailable
	}
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, port: port, authorize: authorize, now: now}, nil
}

type UpdateRequest struct {
	ManualSelection     bool                `json:"manual_selection,omitempty"`
	CatalogCursor       string              `json:"catalog_cursor,omitempty"`
	Key                 string              `json:"key"`
	RoleID              string              `json:"role_id"`
	ExpectedFingerprint string              `json:"expected_fingerprint"`
	Backend             *agentproto.Backend `json:"backend,omitempty"`
	ProfileID           *string             `json:"profile_id,omitempty"`
	Model               *string             `json:"model,omitempty"`
	Reasoning           *string             `json:"reasoning,omitempty"`
}
type Result struct {
	Status          string `json:"status"`
	OperationID     string `json:"operation_id"`
	DesiredRevision int64  `json:"desired_revision,omitempty"`
	Fingerprint     string `json:"fingerprint,omitempty"`
	Replay          bool   `json:"replay"`
	Evidence        string `json:"evidence,omitempty"`
}
type ContextUpdateRequest struct {
	Key              string   `json:"key"`
	ContextID        string   `json:"context_id"`
	ExpectedRevision int64    `json:"expected_revision"`
	AccessMode       *string  `json:"access_mode,omitempty"`
	PlanMode         *string  `json:"plan_mode,omitempty"`
	Verbose          *string  `json:"verbose,omitempty"`
	Reset            []string `json:"reset,omitempty"`
}

type CatalogOwner struct {
	Cursor            string             `json:"cursor,omitempty"`
	ContextID         string             `json:"context_id"`
	WorkerID          string             `json:"worker_id"`
	SessionGeneration int64              `json:"session_generation"`
	SettingsRevision  int64              `json:"settings_revision"`
	Backend           agentproto.Backend `json:"backend"`
	ProfileID         string             `json:"profile_id"`
}
type ObservedCatalog struct {
	Snapshot              *agentproto.ModelCatalogSnapshot
	Evidence, Unavailable string
}
type CatalogReader interface {
	ReadCatalog(context.Context, work.RoleBotBinding, CatalogOwner) (ObservedCatalog, error)
}
