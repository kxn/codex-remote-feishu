package control

// FeishuUIDTOwner identifies which layer currently owns the DTO shape exposed
// to the Feishu adapter. Some UI events still intentionally cross the boundary
// as Feishu-facing DTOs instead of neutral read models.
type FeishuUIDTOwner string

const (
	FeishuUIDTOwnerDirectDTO FeishuUIDTOwner = "feishu_direct_dto"
	FeishuUIDTOwnerSelection FeishuUIDTOwner = "feishu_selection_view"
	FeishuUIDTOwnerCommand   FeishuUIDTOwner = "feishu_command_view"
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
