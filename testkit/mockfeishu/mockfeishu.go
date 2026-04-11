package mockfeishu

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

type Recorder struct {
	Events           []control.UIEvent
	Notices          []string
	Blocks           []render.Block
	TypingOnFor      []string
	TypingOffFor     []string
	ThumbsUpFor      []string
	ThumbsDownFor    []string
	SelectionPrompts []control.FeishuDirectSelectionPrompt
}

func NewRecorder() *Recorder {
	return &Recorder{}
}

func (r *Recorder) Apply(events []control.UIEvent) {
	for _, event := range events {
		r.Events = append(r.Events, event)
		switch event.Kind {
		case control.UIEventNotice:
			if event.Notice != nil {
				r.Notices = append(r.Notices, event.Notice.Text)
			}
		case control.UIEventBlockCommitted:
			if event.Block != nil {
				r.Blocks = append(r.Blocks, *event.Block)
			}
		case control.UIEventPendingInput:
			if event.PendingInput == nil {
				continue
			}
			if event.PendingInput.TypingOn {
				r.TypingOnFor = append(r.TypingOnFor, event.PendingInput.SourceMessageID)
			}
			if event.PendingInput.TypingOff {
				r.TypingOffFor = append(r.TypingOffFor, event.PendingInput.SourceMessageID)
			}
			if event.PendingInput.ThumbsUp {
				r.ThumbsUpFor = append(r.ThumbsUpFor, event.PendingInput.SourceMessageID)
			}
			if event.PendingInput.ThumbsDown {
				r.ThumbsDownFor = append(r.ThumbsDownFor, event.PendingInput.SourceMessageID)
			}
		case control.UIEventFeishuDirectSelectionPrompt:
			if event.FeishuDirectSelectionPrompt != nil {
				r.SelectionPrompts = append(r.SelectionPrompts, *event.FeishuDirectSelectionPrompt)
			}
		}
	}
}
