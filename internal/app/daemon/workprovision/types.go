// Package workprovision executes the persisted project-establishment saga. SQL
// owns intent/receipts; platform calls and execution preparation stay outside SQL.
package workprovision

import (
	"context"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type HomeResource struct {
	Label   string `json:"label"`
	URL     string `json:"url"`
	Version string `json:"version,omitempty"`
}
type HomeDelivery struct {
	Summary      string         `json:"summary"`
	NeededAction string         `json:"needed_action,omitempty"`
	Resources    []HomeResource `json:"resources"`
}
type HomeIndex struct {
	Resources    []HomeResource    `json:"resources,omitempty"`
	Deliveries   []HomeDelivery    `json:"deliveries,omitempty"`
	ProjectName  string            `json:"project_name"`
	ProjectState work.ProjectState `json:"project_state"`
	ReadyMembers int               `json:"ready_members"`
	TotalMembers int               `json:"total_members"`
	ActiveTasks  int               `json:"active_tasks"`
	PendingTasks int               `json:"pending_tasks"`
	DoneTasks    int               `json:"done_tasks"`
	UpdatedAt    time.Time         `json:"updated_at"`
	SyncState    string            `json:"sync_state"`
}
type HomeDocument struct {
	ID, URL   string
	Revision  int
	NodeToken string
	RequestID string
}
type HomeBlock struct{ ID, Text, Fingerprint string }
type HomeBlocks struct {
	Index, Status HomeBlock
	Revision      int
	RequestID     string
}
type HomeBlockIDs struct {
	IndexID, StatusID string
	Revision          int
	RequestID         string
}
type Folder struct{ ID, URL, RequestID string }
type WikiParent struct {
	SpaceID, NodeToken string
	AtSpaceRoot        bool
}
type Navigation struct{ ProjectName, HomeURL, SyncState string }

// Platform is one explicitly resolved bot identity, never a current/default bot.
// Its concrete methods expose only supported project purposes, not arbitrary API.
type Platform interface {
	Binding() work.RoleBotBinding
	ConnectorIdentity() string
	IsCallerInChat(context.Context, string) (bool, error)
	InviteBots(context.Context, string, []string) (feishu.ProjectChatMembersResult, error)
	CreateFolder(context.Context, string, string) (Folder, error)
	CheckFolder(context.Context, string) error
	ResolveWikiParent(context.Context, string) (WikiParent, error)
	CreateWikiDocument(context.Context, WikiParent, string) (HomeDocument, error)
	CreateDocument(context.Context, string, string) (HomeDocument, error)
	ReadDocument(context.Context, string) (HomeDocument, error)
	ReadDocumentURL(context.Context, string) (string, error)
	AppendHomeIndex(context.Context, string, HomeIndex, string) (HomeBlockIDs, error)
	ReadHomeBlocks(context.Context, string, string, string) (HomeBlocks, error)
	UpdateHomeIndex(context.Context, string, string, string, HomeIndex, string) (HomeBlocks, error)
	EnsureGroupView(context.Context, string, string) error
	HasGroupView(context.Context, string, string) (bool, error)
	ListTabs(context.Context, string) ([]feishu.ProjectChatTab, error)
	CreateDocTab(context.Context, string, string) (feishu.ProjectChatTab, error)
	PublishNavigation(context.Context, string, Navigation, string) (string, error)
	PinNavigation(context.Context, string) (string, error)
	ReadNavigation(context.Context, string) (feishu.ProjectNavigationMessage, error)
	HasNavigationPin(context.Context, string, string) (bool, error)
}
type Resolver interface {
	Resolve(context.Context, work.RoleBotBinding) (Platform, error)
}
type ResolverFunc func(context.Context, work.RoleBotBinding) (Platform, error)

func (f ResolverFunc) Resolve(ctx context.Context, b work.RoleBotBinding) (Platform, error) {
	return f(ctx, b)
}

// ExecutionPreparer performs idempotent desired/configuration preparation only.
// It must not start a carrier/native process; the scheduler owns actual startup.
type ExecutionPreparer interface {
	PrepareProject(context.Context, work.Project, []string) error
}
type ExecutionPreparerFunc func(context.Context, work.Project, []string) error

func (f ExecutionPreparerFunc) PrepareProject(ctx context.Context, p work.Project, ids []string) error {
	return f(ctx, p, ids)
}

type Options struct {
	Resolver  Resolver
	Execution ExecutionPreparer
	Now       func() time.Time
}
type Service struct {
	store     *workstore.Store
	resolver  Resolver
	execution ExecutionPreparer
	now       func() time.Time
}
type Report struct {
	OperationID string                    `json:"operation_id"`
	ProjectID   string                    `json:"project_id"`
	State       string                    `json:"state"`
	Steps       []workstore.ProvisionStep `json:"steps"`
	Blockers    []string                  `json:"blockers"`
}

// Recovery is a trusted administrator/human-ingress API, deliberately absent
// from the Agent command registry. Candidate IDs alone never imply ownership.
type Recovery struct {
	InteractionID, ViewID, CandidateURL                           string
	OperationID, StepID, ExternalID, SecondaryID, SourceMessageID string
	ExpectedRevision                                              int64
}
