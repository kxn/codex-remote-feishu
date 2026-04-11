package control

// FeishuUIDTOOwner identifies which layer currently owns the DTO shape exposed
// to the Feishu adapter. Phase 1 keeps these as Feishu-oriented transition DTOs
// while the query/policy boundary is made explicit.
type FeishuUIDTOOwner string

const (
	FeishuUIDTOwnerTransition FeishuUIDTOOwner = "feishu_transition_dto"
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
	CallbackPayloadOwner           FeishuUICallbackPayloadOwner
}

// FeishuUISelectionContext describes the stable query/policy inputs that back a
// selection prompt while the DTO itself remains a Feishu-owned transition type.
type FeishuUISelectionContext struct {
	DTOOwner     FeishuUIDTOOwner
	Surface      FeishuUISurfaceContext
	PromptKind   SelectionPromptKind
	Layout       string
	Title        string
	ContextTitle string
	ContextText  string
	ContextKey   string
}

// FeishuUICommandContext describes the stable query/policy inputs that back a
// command catalog while the catalog DTO remains Feishu-owned in this phase.
type FeishuUICommandContext struct {
	DTOOwner    FeishuUIDTOOwner
	Surface     FeishuUISurfaceContext
	MenuStage   string
	MenuView    string
	Title       string
	Summary     string
	Breadcrumbs []CommandCatalogBreadcrumb
}

// FeishuUIRequestContext describes the stable query/policy inputs that back a
// request prompt while the prompt DTO remains a Feishu transition type.
type FeishuUIRequestContext struct {
	DTOOwner    FeishuUIDTOOwner
	Surface     FeishuUISurfaceContext
	RequestID   string
	RequestType string
	ThreadID    string
	ThreadTitle string
	Title       string
}
