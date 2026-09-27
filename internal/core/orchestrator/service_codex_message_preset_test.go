package orchestrator

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestParseCodexMessagePreset(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		wantClean    string
		wantPreset   codexMessagePreset
		wantExplicit bool
		wantErr      bool
	}{
		{
			name:       "unprefixed leaves defaults to Codex",
			text:       "  diagnose this  ",
			wantClean:  "diagnose this",
			wantPreset: codexMessagePreset{},
		},
		{
			name:         "luna prefix",
			text:         "[luna] diagnose this",
			wantClean:    "diagnose this",
			wantPreset:   codexMessagePreset{Key: "luna", Model: "gpt-6-luna", ReasoningEffort: "high"},
			wantExplicit: true,
		},
		{
			name:         "terra prefix",
			text:         "[terra] fix this",
			wantClean:    "fix this",
			wantPreset:   codexMessagePreset{Key: "terra", Model: "gpt-5.6-terra", ReasoningEffort: "high"},
			wantExplicit: true,
		},
		{
			name:         "sol prefix",
			text:         "[sol] explain this",
			wantClean:    "explain this",
			wantPreset:   codexMessagePreset{Key: "sol", Model: "gpt-6-sol", ReasoningEffort: "high"},
			wantExplicit: true,
		},
		{
			name:         "astra prefix",
			text:         "[astra] solve this",
			wantClean:    "solve this",
			wantPreset:   codexMessagePreset{Key: "astra", Model: "gpt-6-astra", ReasoningEffort: "high"},
			wantExplicit: true,
		},
		{
			name:         "astra prefix with reasoning effort",
			text:         "[astra:xhigh] solve this",
			wantClean:    "solve this",
			wantPreset:   codexMessagePreset{Key: "astra", Model: "gpt-6-astra", ReasoningEffort: "xhigh"},
			wantExplicit: true,
		},
		{
			name:         "terra prefix with reasoning effort",
			text:         "[terra:medium] fix this",
			wantClean:    "fix this",
			wantPreset:   codexMessagePreset{Key: "terra", Model: "gpt-5.6-terra", ReasoningEffort: "medium"},
			wantExplicit: true,
		},
		{
			name:         "known model with unsupported reasoning effort rejects",
			text:         "[astra:ultra] solve this",
			wantClean:    "solve this",
			wantPreset:   codexMessagePreset{Key: "astra", Model: "gpt-6-astra", ReasoningEffort: "high"},
			wantExplicit: true,
			wantErr:      true,
		},
		{
			name:         "ASCII case insensitive",
			text:         "[TeRrA] fix this",
			wantClean:    "fix this",
			wantPreset:   codexMessagePreset{Key: "terra", Model: "gpt-5.6-terra", ReasoningEffort: "high"},
			wantExplicit: true,
		},
		{
			name:         "Unicode whitespace delimiter",
			text:         "\u3000[sol]\u3000explain this\u3000",
			wantClean:    "explain this",
			wantPreset:   codexMessagePreset{Key: "sol", Model: "gpt-6-sol", ReasoningEffort: "high"},
			wantExplicit: true,
		},
		{
			name:         "case insensitive prefix accepts attached Chinese body",
			text:         "[Sol]现在咋样了",
			wantClean:    "现在咋样了",
			wantPreset:   codexMessagePreset{Key: "sol", Model: "gpt-6-sol", ReasoningEffort: "high"},
			wantExplicit: true,
		},
		{
			name:       "unknown prefix is ordinary text",
			text:       "[max] explain this",
			wantClean:  "[max] explain this",
			wantPreset: codexMessagePreset{},
		},
		{
			name:         "prefix only",
			text:         "[sol]",
			wantPreset:   codexMessagePreset{Key: "sol", Model: "gpt-6-sol", ReasoningEffort: "high"},
			wantExplicit: true,
			wantErr:      true,
		},
		{
			name:         "prefix and whitespace only",
			text:         "[sol]   ",
			wantPreset:   codexMessagePreset{Key: "sol", Model: "gpt-6-sol", ReasoningEffort: "high"},
			wantExplicit: true,
			wantErr:      true,
		},
		{
			name:       "whitespace only",
			text:       " \u3000\t\n",
			wantPreset: codexMessagePreset{},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clean, preset, explicit, err := parseCodexMessagePreset(tt.text)
			if clean != tt.wantClean {
				t.Fatalf("clean = %q, want %q", clean, tt.wantClean)
			}
			if preset != tt.wantPreset {
				t.Fatalf("preset = %#v, want %#v", preset, tt.wantPreset)
			}
			if explicit != tt.wantExplicit {
				t.Fatalf("explicit = %t, want %t", explicit, tt.wantExplicit)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestValidCodexMessagePreset(t *testing.T) {
	if !validCodexMessagePreset("astra") {
		t.Fatal("astra must pass the frozen-preset dispatch guard")
	}
}

func TestCodexMessagePresetUsesPersistedTopicAndPreservesAccess(t *testing.T) {
	now := time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	surface := svc.root.Surfaces["surface-1"]
	surface.PromptOverride.AccessMode = agentproto.AccessModeConfirm
	surface.CodexPromptOverride = state.CodexPromptOverrideRecord{Model: "gpt-6-sol", ReasoningEffort: "high"}
	wantTopic := surface.CodexPromptOverride

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-luna",
		Text:             "检查这个问题",
		Inputs:           []agentproto.Input{{Type: agentproto.InputText, Text: "检查这个问题"}},
	})

	item := queueItemForSourceMessage(t, surface, "msg-luna")
	if item.FrozenOverride != (state.ModelConfigRecord{Model: "gpt-6-sol", ReasoningEffort: "high", AccessMode: agentproto.AccessModeConfirm}) {
		t.Fatalf("frozen override = %#v", item.FrozenOverride)
	}
	if surface.CodexPromptOverride != wantTopic {
		t.Fatalf("unprefixed message mutated topic override: %#v", surface.CodexPromptOverride)
	}
}

func TestCodexMessagePresetPersistsCurrentTopicForFollowingMessages(t *testing.T) {
	now := time.Date(2026, 8, 27, 5, 0, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	surface := svc.root.Surfaces["surface-1"]
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, MessageID: "msg-astra", Text: "[astra:xhigh]复杂任务", Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "[astra:xhigh]复杂任务"}}})
	if got := surface.CodexPromptOverride; got != (state.CodexPromptOverrideRecord{Model: "gpt-6-astra", ReasoningEffort: "xhigh"}) {
		t.Fatalf("topic override = %#v", got)
	}
	if got := queueItemForSourceMessage(t, surface, "msg-astra").FrozenOverride; got.Model != "gpt-6-astra" || got.ReasoningEffort != "xhigh" {
		t.Fatalf("prefixed model = %q", got)
	}
	delete(surface.QueueItems, surface.ActiveQueueItemID)
	surface.ActiveQueueItemID = ""
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, MessageID: "msg-follow", Text: "继续", Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "继续"}}})
	if got := queueItemForSourceMessage(t, surface, "msg-follow").FrozenOverride; got.Model != "gpt-6-astra" || got.ReasoningEffort != "xhigh" {
		t.Fatalf("following model = %q", got)
	}
}

