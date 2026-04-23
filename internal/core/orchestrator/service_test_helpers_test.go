package orchestrator

import (
	"strings"
	"testing"
	"time"

	feishuadapter "github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func newServiceForTest(now *time.Time) *Service {
	return NewService(func() time.Time { return *now }, Config{TurnHandoffWait: 800 * time.Millisecond, GitAvailable: true}, renderer.NewPlanner())
}

func materializeVSCodeSurfaceForTest(svc *Service, surfaceID string) {
	svc.MaterializeSurface(surfaceID, "app-1", "chat-1", "user-1")
	svc.root.Surfaces[surfaceID].ProductMode = state.ProductModeVSCode
}

func firstCommands(entries []control.CommandCatalogEntry) []string {
	commands := make([]string, 0, len(entries))
	for _, entry := range entries {
		if len(entry.Commands) == 0 {
			continue
		}
		commands = append(commands, entry.Commands[0])
	}
	return commands
}

func firstButtonLabels(entries []control.CommandCatalogEntry) []string {
	labels := make([]string, 0, len(entries))
	for _, entry := range entries {
		if len(entry.Buttons) == 0 {
			continue
		}
		labels = append(labels, entry.Buttons[0].Label)
	}
	return labels
}

func eventSelectionPrompt(event eventcontract.Event) (*control.FeishuDirectSelectionPrompt, bool) {
	if event.SelectionView != nil {
		prompt, ok := feishuadapter.FeishuDirectSelectionPromptFromView(*event.SelectionView, event.SelectionContext)
		if !ok {
			return nil, false
		}
		return &prompt, true
	}
	return nil, false
}

func selectionPromptFromEvent(t *testing.T, event eventcontract.Event) *control.FeishuDirectSelectionPrompt {
	t.Helper()
	prompt, ok := eventSelectionPrompt(event)
	if !ok {
		t.Fatalf("expected selection prompt or selection view, got %#v", event)
	}
	return prompt
}

func targetPickerFromEvent(t *testing.T, event eventcontract.Event) *control.FeishuTargetPickerView {
	t.Helper()
	if event.TargetPickerView == nil {
		t.Fatalf("expected target picker view, got %#v", event)
	}
	return event.TargetPickerView
}

func selectionViewFromEvent(t *testing.T, event eventcontract.Event) *control.FeishuSelectionView {
	t.Helper()
	if event.SelectionView == nil {
		t.Fatalf("expected selection view, got %#v", event)
	}
	return event.SelectionView
}

func singleTargetPickerEvent(t *testing.T, events []eventcontract.Event) *control.FeishuTargetPickerView {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("expected exactly one event, got %#v", events)
	}
	return targetPickerFromEvent(t, events[0])
}

func targetPickerWorkspaceOption(view *control.FeishuTargetPickerView, value string) (control.FeishuTargetPickerWorkspaceOption, bool) {
	if view == nil {
		return control.FeishuTargetPickerWorkspaceOption{}, false
	}
	for _, option := range view.WorkspaceOptions {
		if option.Value == value {
			return option, true
		}
	}
	return control.FeishuTargetPickerWorkspaceOption{}, false
}

func targetPickerSessionOption(view *control.FeishuTargetPickerView, value string) (control.FeishuTargetPickerSessionOption, bool) {
	if view == nil {
		return control.FeishuTargetPickerSessionOption{}, false
	}
	for _, option := range view.SessionOptions {
		if option.Value == value {
			return option, true
		}
	}
	return control.FeishuTargetPickerSessionOption{}, false
}

func targetPickerModeOption(view *control.FeishuTargetPickerView, value control.FeishuTargetPickerMode) (control.FeishuTargetPickerModeOption, bool) {
	if view == nil {
		return control.FeishuTargetPickerModeOption{}, false
	}
	for _, option := range view.ModeOptions {
		if option.Value == value {
			return option, true
		}
	}
	return control.FeishuTargetPickerModeOption{}, false
}

