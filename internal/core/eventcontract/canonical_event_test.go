package eventcontract

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestCanonicalKindPrefersPayload(t *testing.T) {
	event := Event{
		Kind:          KindSelection,
		SelectionView: &control.FeishuSelectionView{},
		Payload: RequestPayload{
			View: control.FeishuRequestView{RequestID: "request-1"},
		},
	}

	if got := event.CanonicalKind(); got != KindRequest {
		t.Fatalf("CanonicalKind() = %q, want %q", got, KindRequest)
	}
}

func TestCanonicalPayloadBuildsFromCanonicalRootFields(t *testing.T) {
	event := Event{
		RequestView:    &control.FeishuRequestView{RequestID: "request-1"},
		RequestContext: &control.FeishuUIRequestContext{ThreadID: "thread-1"},
	}

	canonicalPayload := event.CanonicalPayload()
	payload, ok := canonicalPayload.(RequestPayload)
	if !ok {
		t.Fatalf("CanonicalPayload() type = %T, want RequestPayload", canonicalPayload)
	}
	if payload.View.RequestID != "request-1" {
		t.Fatalf("payload view request id = %q, want request-1", payload.View.RequestID)
	}
	if payload.Context == nil || payload.Context.ThreadID != "thread-1" {
		t.Fatalf("payload context = %#v, want thread-1", payload.Context)
	}
}

func TestCanonicalSemanticsNoticeThreadSelection(t *testing.T) {
	event := Event{
		Payload: NoticePayload{
			Notice: control.Notice{
				Code:  "thread_selected",
				Title: "已切换线程",
			},
			ThreadSelection: &control.ThreadSelectionChanged{ThreadID: "thread-1"},
		},
	}

	got := event.CanonicalSemantics()
	want := DeliverySemantics{
		VisibilityClass:        VisibilityClassUINavigation,
		HandoffClass:           HandoffClassThreadSelection,
		FirstResultDisposition: FirstResultDispositionDrop,
		OwnerCardDisposition:   OwnerCardDispositionDrop,
	}
	if got != want {
		t.Fatalf("CanonicalSemantics() = %#v, want %#v", got, want)
	}
}

func TestCanonicalSemanticsForBlockCommitted(t *testing.T) {
	tests := []struct {
		name  string
		block render.Block
		want  DeliverySemantics
	}{
		{
			name:  "progress block",
			block: render.Block{Final: false},
			want: DeliverySemantics{
				VisibilityClass:        VisibilityClassProgressText,
				HandoffClass:           HandoffClassProcessDetail,
				FirstResultDisposition: FirstResultDispositionKeep,
				OwnerCardDisposition:   OwnerCardDispositionKeep,
			},
		},
		{
			name:  "final block",
			block: render.Block{Final: true},
			want: DeliverySemantics{
				VisibilityClass:        VisibilityClassAlwaysVisible,
				HandoffClass:           HandoffClassTerminalContent,
				FirstResultDisposition: FirstResultDispositionKeep,
				OwnerCardDisposition:   OwnerCardDispositionKeep,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := Event{
				Payload: BlockCommittedPayload{Block: tt.block},
			}
			if got := event.CanonicalSemantics(); got != tt.want {
				t.Fatalf("CanonicalSemantics() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestCanonicalSemanticsUsesExplicitOverride(t *testing.T) {
	event := Event{
		Payload: RequestPayload{
			View: control.FeishuRequestView{RequestID: "request-1"},
		},
		Meta: EventMeta{
			Semantics: DeliverySemantics{
				VisibilityClass: VisibilityClassProcessDetail,
				HandoffClass:    HandoffClassProcessDetail,
			},
		},
	}

	got := event.CanonicalSemantics()
	want := DeliverySemantics{
		VisibilityClass: VisibilityClassProcessDetail,
		HandoffClass:    HandoffClassProcessDetail,
	}
	if got != want {
		t.Fatalf("CanonicalSemantics() = %#v, want %#v", got, want)
	}
}

func TestCanonicalMessageDeliveryDefaults(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  MessageDelivery
	}{
		{
			name: "page patches top level message",
			event: Event{
				Payload: PagePayload{
					View: control.FeishuPageView{Title: "菜单"},
				},
			},
			want: MessageDelivery{
				FirstSendLane: MessageLaneTopLevel,
				Mutation:      MessageMutationPatchSameMessage,
			},
		},
		{
			name: "plan update stays top level append only",
			event: Event{
				Payload: PlanUpdatePayload{
					PlanUpdate: control.PlanUpdate{Explanation: "继续"},
				},
			},
			want: MessageDelivery{
				FirstSendLane: MessageLaneTopLevel,
				Mutation:      MessageMutationAppendOnly,
			},
		},
		{
			name: "timeline text defaults to reply thread append only",
			event: Event{
				Payload: TimelineTextPayload{
					TimelineText: control.TimelineText{Text: "处理中"},
				},
			},
			want: MessageDelivery{
				FirstSendLane: MessageLaneReplyThread,
				Mutation:      MessageMutationAppendOnly,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.CanonicalMessageDelivery(); got != tt.want {
				t.Fatalf("CanonicalMessageDelivery() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestFilterEventsByFollowupPolicyUsesCanonicalHandoffClass(t *testing.T) {
	events := []Event{
		{
			Payload: NoticePayload{
				Notice:          control.Notice{Code: "thread_selection_changed"},
				ThreadSelection: &control.ThreadSelectionChanged{ThreadID: "thread-1"},
			},
		},
		{
			Payload: NoticePayload{
				Notice: control.Notice{Code: "generic_notice"},
			},
		},
	}

	filtered := FilterEventsByFollowupPolicy(events, FollowupPolicy{
		DropClasses: []HandoffClass{HandoffClassThreadSelection},
	})
	if len(filtered) != 1 {
		t.Fatalf("expected one event after filtering thread selection, got %#v", filtered)
	}
	if got := filtered[0].CanonicalSemantics().HandoffClass; got != HandoffClassNotice {
		t.Fatalf("unexpected remaining handoff class %q", got)
	}
}

func TestEventMetaNormalizesAttentionAnnotation(t *testing.T) {
	meta := EventMeta{
		Attention: AttentionAnnotation{
			Text:          "  需要你回来处理：请确认这条请求。 \n",
			MentionUserID: " ou-user-1 ",
		},
	}.Normalized()

	want := AttentionAnnotation{
		Text:          "需要你回来处理：请确认这条请求。",
		MentionUserID: "ou-user-1",
	}
	if meta.Attention != want {
		t.Fatalf("normalized attention = %#v, want %#v", meta.Attention, want)
	}
	if got := (EventMeta{Attention: AttentionAnnotation{Text: "只有文案"}}).Normalized().Attention; got != (AttentionAnnotation{}) {
		t.Fatalf("expected incomplete attention annotation to normalize to zero, got %#v", got)
	}
}