func TestCodexMessagePresetCleansBodyAndMarksNormalAndDetourQueueItems(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		wantText   string
		wantPreset string
		wantModel  string
		detour     bool
	}{
		{name: "luna", text: "[luna] 检查", wantText: "检查", wantPreset: "luna", wantModel: "gpt-6-luna"},
		{name: "terra quoted reply", text: "[terra] 修复", wantText: "修复", wantPreset: "terra", wantModel: "gpt-5.6-terra"},
		{name: "sol", text: "[sol] 解释", wantText: "解释", wantPreset: "sol", wantModel: "gpt-6-sol"},
		{name: "astra", text: "[astra] 复杂推理", wantText: "复杂推理", wantPreset: "astra", wantModel: "gpt-6-astra"},
		{name: "sol detour", text: "[sol] [什么？] 临时解释", wantText: "临时解释", wantPreset: "sol", wantModel: "gpt-6-sol", detour: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 8, 26, 9, 10, 0, 0, time.UTC)
			svc := newReplyAutoSteerServiceFixture(&now)
			surface := svc.root.Surfaces["surface-1"]
			quote := "<被引用内容>\n原消息\n</被引用内容>"
			svc.ApplySurfaceAction(control.Action{
				Kind:             control.ActionTextMessage,
				SurfaceSessionID: surface.SurfaceSessionID,
				MessageID:        "msg-preset",
				Text:             tt.text,
				Inputs: []agentproto.Input{
					{Type: agentproto.InputText, Text: quote},
					{Type: agentproto.InputText, Text: tt.text},
				},
			})

			item := queueItemForSourceMessage(t, surface, "msg-preset")
			if item.CodexMessagePreset != tt.wantPreset || item.FrozenOverride.Model != tt.wantModel || item.FrozenOverride.ReasoningEffort != "high" {
				t.Fatalf("queue route = preset %q override %#v", item.CodexMessagePreset, item.FrozenOverride)
			}
			if item.SourceMessagePreview != tt.wantText {
				t.Fatalf("preview = %q, want %q", item.SourceMessagePreview, tt.wantText)
			}
			if len(item.Inputs) != 2 || item.Inputs[0].Text != quote || item.Inputs[1].Text != tt.wantText {
				t.Fatalf("inputs = %#v", item.Inputs)
			}
			if gotDetour := queuedItemPromptDispatchPlan(item).ExecutionMode == agentproto.PromptExecutionModeForkEphemeral; gotDetour != tt.detour {
				t.Fatalf("detour = %t, want %t", gotDetour, tt.detour)
			}
		})
	}
}