func targetPickerSourceOption(view *control.FeishuTargetPickerView, value control.FeishuTargetPickerSourceKind) (control.FeishuTargetPickerSourceOption, bool) {
	if view == nil {
		return control.FeishuTargetPickerSourceOption{}, false
	}
	for _, option := range view.SourceOptions {
		if option.Value == value {
			return option, true
		}
	}
	return control.FeishuTargetPickerSourceOption{}, false
}

func requestPromptFromEvent(t *testing.T, event eventcontract.Event) *control.FeishuRequestView {
	t.Helper()
	if event.RequestView == nil {
		t.Fatalf("expected request prompt event, got %#v", event)
	}
	return event.RequestView
}

func singleRequestPromptEvent(t *testing.T, events []eventcontract.Event) *control.FeishuRequestView {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("expected exactly one event, got %#v", events)
	}
	return requestPromptFromEvent(t, events[0])
}

func eventCommandCatalog(event eventcontract.Event) (*control.FeishuPageView, bool) {
	if event.PageView != nil {
		page := control.NormalizeFeishuPageView(*event.PageView)
		catalog := control.NormalizeFeishuPageView(control.FeishuPageView{
			CommandID:                     page.CommandID,
			Title:                         page.Title,
			MessageID:                     page.MessageID,
			TrackingKey:                   page.TrackingKey,
			ThemeKey:                      page.ThemeKey,
			Patchable:                     page.Patchable,
			Breadcrumbs:                   append([]control.CommandCatalogBreadcrumb(nil), page.Breadcrumbs...),
			SummarySections:               append([]control.FeishuCardTextSection(nil), page.SummarySections...),
			BodySections:                  append([]control.FeishuCardTextSection(nil), page.BodySections...),
			NoticeSections:                append([]control.FeishuCardTextSection(nil), page.NoticeSections...),
			StatusKind:                    page.StatusKind,
			StatusText:                    page.StatusText,
			Interactive:                   page.Interactive,
			Sealed:                        page.Sealed,
			DisplayStyle:                  page.DisplayStyle,
			Sections:                      append([]control.CommandCatalogSection(nil), page.Sections...),
			RelatedButtons:                append([]control.CommandCatalogButton(nil), page.RelatedButtons...),
			SuppressDefaultRelatedButtons: page.SuppressDefaultRelatedButtons,
		})
		return &catalog, true
	}
	return nil, false
}

func commandCatalogFromEvent(t *testing.T, event eventcontract.Event) *control.FeishuPageView {
	t.Helper()
	catalog, ok := eventCommandCatalog(event)
	if !ok {
		t.Fatalf("expected page catalog event, got %#v", event)
	}
	return catalog
}

func commandCatalogSummaryText(catalog *control.FeishuPageView) string {
	if catalog == nil {
		return ""
	}
	normalizedPage := control.NormalizeFeishuPageView(*catalog)
	parts := []string{}
	sections := control.BuildFeishuPageBodySections(normalizedPage)
	for _, section := range sections {
		normalized := section.Normalized()
		if normalized.Label != "" {
			parts = append(parts, normalized.Label)
		}
		parts = append(parts, normalized.Lines...)
	}
	for _, section := range control.BuildFeishuPageNoticeSections(normalizedPage) {
		normalized := section.Normalized()
		if normalized.Label != "" {
			parts = append(parts, normalized.Label)
		}
		parts = append(parts, normalized.Lines...)
	}
	return strings.Join(parts, "\n")
}

func singleSelectionPromptEvent(t *testing.T, events []eventcontract.Event) *control.FeishuDirectSelectionPrompt {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("expected exactly one event, got %#v", events)
	}
	return selectionPromptFromEvent(t, events[0])
}

func findSelectionPromptByKind(t *testing.T, events []eventcontract.Event, kind control.SelectionPromptKind) *control.FeishuDirectSelectionPrompt {
	t.Helper()
	for _, event := range events {
		prompt, ok := eventSelectionPrompt(event)
		if ok && prompt.Kind == kind {
			return prompt
		}
	}
	return nil
}

