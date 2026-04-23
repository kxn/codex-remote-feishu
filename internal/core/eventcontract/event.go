package eventcontract

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

type EventMeta struct {
	Target               TargetRef
	SourceMessageID      string
	SourceMessagePreview string
	DaemonLifecycleID    string
	InlineReplaceMode    InlineReplaceMode
	Semantics            DeliverySemantics
}

func (meta EventMeta) Normalized() EventMeta {
	meta.Target = meta.Target.Normalized()
	meta.SourceMessageID = strings.TrimSpace(meta.SourceMessageID)
	meta.SourceMessagePreview = strings.TrimSpace(meta.SourceMessagePreview)
	meta.DaemonLifecycleID = strings.TrimSpace(meta.DaemonLifecycleID)
	switch meta.InlineReplaceMode {
	case InlineReplaceCurrentCard:
	default:
		meta.InlineReplaceMode = InlineReplaceNone
	}
	meta.Semantics = meta.Semantics.Normalized()
	return meta
}

type Event struct {
	Kind                     Kind
	GatewayID                string
	SurfaceSessionID         string
	DaemonLifecycleID        string
	SourceMessageID          string
	SourceMessagePreview     string
	InlineReplaceCurrentCard bool
	Snapshot                 *control.Snapshot
	SelectionView            *control.FeishuSelectionView
	SelectionContext         *control.FeishuUISelectionContext
	PageView                 *control.FeishuPageView
	PageContext              *control.FeishuUIPageContext
	RequestView              *control.FeishuRequestView
	RequestContext           *control.FeishuUIRequestContext
	PathPickerView           *control.FeishuPathPickerView
	PathPickerContext        *control.FeishuUIPathPickerContext
	TargetPickerView         *control.FeishuTargetPickerView
	TargetPickerContext      *control.FeishuUITargetPickerContext
	ThreadHistoryView        *control.FeishuThreadHistoryView
	ThreadHistoryContext     *control.FeishuUIThreadHistoryContext
	PendingInput             *control.PendingInputState
	Notice                   *control.Notice
	PlanUpdate               *control.PlanUpdate
	ThreadSelection          *control.ThreadSelectionChanged
	Block                    *render.Block
	TimelineText             *control.TimelineText
	ImageOutput              *control.ImageOutput
	ExecCommandProgress      *control.ExecCommandProgress
	FileChangeSummary        *control.FileChangeSummary
	TurnDiffSnapshot         *control.TurnDiffSnapshot
	FinalTurnSummary         *control.FinalTurnSummary
	Command                  *agentproto.Command
	DaemonCommand            *control.DaemonCommand
	Meta                     EventMeta
	Payload                  Payload
}