func TestCodexMessagePresetCleansFirstCurrentPostInputWithoutLosingOrder(t *testing.T) {
	quote := agentproto.Input{Type: agentproto.InputText, Text: "[sol] 引用内容不能改"}
	tests := []struct {
		name    string
		inputs  []agentproto.Input
		current []agentproto.Input
		want    []agentproto.Input
	}{
		{
			name: "multi segment post",
			inputs: []agentproto.Input{
				{Type: agentproto.InputText, Text: "[sol] 修复"},
				{Type: agentproto.InputText, Text: "补充"},
			},
			current: []agentproto.Input{
				{Type: agentproto.InputText, Text: "[sol] 修复"},
				{Type: agentproto.InputText, Text: "补充"},
			},
			want: []agentproto.Input{
				{Type: agentproto.InputText, Text: "修复"},
				{Type: agentproto.InputText, Text: "补充"},
			},
		},
		{
			name: "reply post keeps quote",
			inputs: []agentproto.Input{
				quote,
				{Type: agentproto.InputText, Text: "[sol] 修复"},
				{Type: agentproto.InputText, Text: "补充"},
			},
			current: []agentproto.Input{
				{Type: agentproto.InputText, Text: "[sol] 修复"},
				{Type: agentproto.InputText, Text: "补充"},
			},
			want: []agentproto.Input{
				quote,
				{Type: agentproto.InputText, Text: "修复"},
				{Type: agentproto.InputText, Text: "补充"},
			},
		},
		{
			name: "image post",
			inputs: []agentproto.Input{
				{Type: agentproto.InputText, Text: "[sol] 修复"},
				{Type: agentproto.InputLocalImage, Path: "/tmp/post.png", MIMEType: "image/png"},
				{Type: agentproto.InputText, Text: "补充"},
			},
			current: []agentproto.Input{
				{Type: agentproto.InputText, Text: "[sol] 修复"},
				{Type: agentproto.InputLocalImage, Path: "/tmp/post.png", MIMEType: "image/png"},
				{Type: agentproto.InputText, Text: "补充"},
			},
			want: []agentproto.Input{
				{Type: agentproto.InputText, Text: "修复"},
				{Type: agentproto.InputLocalImage, Path: "/tmp/post.png", MIMEType: "image/png"},
				{Type: agentproto.InputText, Text: "补充"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
			svc := newReplyAutoSteerServiceFixture(&now)
			surface := svc.root.Surfaces["surface-1"]
			svc.ApplySurfaceAction(control.Action{
				Kind:             control.ActionTextMessage,
				SurfaceSessionID: surface.SurfaceSessionID,
				MessageID:        "msg-post",
				Text:             "[sol] 修复\n\n补充",
				Inputs:           tt.inputs,
				SteerInputs:      tt.current,
			})
			item := queueItemForSourceMessage(t, surface, "msg-post")
			if item.CodexMessagePreset != "sol" || len(item.Inputs) != len(tt.want) {
				t.Fatalf("post route = %#v", item)
			}
			for i := range tt.want {
				if item.Inputs[i] != tt.want[i] {
					t.Fatalf("input[%d] = %#v, want %#v; all=%#v", i, item.Inputs[i], tt.want[i], item.Inputs)
				}
			}
		})
	}
}

func TestCodexMessagePresetFixedProfileAndCatalogValidation(t *testing.T) {
	t.Run("fixed profile keeps unprefixed old path and rejects explicit prefix", func(t *testing.T) {
		now := time.Date(2026, 8, 26, 9, 20, 0, 0, time.UTC)
		svc := newReplyAutoSteerServiceFixture(&now)
		svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
			{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Available: true},
			{ID: "fixed", Kind: state.CodexProfileKindAPI, Model: "provider-custom", ReasoningEffort: "high", Available: true},
		})
		surface := svc.root.Surfaces["surface-1"]
		surface.CodexProfileID = "fixed"
		surface.CodexPromptOverride = state.CodexPromptOverrideRecord{Model: "legacy-topic", ReasoningEffort: "medium"}

		svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, MessageID: "msg-old", Text: "普通消息"})
		if item := queueItemForSourceMessage(t, surface, "msg-old"); item.CodexMessagePreset != "" {
			t.Fatalf("fixed unprefixed preset = %q, want empty", item.CodexMessagePreset)
		}
		before := len(surface.QueueItems)
		events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, MessageID: "msg-reject", Text: "[sol] 普通消息"})
		if len(surface.QueueItems) != before || len(events) != 1 || events[0].Notice == nil || !strings.Contains(events[0].Notice.Text, "固定模型 provider-custom") {
			t.Fatalf("fixed explicit prefix should reject before enqueue: %#v", events)
		}
	})

	for _, tc := range []struct {
		name       string
		catalog    *agentproto.ModelCatalogSnapshot
		wantReject bool
	}{
		{name: "complete catalog missing rejects", catalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{Model: "gpt-6-luna"}}}, wantReject: true},
		{name: "paginated catalog missing stays unknown", catalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{Model: "gpt-6-luna"}}, NextCursor: "page-2"}},
		{name: "unsupported catalog stays unknown", catalog: &agentproto.ModelCatalogSnapshot{Unsupported: true}},
		{name: "failed catalog stays unknown", catalog: &agentproto.ModelCatalogSnapshot{ErrorMessage: "temporary failure"}},
		{name: "nil catalog stays unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 8, 26, 9, 30, 0, 0, time.UTC)
			svc := newReplyAutoSteerServiceFixture(&now)
			surface := svc.root.Surfaces["surface-1"]
			svc.root.Instances["inst-1"].ModelCatalog = tc.catalog
			events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, MessageID: "msg-catalog", Text: "[terra] 修复"})
			_, queued := findQueueItemForSourceMessage(surface, "msg-catalog")
			if queued == tc.wantReject {
				t.Fatalf("queued = %t, want %t; events=%#v", queued, !tc.wantReject, events)
			}
			if tc.wantReject && (len(events) != 1 || events[0].Notice == nil || !strings.Contains(events[0].Notice.Text, "gpt-5.6-terra")) {
				t.Fatalf("missing catalog rejection = %#v", events)
			}
		})
	}
}

