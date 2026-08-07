package mockfeishu

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

type Recorder struct {
	Events         []eventcontract.Event
	Notices        []string
	Blocks         []render.Block
	TypingOnFor    []string
	TypingOffFor   []string
	ThumbsUpFor    []string
	ThumbsDownFor  []string
	SelectionViews []control.FeishuSelectionView
}

func NewRecorder() *Recorder {
	return &Recorder{}
}

func (r *Recorder) ApplyEvents(events []eventcontract.Event) {
	for _, event := range events {
		event = event.Normalized()
		r.Events = append(r.Events, event)
		switch payload := event.Payload.(type) {
		case eventcontract.NoticePayload:
			r.Notices = append(r.Notices, payload.Notice.Text)
		case eventcontract.BlockCommittedPayload:
			r.Blocks = append(r.Blocks, payload.Block)
		case eventcontract.PendingInputPayload:
			if payload.State.TypingOn {
				r.TypingOnFor = append(r.TypingOnFor, payload.State.SourceMessageID)
			}
			if payload.State.TypingOff {
				r.TypingOffFor = append(r.TypingOffFor, payload.State.SourceMessageID)
			}
			if payload.State.ThumbsUp {
				r.ThumbsUpFor = append(r.ThumbsUpFor, payload.State.SourceMessageID)
			}
			if payload.State.ThumbsDown {
				r.ThumbsDownFor = append(r.ThumbsDownFor, payload.State.SourceMessageID)
			}
		case eventcontract.SelectionPayload:
			r.SelectionViews = append(r.SelectionViews, payload.View)
		}
	}
}