func (event Event) Normalized() Event {
	event.GatewayID = strings.TrimSpace(event.GatewayID)
	event.SurfaceSessionID = strings.TrimSpace(event.SurfaceSessionID)
	event.DaemonLifecycleID = strings.TrimSpace(event.DaemonLifecycleID)
	event.SourceMessageID = strings.TrimSpace(event.SourceMessageID)
	event.SourceMessagePreview = strings.TrimSpace(event.SourceMessagePreview)

	event.Meta = event.Meta.Normalized()
	if event.Meta.Target.GatewayID == "" {
		event.Meta.Target.GatewayID = event.GatewayID
	}
	if event.Meta.Target.SurfaceSessionID == "" {
		event.Meta.Target.SurfaceSessionID = event.SurfaceSessionID
	}
	if event.GatewayID == "" {
		event.GatewayID = event.Meta.Target.GatewayID
	}
	if event.SurfaceSessionID == "" {
		event.SurfaceSessionID = event.Meta.Target.SurfaceSessionID
	}
	if event.Meta.SourceMessageID == "" {
		event.Meta.SourceMessageID = event.SourceMessageID
	}
	if event.Meta.SourceMessagePreview == "" {
		event.Meta.SourceMessagePreview = event.SourceMessagePreview
	}
	if event.SourceMessageID == "" {
		event.SourceMessageID = event.Meta.SourceMessageID
	}
	if event.SourceMessagePreview == "" {
		event.SourceMessagePreview = event.Meta.SourceMessagePreview
	}
	if event.Meta.DaemonLifecycleID == "" {
		event.Meta.DaemonLifecycleID = event.DaemonLifecycleID
	}
	if event.DaemonLifecycleID == "" {
		event.DaemonLifecycleID = event.Meta.DaemonLifecycleID
	}
	if event.Meta.InlineReplaceMode == InlineReplaceNone && event.InlineReplaceCurrentCard {
		event.Meta.InlineReplaceMode = InlineReplaceCurrentCard
	}
	if !event.InlineReplaceCurrentCard {
		event.InlineReplaceCurrentCard = event.Meta.InlineReplaceMode == InlineReplaceCurrentCard
	}

	// When callers provide payload-first events, hydrate flat fields so current
	// in-repo callers can still read event data from the Event root.
	switch payload := event.Payload.(type) {
	case SnapshotPayload:
		if event.Snapshot == nil {
			snapshot := payload.Snapshot
			event.Snapshot = &snapshot
		}
	case SelectionPayload:
		if event.SelectionView == nil {
			view := payload.View
			event.SelectionView = &view
		}
		if event.SelectionContext == nil {
			event.SelectionContext = cloneSelectionContext(payload.Context)
		}
	case PagePayload:
		if event.PageView == nil {
			view := payload.View
			event.PageView = &view
		}
		if event.PageContext == nil {
			event.PageContext = clonePageContext(payload.Context)
		}
	case RequestPayload:
		if event.RequestView == nil {
			view := payload.View
			event.RequestView = &view
		}
		if event.RequestContext == nil {
			event.RequestContext = cloneRequestContext(payload.Context)
		}
	case PathPickerPayload:
		if event.PathPickerView == nil {
			view := payload.View
			event.PathPickerView = &view
		}
		if event.PathPickerContext == nil {
			event.PathPickerContext = clonePathPickerContext(payload.Context)
		}
	case TargetPickerPayload:
		if event.TargetPickerView == nil {
			view := payload.View
			event.TargetPickerView = &view
		}
		if event.TargetPickerContext == nil {
			event.TargetPickerContext = cloneTargetPickerContext(payload.Context)
		}
	case ThreadHistoryPayload:
		if event.ThreadHistoryView == nil {
			view := payload.View
			event.ThreadHistoryView = &view
		}
		if event.ThreadHistoryContext == nil {
			event.ThreadHistoryContext = cloneThreadHistoryContext(payload.Context)
		}
	case PendingInputPayload:
		if event.PendingInput == nil {
			state := payload.State
			event.PendingInput = &state
		}
	case NoticePayload:
		if event.Notice == nil {
			notice := payload.Notice
			event.Notice = &notice
		}
		if event.ThreadSelection == nil {
			event.ThreadSelection = cloneThreadSelection(payload.ThreadSelection)
		}
	case PlanUpdatePayload:
		if event.PlanUpdate == nil {
			plan := payload.PlanUpdate
			event.PlanUpdate = &plan
		}
	case BlockCommittedPayload:
		if event.Block == nil {
			block := payload.Block
			event.Block = &block
		}
		if event.FileChangeSummary == nil {
			event.FileChangeSummary = cloneFileChangeSummary(payload.FileChangeSummary)
		}
		if event.TurnDiffSnapshot == nil {
			event.TurnDiffSnapshot = cloneTurnDiffSnapshot(payload.TurnDiffSnapshot)
		}
		if event.FinalTurnSummary == nil {
			event.FinalTurnSummary = cloneFinalTurnSummary(payload.FinalTurnSummary)
		}
	case TimelineTextPayload:
		if event.TimelineText == nil {
			text := payload.TimelineText
			event.TimelineText = &text
		}
	case ImageOutputPayload:
		if event.ImageOutput == nil {
			output := payload.ImageOutput
			event.ImageOutput = &output
		}
	case ExecCommandProgressPayload:
		if event.ExecCommandProgress == nil {
			progress := payload.Progress
			event.ExecCommandProgress = &progress
		}
	case AgentCommandPayload:
		if event.Command == nil {
			command := payload.Command
			event.Command = &command
		}
	case DaemonCommandPayload:
		if event.DaemonCommand == nil {
			command := payload.Command
			event.DaemonCommand = &command
		}
	}

	event.Kind = event.CanonicalKind()
	if event.Payload == nil {
		event.Payload = event.CanonicalPayload()
	}
	if event.Meta.Semantics == (DeliverySemantics{}) {
		event.Meta.Semantics = event.CanonicalSemantics()
	}
	event.Meta.Semantics = event.Meta.Semantics.Normalized()
	return event
}