func TestCodexMessagePresetRoutesOAuthAndDynamicGPTProfiles(t *testing.T) {
	for _, profile := range []state.CodexProfileSummary{
		{ID: state.OAuthCodexProfileID, Kind: state.CodexProfileKindOAuth, Available: true},
		{ID: "dynamic-gpt", Kind: state.CodexProfileKindAPI, Model: "gpt-6-luna", Available: true},
	} {
		t.Run(profile.ID, func(t *testing.T) {
			now := time.Date(2026, 8, 26, 9, 35, 0, 0, time.UTC)
			svc := newReplyAutoSteerServiceFixture(&now)
			svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
				{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Available: true},
				profile,
			})
			surface := svc.root.Surfaces["surface-1"]
			surface.CodexProfileID = profile.ID
			svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, MessageID: "msg-profile", Text: "默认档"})
			item := queueItemForSourceMessage(t, surface, "msg-profile")
			if item.CodexMessagePreset != "" || item.FrozenOverride.Model != "" || item.FrozenOverride.ReasoningEffort != "" {
				t.Fatalf("profile %q did not route through terra preset: %#v", profile.ID, item)
			}
		})
	}
}

func TestCodexMessagePresetDoesNotRouteDynamicNonGPTProfile(t *testing.T) {
	now := time.Date(2026, 8, 26, 9, 37, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Available: true},
		{
			ID:              "deepseek-profile",
			Kind:            state.CodexProfileKindAPI,
			BaseURL:         "https://api.deepseek.com/",
			Model:           "deepseek-v4-flash",
			ReasoningEffort: "high",
			Available:       true,
		},
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.CodexProfileID = "deepseek-profile"

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-deepseek",
		Text:             "[terra] 保持 DeepSeek",
		Inputs:           []agentproto.Input{{Type: agentproto.InputText, Text: "[terra] 保持 DeepSeek"}},
	})

	item := queueItemForSourceMessage(t, surface, "msg-deepseek")
	if item.CodexMessagePreset != "" || strings.HasPrefix(item.FrozenOverride.Model, "gpt-") {
		t.Fatalf("non-GPT profile received GPT preset: %#v", item)
	}
	if item.SourceMessagePreview != "[terra] 保持 DeepSeek" || len(item.Inputs) != 1 || item.Inputs[0].Text != "[terra] 保持 DeepSeek" {
		t.Fatalf("non-GPT profile should keep ordinary message unchanged: %#v", item)
	}
}

