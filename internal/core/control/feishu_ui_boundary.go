package control

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

// FeishuUIDTOwner identifies which layer currently owns the DTO shape exposed
// to the Feishu adapter. Some UI events still intentionally cross the boundary
// as Feishu-facing DTOs instead of neutral read models.
type FeishuUIDTOwner string

const (
	FeishuUIDTOwnerSelection     FeishuUIDTOwner = "feishu_selection_view"
	FeishuUIDTOwnerPage          FeishuUIDTOwner = "feishu_page_view"
	FeishuUIDTOwnerRequest       FeishuUIDTOwner = "feishu_request_view"
	FeishuUIDTOwnerPathPicker    FeishuUIDTOwner = "feishu_path_picker_view"
	FeishuUIDTOwnerTargetPicker  FeishuUIDTOwner = "feishu_target_picker_view"
	FeishuUIDTOwnerThreadHistory FeishuUIDTOwner = "feishu_thread_history_view"
)

// FeishuUICallbackPayloadOwner identifies the layer that owns callback payload
// schema definitions shared by the Feishu projector and gateway.
type FeishuUICallbackPayloadOwner string

const (
	FeishuUICallbackPayloadOwnerAdapter FeishuUICallbackPayloadOwner = "feishu_adapter_payload"
)

// FeishuUIOwnerCardFlowContext describes the stable owner-card flow runtime
// state currently attached to a surface or specific business card.
type FeishuUIOwnerCardFlowContext struct {
	FlowID    string
	Kind      string
	Revision  int
	Phase     string
	MessageID string
}

// FeishuUISurfaceContext is the stable read-only surface summary used by the
// Feishu UI layer. It deliberately mirrors only queryable product state and
// policy signals, not mutable orchestrator internals.
type FeishuUISurfaceContext struct {
	SurfaceSessionID               string
	GatewayID                      string
	ProductMode                    string
	AttachedInstanceID             string
	CurrentWorkspaceKey            string
	RouteMode                      string
	SelectedThreadID               string
	Gate                           GateSummary
	RouteMutationBlocked           bool
	RouteMutationBlockedBy         string
	InlineReplaceFreshness         string
	InlineReplaceRequiresFreshness bool
	InlineReplaceViewSession       string
	InlineReplaceRequiresViewState bool
	CallbackPayloadOwner           FeishuUICallbackPayloadOwner
	ActiveOwnerCard                *FeishuUIOwnerCardFlowContext
}

// FeishuUISelectionContext describes the stable query/policy inputs that back a
// selection prompt while the rendered DTO itself remains Feishu-facing.
type FeishuUISelectionContext struct {
	DTOOwner         FeishuUIDTOwner
	Surface          FeishuUISurfaceContext
	PromptKind       SelectionPromptKind
	CatalogFamilyID  string
	CatalogVariantID string
	CatalogBackend   agentproto.Backend
	ViewMode         string
	Layout           string
	Title            string
	ContextTitle     string
	ContextText      string
	ContextKey       string
}

// FeishuUIPageContext describes the stable query/policy inputs for the
// generic page-card family.
type FeishuUIPageContext struct {
	DTOOwner  FeishuUIDTOwner
	Surface   FeishuUISurfaceContext
	PageID    string
	CommandID string
	Title     string
}

// FeishuUIRequestContext describes the stable query/policy inputs that back a
// UI-owned request view across the Feishu UI boundary.
type FeishuUIRequestContext struct {
	DTOOwner    FeishuUIDTOwner
	Surface     FeishuUISurfaceContext
	RequestID   string
	RequestType string
	ThreadID    string
	ThreadTitle string
	Title       string
}

// FeishuUIPathPickerContext describes the stable query/policy inputs backing
// the reusable Feishu path picker card.
type FeishuUIPathPickerContext struct {
	DTOOwner     FeishuUIDTOwner
	Surface      FeishuUISurfaceContext
	PickerID     string
	Mode         PathPickerMode
	Title        string
	RootPath     string
	CurrentPath  string
	SelectedPath string
}

// FeishuUITargetPickerContext describes the stable query/policy inputs backing
// the unified workspace/session target picker card.
type FeishuUITargetPickerContext struct {
	DTOOwner                 FeishuUIDTOwner
	Surface                  FeishuUISurfaceContext
	PickerID                 string
	Source                   TargetPickerRequestSource
	CatalogFamilyID          string
	CatalogVariantID         string
	CatalogBackend           agentproto.Backend
	Title                    string
	Page                     FeishuTargetPickerPage
	WorkspaceSelectionLocked bool
	LockedWorkspaceKey       string
	AllowNewThread           bool
	SelectedWorkspaceKey     string
	SelectedSessionValue     string
}

// FeishuUIThreadHistoryContext describes the stable query/policy inputs
// backing the /history list/detail card flow.
type FeishuUIThreadHistoryContext struct {
	DTOOwner       FeishuUIDTOwner
	Surface        FeishuUISurfaceContext
	OwnerCard      *FeishuUIOwnerCardFlowContext
	PickerID       string
	ThreadID       string
	Mode           FeishuThreadHistoryViewMode
	Page           int
	SelectedTurnID string
	Loading        bool
}
