package feishu

import (
	"context"
	"net/http"

	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

type FinalBlockPreviewService interface {
	RewriteFinalBlock(context.Context, FinalBlockPreviewRequest) (FinalBlockPreviewResult, error)
}

type FinalBlockPreviewMaintenanceService interface {
	RunBackgroundMaintenance(context.Context)
}

type FinalBlockPreviewRequest struct {
	GatewayID        string
	SurfaceSessionID string
	ChatID           string
	ActorUserID      string
	WorkspaceRoot    string
	ThreadCWD        string
	Block            render.Block
}

type FinalBlockPreviewResult struct {
	Block       render.Block
	Supplements []PreviewSupplement
}

type PreviewSupplement struct {
	Kind string         `json:"kind,omitempty"`
	Data map[string]any `json:"data,omitempty"`
}

type PreviewReference struct {
	RawTarget   string
	TargetStart int
	TargetEnd   int
}

type PreviewPlan struct {
	HandlerID  string
	Artifact   PreparedPreviewArtifact
	Deliveries []PreviewDeliveryPlan
}

type PreparedPreviewArtifact struct {
	SourcePath   string
	DisplayName  string
	ContentHash  string
	ArtifactKind string
	MIMEType     string
	RendererKind string
	Text         string
	Bytes        []byte
}

type PreviewDeliveryKind string

const (
	PreviewDeliveryDriveFileLink PreviewDeliveryKind = "drive_file_link"
	PreviewDeliveryWebFileLink   PreviewDeliveryKind = "web_file_link"
)

type PreviewDeliveryPlan struct {
	Kind     PreviewDeliveryKind
	Metadata map[string]any
}

type PreviewPublishMode string

const (
	PreviewPublishModeInlineLink PreviewPublishMode = "inline_link"
	PreviewPublishModeSupplement PreviewPublishMode = "supplement"
)

type PreviewPublishResult struct {
	PublisherID string
	Mode        PreviewPublishMode
	URL         string
	Supplements []PreviewSupplement
}

type PreviewPublishRequest struct {
	Request    FinalBlockPreviewRequest
	Plan       PreviewPlan
	Delivery   PreviewDeliveryPlan
	State      *previewState
	ScopeKey   string
	Principals []previewPrincipal
	Runtime    *previewRewriteRuntime
}

type FinalBlockPreviewHandler interface {
	ID() string
	Match(FinalBlockPreviewRequest, PreviewReference) bool
	Plan(context.Context, FinalBlockPreviewRequest, PreviewReference) (*PreviewPlan, bool, error)
}

type FinalBlockPreviewPublisher interface {
	ID() string
	Supports(PreviewDeliveryPlan, PreparedPreviewArtifact) bool
	Publish(context.Context, PreviewPublishRequest) (*PreviewPublishResult, bool, error)
}

type MarkdownPreviewService = FinalBlockPreviewService
type MarkdownPreviewRequest = FinalBlockPreviewRequest

type WebPreviewPublisher interface {
	IssueScopePrefix(context.Context, string) (string, error)
}

type WebPreviewConfigurable interface {
	SetWebPreviewPublisher(WebPreviewPublisher)
}

type WebPreviewRouteService interface {
	ServeWebPreview(http.ResponseWriter, *http.Request, string, string, bool) bool
}