func TestCodexMessagePresetUnboundReplayPreservesExplicitPreset(t *testing.T) {
	now := time.Date(2026, 8, 26, 9, 38, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	surface := svc.root.Surfaces["surface-1"]
	surface.RouteMode = state.RouteModeUnbound
	surface.SelectedThreadID = ""
	surface.ClaimedWorkspaceKey = ""
	surface.PreparedThreadCWD = ""
	inst := svc.root.Instances["inst-1"]
	inst.WorkspaceRoot = ""
	inst.WorkspaceKey = ""
	originalText := "[terra] 重放仍用 Terra"
	originalInputs := []agentproto.Input{
		{Type: agentproto.InputText, Text: "<被引用内容>\n原消息\n</被引用内容>"},
		{Type: agentproto.InputText, Text: originalText},
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-replay",
		Text:             originalText,
		Inputs:           originalInputs,
	})
	if surface.PendingTextInput == nil || surface.PendingTextInput.Text != originalText || len(surface.PendingTextInput.Inputs) != 2 || surface.PendingTextInput.Inputs[1].Text != originalText {
		t.Fatalf("blocked input lost original preset before replay: %#v", surface.PendingTextInput)
	}

	pending := svc.takePendingTextInput(surface)
	inst.WorkspaceRoot = "/data/dl/droid"
	inst.WorkspaceKey = "/data/dl/droid"
	surface.RouteMode = state.RouteModePinned
	surface.SelectedThreadID = "thread-1"
	svc.replayPendingTextInput(surface, pending)
	item := queueItemForSourceMessage(t, surface, "msg-replay")
	if item.CodexMessagePreset != "terra" || item.FrozenOverride.Model != "gpt-5.6-terra" || item.FrozenOverride.ReasoningEffort != "high" {
		t.Fatalf("replayed item did not preserve terra preset: %#v", item)
	}
	if len(item.Inputs) != 2 || item.Inputs[0].Text != originalInputs[0].Text || item.Inputs[1].Text != "重放仍用 Terra" {
		t.Fatalf("replayed inputs were not cleaned once: %#v", item.Inputs)
	}
}

