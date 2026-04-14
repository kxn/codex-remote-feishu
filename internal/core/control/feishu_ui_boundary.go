package control

// FeishuUIDTOwner identifies which layer currently owns the DTO shape exposed
// to the Feishu adapter. Some UI events still intentionally cross the boundary
// as Feishu-facing DTOs instead of neutral read models.
type FeishuUIDTOwner string

const (
	FeishuUIDTOwnerDirectDTO    FeishuUIDTOwner = "feishu_direct_dto"
	FeishuUIDTOwnerSelection    FeishuUIDTOwner = "feishu_selection_view"
	FeishuUIDTOwnerCommand      FeishuUIDTOwner = "feishu_command_view"
	FeishuUIDTOwnerPathPicker   FeishuUIDTOwner = "feishu_path_picker_view"
	FeishuUIDTOwnerTargetPicker FeishuUIDTOwner = "feishu_target_picker_view"
	FeishuUIDTOwnerThreadHistory FeishuUIDTOwner = "feishu_thread_history_view"
)

// FeishuUICallbackPayloadOwner identifies the layer that owns callback payload
// schema definitions shared by the Feishu projector and gateway.
type FeishuUICallbackPayloadOwner string

const (
	FeishuUICallbackPayloadOwnerAdapter FeishuUICallbackPayloadOwner = "feishu_adapter_payload"
)

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
}

// FeishuUISelectionContext describes the stable query/policy inputs that back a
// selection prompt while the rendered DTO itself remains Feishu-facing.
type FeishuUISelectionContext struct {
	DTOOwner     FeishuUIDTOwner
	Surface      FeishuUISurfaceContext
	PromptKind   SelectionPromptKind
	ViewMode     string
	Layout       string
	Title        string
	ContextTitle string
	ContextText  string
	ContextKey   string
}

// FeishuUICommandContext describes the stable query/policy inputs that back a
// command catalog while some command cards still remain Feishu-facing DTOs.
type FeishuUICommandContext struct {
	DTOOwner    FeishuUIDTOwner
	Surface     FeishuUISurfaceContext
	ViewKind    string
	MenuStage   string
	MenuView    string
	CommandID   string
	NeedsTarget bool
	Title       string
	Summary     string
	Breadcrumbs []CommandCatalogBreadcrumb
}

// FeishuUIRequestContext describes the stable query/policy inputs that back a
// request prompt while the request card still remains Feishu-facing.
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
	DTOOwner             FeishuUIDTOwner
	Surface              FeishuUISurfaceContext
	PickerID             string
	Source               TargetPickerRequestSource
	Title                string
	SelectedWorkspaceKey string
	SelectedSessionValue string
}

// FeishuUIThreadHistoryContext describes the stable query/policy inputs
// backing the /history list/detail card flow.
type FeishuUIThreadHistoryContext struct {
	DTOOwner       FeishuUIDTOwner
	Surface        FeishuUISurfaceContext
	PickerID       string
	ThreadID       string
	Mode           FeishuThreadHistoryViewMode
	Page           int
	SelectedTurnID string
	Loading        bool
}
