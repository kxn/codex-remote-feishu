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
	PreviewGrantKey  string
	Block            render.Block
}

type FinalBlockPreviewResult struct {
	Block       render.Block
	Supplements []PreviewSupplement
}

type PreviewLocation struct {
	Line   int
	Column int
}

func (l PreviewLocation) Valid() bool {
	return l.Line > 0
}

func (l PreviewLocation) QueryValue() string {
	if l.Line <= 0 {
		return ""
	}
	if l.Column > 0 {
		return "L" + previewItoa(l.Line) + "C" + previewItoa(l.Column)
	}
	return "L" + previewItoa(l.Line)
}

func (l PreviewLocation) FragmentID() string {
	if l.Line <= 0 {
		return ""
	}
	return "L" + previewItoa(l.Line)
}

type PreviewSupplement struct {
	Kind string         `json:"kind,omitempty"`
	Data map[string]any `json:"data,omitempty"`
}

type PreviewReference struct {
	RawTarget   string
	TargetStart int
	TargetEnd   int
	Location    PreviewLocation
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
	Reference  PreviewReference
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

type WebPreviewGrantRequest struct {
	ScopePublicID string
	GrantKey      string
}

type WebPreviewPublisher interface {
	IssueScopePrefix(context.Context, WebPreviewGrantRequest) (string, error)
}

type WebPreviewConfigurable interface {
	SetWebPreviewPublisher(WebPreviewPublisher)
}

type WebPreviewRouteService interface {
	ServeWebPreview(http.ResponseWriter, *http.Request, string, string, bool) bool
}

func previewItoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + (value % 10))
		value /= 10
	}
	return string(buf[i:])
}