func TestCodexMessagePresetCatalogReasoningSupport(t *testing.T) {
	for _, tc := range []struct {
		name       string
		efforts    []agentproto.ReasoningEffortOption
		wantReject bool
	}{
		{
			name: "explicitly unsupported high rejects",
			efforts: []agentproto.ReasoningEffortOption{
				{ReasoningEffort: "low"},
				{ReasoningEffort: "medium"},
			},
			wantReject: true,
		},
		{name: "empty efforts stays unknown"},
		{name: "high supported", efforts: []agentproto.ReasoningEffortOption{{ReasoningEffort: "high"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 8, 26, 9, 39, 0, 0, time.UTC)
			svc := newReplyAutoSteerServiceFixture(&now)
			surface := svc.root.Surfaces["surface-1"]
			svc.root.Instances["inst-1"].ModelCatalog = &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
				Model:                     "gpt-5.6-terra",
				SupportedReasoningEfforts: tc.efforts,
			}}}
			events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, MessageID: "msg-effort", Text: "[terra] 修复"})
			_, queued := findQueueItemForSourceMessage(surface, "msg-effort")
			if queued == tc.wantReject {
				t.Fatalf("queued = %t, want %t; events=%#v", queued, !tc.wantReject, events)
			}
			if tc.wantReject && (len(events) != 1 || events[0].Notice == nil || !strings.Contains(events[0].Notice.Text, "high")) {
				t.Fatalf("unsupported high rejection = %#v", events)
			}
		})
	}
}

func TestCodexMessagePresetParserErrorReturnsNoticeWithoutEnqueue(t *testing.T) {
	now := time.Date(2026, 8, 26, 9, 40, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	surface := svc.root.Surfaces["surface-1"]
	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, MessageID: "msg-empty", Text: "[sol]"})
	if len(surface.QueueItems) != 0 || len(events) != 1 || events[0].Notice == nil || !strings.Contains(events[0].Notice.Text, "不能为空") ||
		!strings.Contains(events[0].Notice.Text, "[luna]") || !strings.Contains(events[0].Notice.Text, "[terra]") || !strings.Contains(events[0].Notice.Text, "[sol]") || !strings.Contains(events[0].Notice.Text, "[astra]") {
		t.Fatalf("parser error result = items %#v events %#v", surface.QueueItems, events)
	}
}

func TestCodexMessagePresetDispatchRejectsFrozenOverrideDrift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		preset string
		text   string
		drift  func(*Service, *state.SurfaceConsoleRecord)
	}{
		{
			name:   "catalog becomes complete without high",
			preset: "terra",
			text:   "[terra] 后续任务",
			drift: func(svc *Service, _ *state.SurfaceConsoleRecord) {
				svc.root.Instances["inst-1"].ModelCatalog = &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
					Model: "gpt-5.6-terra",
					SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
						{ReasoningEffort: "low"},
					},
				}}}
			},
		},
		{
			name:   "profile becomes fixed",
			preset: "sol",
			text:   "[sol] 后续任务",
			drift: func(svc *Service, surface *state.SurfaceConsoleRecord) {
				svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
					{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Available: true},
					{ID: "fixed", Kind: state.CodexProfileKindAPI, Model: "provider-custom", ReasoningEffort: "high", Available: true},
				})
				surface.CodexProfileID = "fixed"
			},
		},
		{
			name:   "backend becomes non Codex",
			preset: "terra",
			text:   "[terra] 后续任务",
			drift: func(svc *Service, surface *state.SurfaceConsoleRecord) {
				svc.root.Instances["inst-1"].Backend = agentproto.BackendClaude
				surface.Backend = agentproto.BackendClaude
			},
		},
		{
			name:   "profile becomes dynamic non GPT",
			preset: "sol",
			text:   "[sol] 后续任务",
			drift: func(svc *Service, surface *state.SurfaceConsoleRecord) {
				svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
					{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Available: true},
					{ID: "deepseek", Kind: state.CodexProfileKindAPI, BaseURL: "https://api.deepseek.com/", Model: "deepseek-v4-flash", Available: true},
				})
				surface.CodexProfileID = "deepseek"
			},
		},
		{
			name:   "complete catalog loses preset model",
			preset: "terra",
			text:   "[terra] 后续任务",
			drift: func(svc *Service, _ *state.SurfaceConsoleRecord) {
				svc.root.Instances["inst-1"].ModelCatalog = &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
					Model: "gpt-6-luna",
					SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
						{ReasoningEffort: "high"},
					},
				}}}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 8, 26, 10, 10, 0, 0, time.UTC)
			svc := newReplyAutoSteerServiceFixture(&now)
			surface := svc.root.Surfaces["surface-1"]
			startReplyAutoSteerTurn(svc)
			svc.ApplySurfaceAction(control.Action{
				Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID,
				MessageID: "msg-drift", Text: tc.text,
			})
			item := queueItemForSourceMessage(t, surface, "msg-drift")
			if item.Status != state.QueueItemQueued || item.CodexMessagePreset != tc.preset {
				t.Fatalf("preset item did not wait before drift: %#v", item)
			}
			tc.drift(svc, surface)

			events := completeRemoteTurnWithFinalText(t, svc, "turn-1", "completed", "", "", nil)
			if item.Status != state.QueueItemFailed {
				t.Fatalf("drifted preset item status = %q, want failed; events=%#v", item.Status, events)
			}
			if findQueuedStartNotice(events) != nil || findPromptSendCommand(events) != nil {
				t.Fatalf("drifted preset emitted started notice or PromptSend: %#v", events)
			}
			foundFailure := false
			for _, event := range events {
				if event.Notice != nil && event.Notice.Code == "codex_message_preset_dispatch_rejected" {
					foundFailure = true
				}
			}
			if !foundFailure {
				t.Fatalf("drifted preset missing explicit failure notice: %#v", events)
			}
		})
	}
}