type fakePersistedThreadCatalog struct {
	recent              []state.ThreadRecord
	recentWorkspaces    map[string]time.Time
	byID                map[string]state.ThreadRecord
	recentErr           error
	recentWorkspacesErr error
	byIDErr             error
}

func (f *fakePersistedThreadCatalog) RecentThreads(limit int) ([]state.ThreadRecord, error) {
	if f == nil {
		return nil, nil
	}
	if f.recentErr != nil {
		return nil, f.recentErr
	}
	if limit <= 0 || limit >= len(f.recent) {
		return append([]state.ThreadRecord(nil), f.recent...), nil
	}
	return append([]state.ThreadRecord(nil), f.recent[:limit]...), nil
}

func (f *fakePersistedThreadCatalog) RecentWorkspaces(limit int) (map[string]time.Time, error) {
	if f == nil {
		return nil, nil
	}
	if f.recentWorkspacesErr != nil {
		return nil, f.recentWorkspacesErr
	}
	if len(f.recentWorkspaces) == 0 {
		return nil, nil
	}
	out := make(map[string]time.Time, len(f.recentWorkspaces))
	for workspaceKey, usedAt := range f.recentWorkspaces {
		out[workspaceKey] = usedAt
	}
	return out, nil
}

func (f *fakePersistedThreadCatalog) ThreadByID(threadID string) (*state.ThreadRecord, error) {
	if f == nil {
		return nil, nil
	}
	if f.byIDErr != nil {
		return nil, f.byIDErr
	}
	thread, ok := f.byID[threadID]
	if !ok {
		return nil, nil
	}
	threadCopy := thread
	return &threadCopy, nil
}

func recordLocalFinalText(t *testing.T, svc *Service, instanceID, threadID, turnID, itemID, text string) []eventcontract.Event {
	t.Helper()
	if events := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: threadID,
		TurnID:   turnID,
		ItemID:   itemID,
		ItemKind: "agent_message",
		Delta:    text,
	}); len(events) != 0 {
		t.Fatalf("expected no UI events while collecting local text, got %#v", events)
	}
	if events := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: threadID,
		TurnID:   turnID,
		ItemID:   itemID,
		ItemKind: "agent_message",
	}); len(events) != 0 {
		t.Fatalf("expected no UI events before local turn completion, got %#v", events)
	}
	return svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  threadID,
		TurnID:    turnID,
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
}

func setupAutoContinueSurface(t *testing.T, svc *Service) *state.SurfaceConsoleRecord {
	t.Helper()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface == nil {
		t.Fatal("expected attached surface")
	}
	surface.AutoContinue.Enabled = true
	return surface
}

func startRemoteTurnForAutoContinueTest(t *testing.T, svc *Service, messageID, text, turnID string) {
	t.Helper()
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        messageID,
		Text:             text,
	})
	if len(events) == 0 {
		t.Fatal("expected enqueue events")
	}
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    turnID,
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
}

func completeRemoteTurnWithFinalText(t *testing.T, svc *Service, turnID, status, errorMessage, finalText string, problem *agentproto.ErrorInfo) []eventcontract.Event {
	t.Helper()
	if strings.TrimSpace(finalText) != "" {
		if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
			Kind:     agentproto.EventItemDelta,
			ThreadID: "thread-1",
			TurnID:   turnID,
			ItemID:   "item-" + turnID,
			ItemKind: "agent_message",
			Delta:    finalText,
		}); len(events) != 0 {
			t.Fatalf("expected no UI events while collecting remote final text, got %#v", events)
		}
		if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
			Kind:     agentproto.EventItemCompleted,
			ThreadID: "thread-1",
			TurnID:   turnID,
			ItemID:   "item-" + turnID,
			ItemKind: "agent_message",
		}); len(events) != 0 {
			t.Fatalf("expected no UI events before remote turn completion, got %#v", events)
		}
	}
	return svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnCompleted,
		ThreadID:     "thread-1",
		TurnID:       turnID,
		Status:       status,
		ErrorMessage: errorMessage,
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
		Problem:      problem,
	})
}