func TestCodexMessagePresetRejectedQueueHeadContinuesDispatch(t *testing.T) {
	t.Run("next valid preset dispatches", func(t *testing.T) {
		now := time.Date(2026, 8, 26, 10, 20, 0, 0, time.UTC)
		svc := newReplyAutoSteerServiceFixture(&now)
		surface := svc.root.Surfaces["surface-1"]
		startReplyAutoSteerTurn(svc)
		svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, MessageID: "msg-rejected", Text: "[terra] 无效队头"})
		svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, MessageID: "msg-next", Text: "[luna] 合法 Luna"})
		rejected := queueItemForSourceMessage(t, surface, "msg-rejected")
		next := queueItemForSourceMessage(t, surface, "msg-next")
		svc.root.Instances["inst-1"].ModelCatalog = &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
			Model: "gpt-6-luna",
			SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
				{ReasoningEffort: "high"},
			},
		}}}

		events := completeRemoteTurnWithFinalText(t, svc, "turn-1", "completed", "", "", nil)
		if rejected.Status != state.QueueItemFailed || next.Status != state.QueueItemDispatching {
			t.Fatalf("queue did not advance after rejected head: rejected=%#v next=%#v events=%#v", rejected, next, events)
		}
		command := findPromptSendCommand(events)
		if command == nil || command.Origin.MessageID != "msg-next" || command.Overrides.Model != "gpt-6-luna" {
			t.Fatalf("next valid preset did not dispatch immediately: %#v", events)
		}
	})

	t.Run("next non preset dispatches", func(t *testing.T) {
		now := time.Date(2026, 8, 26, 10, 25, 0, 0, time.UTC)
		svc := newReplyAutoSteerServiceFixture(&now)
		surface := svc.root.Surfaces["surface-1"]
		startReplyAutoSteerTurn(svc)
		svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, MessageID: "msg-rejected", Text: "[sol] 无效队头"})
		svc.enqueueQueueItem(
			surface,
			"msg-next",
			"普通旧队列项",
			nil,
			[]agentproto.Input{{Type: agentproto.InputText, Text: "普通旧队列项"}},
			"thread-1",
			"/data/dl/droid",
			state.RouteModePinned,
			state.ModelConfigRecord{},
			false,
		)
		rejected := queueItemForSourceMessage(t, surface, "msg-rejected")
		next := queueItemForSourceMessage(t, surface, "msg-next")
		svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
			{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Available: true},
			{ID: "fixed", Kind: state.CodexProfileKindAPI, Model: "provider-custom", ReasoningEffort: "high", Available: true},
		})
		surface.CodexProfileID = "fixed"

		events := completeRemoteTurnWithFinalText(t, svc, "turn-1", "completed", "", "", nil)
		if rejected.Status != state.QueueItemFailed || next.Status != state.QueueItemDispatching {
			t.Fatalf("queue did not advance to non-preset item: rejected=%#v next=%#v events=%#v", rejected, next, events)
		}
		command := findPromptSendCommand(events)
		if command == nil || command.Origin.MessageID != "msg-next" {
			t.Fatalf("next non-preset item did not dispatch immediately: %#v", events)
		}
	})
}

func TestCodexMessagePresetManyRejectedQueueHeadsDrainIteratively(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 30, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	surface := svc.root.Surfaces["surface-1"]
	startReplyAutoSteerTurn(svc)
	const rejectedCount = 128
	for i := 0; i < rejectedCount; i++ {
		messageID := fmt.Sprintf("msg-rejected-%03d", i)
		svc.ApplySurfaceAction(control.Action{
			Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID,
			MessageID: messageID, Text: "[terra] 无效队头",
		})
	}
	svc.ApplySurfaceAction(control.Action{
		Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID,
		MessageID: "msg-valid", Text: "[luna] 合法 Luna",
	})
	svc.root.Instances["inst-1"].ModelCatalog = &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
		Model: "gpt-6-luna",
		SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
			{ReasoningEffort: "high"},
		},
	}}}

	events := completeRemoteTurnWithFinalText(t, svc, "turn-1", "completed", "", "", nil)
	queueOff := map[string]bool{}
	rejectedNotices := map[string]bool{}
	startedRejected := false
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.QueueOff && event.PendingInput.Status == string(state.QueueItemFailed) {
			queueOff[event.PendingInput.QueueItemID] = true
		}
		if event.Notice != nil && event.Notice.Code == "codex_message_preset_dispatch_rejected" {
			rejectedNotices[event.SourceMessageID] = true
		}
		if event.TimelineText != nil && event.TimelineText.Type == control.TimelineTextQueuedMessageStarted && strings.HasPrefix(event.SourceMessageID, "msg-rejected-") {
			startedRejected = true
		}
	}
	for i := 0; i < rejectedCount; i++ {
		messageID := fmt.Sprintf("msg-rejected-%03d", i)
		item := queueItemForSourceMessage(t, surface, messageID)
		if item.Status != state.QueueItemFailed || !queueOff[item.ID] || !rejectedNotices[messageID] {
			t.Fatalf("rejected item %s not fully failed: item=%#v queueOff=%t notice=%t", messageID, item, queueOff[item.ID], rejectedNotices[messageID])
		}
	}
	valid := queueItemForSourceMessage(t, surface, "msg-valid")
	command := findPromptSendCommand(events)
	if valid.Status != state.QueueItemDispatching || command == nil || command.Origin.MessageID != "msg-valid" {
		t.Fatalf("valid tail did not dispatch after draining rejected heads: valid=%#v events=%#v", valid, events)
	}
	if startedRejected {
		t.Fatalf("rejected queue head emitted started notice: %#v", events)
	}
	if len(rejectedNotices) != rejectedCount || len(queueOff) != rejectedCount {
		t.Fatalf("rejected event counts: notices=%d queueOff=%d want=%d", len(rejectedNotices), len(queueOff), rejectedCount)
	}

	source, err := os.ReadFile("service_queue.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), "return append(events, s.dispatchNext(surface)...)") {
		t.Fatal("preset rejection must drain iteratively instead of recursively calling dispatchNext")
	}
}

func queueItemForSourceMessage(t *testing.T, surface *state.SurfaceConsoleRecord, sourceMessageID string) *state.QueueItemRecord {
	t.Helper()
	item, ok := findQueueItemForSourceMessage(surface, sourceMessageID)
	if !ok {
		t.Fatalf("queue item for %q not found: %#v", sourceMessageID, surface.QueueItems)
	}
	return item
}

func findQueueItemForSourceMessage(surface *state.SurfaceConsoleRecord, sourceMessageID string) (*state.QueueItemRecord, bool) {
	for _, item := range surface.QueueItems {
		if item != nil && item.SourceMessageID == sourceMessageID {
			return item, true
		}
	}
	return nil, false
}
