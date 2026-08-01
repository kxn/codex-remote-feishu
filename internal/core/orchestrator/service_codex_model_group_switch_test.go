package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCodexCrossModelGroupSwitchStartsNewThreadWithNotice(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(codexModelGroupSwitchInstance("deepseek-v4-flash"))
	surface := codexModelGroupSwitchSurface(t, svc)
	surface.CodexThreadPolicy = &state.CodexThreadPolicy{
		ThreadPolicyID:  "policy-gpt",
		ModelMode:       state.CodexThreadValueExplicit,
		Model:           "gpt-5.5",
		ReviewModelMode: state.CodexReviewModelConfig,
		ReasoningMode:   state.CodexThreadValueDefault,
		ContextMode:     state.CodexContextModeDefault,
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		ChatID:           surface.ChatID,
		ActorUserID:      surface.ActorUserID,
		MessageID:        "msg-1",
		Text:             "继续",
	})

	command := firstCodexCommand(events, agentproto.CommandPromptSend)
	if command == nil {
		t.Fatalf("expected prompt command, got %#v", events)
	}
	if command.Target.ExecutionMode != agentproto.PromptExecutionModeStartNew ||
		command.Target.ThreadID != "" ||
		!command.Target.CreateThreadIfMissing ||
		command.Target.CWD != "/data/dl/repo" {
		t.Fatalf("expected cross-group switch to start a new thread in the same cwd, got %#v", command.Target)
	}
	if command.CodexResume == nil || command.CodexResume.Model != "gpt-5.5" {
		t.Fatalf("expected codex resume policy to keep target model, got %#v", command.CodexResume)
	}
	if got := command.Overrides.Model; got != "" {
		t.Fatalf("expected prompt override to avoid carrying the old thread model, got %q", got)
	}
	if surface.RouteMode != state.RouteModeNewThreadReady ||
		surface.SelectedThreadID != "" ||
		surface.PreparedThreadCWD != "/data/dl/repo" ||
		surface.PreparedFromThreadID != "thread-1" {
		t.Fatalf("expected surface to enter new-thread-ready for the same workspace, got %#v", surface)
	}
	if notice := noticeByCode(events, "codex_model_group_new_thread"); notice == nil {
		t.Fatalf("expected cross-model-group notice, got %#v", events)
	} else if strings.Contains(notice.Text, "reasoning_text") || strings.Contains(notice.Text, "protocol") {
		t.Fatalf("notice should not expose protocol internals: %#v", notice)
	}
}

func TestCodexSameModelGroupSwitchKeepsThread(t *testing.T) {
	tests := []struct {
		name        string
		threadModel string
		targetModel string
	}{
		{name: "gpt to gpt", threadModel: "gpt-5.4", targetModel: "gpt-5.5"},
		{name: "non gpt to non gpt", threadModel: "deepseek-v4-flash", targetModel: "qwen-coder"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 8, 2, 10, 5, 0, 0, time.UTC)
			svc := newServiceForTest(&now)
			svc.UpsertInstance(codexModelGroupSwitchInstance(tt.threadModel))
			surface := codexModelGroupSwitchSurface(t, svc)
			surface.CodexThreadPolicy = &state.CodexThreadPolicy{
				ThreadPolicyID:  "policy-target",
				ModelMode:       state.CodexThreadValueExplicit,
				Model:           tt.targetModel,
				ReviewModelMode: state.CodexReviewModelConfig,
				ReasoningMode:   state.CodexThreadValueDefault,
				ContextMode:     state.CodexContextModeDefault,
			}

			events := svc.ApplySurfaceAction(control.Action{
				Kind:             control.ActionTextMessage,
				SurfaceSessionID: surface.SurfaceSessionID,
				ChatID:           surface.ChatID,
				ActorUserID:      surface.ActorUserID,
				MessageID:        "msg-1",
				Text:             "继续",
			})

			command := firstCodexCommand(events, agentproto.CommandPromptSend)
			if command == nil {
				t.Fatalf("expected prompt command, got %#v", events)
			}
			if command.Target.ExecutionMode != agentproto.PromptExecutionModeResumeExisting ||
				command.Target.ThreadID != "thread-1" ||
				command.Target.CreateThreadIfMissing {
				t.Fatalf("expected same-group switch to keep existing thread, got %#v", command.Target)
			}
			if notice := noticeByCode(events, "codex_model_group_new_thread"); notice != nil {
				t.Fatalf("expected no cross-model-group notice, got %#v", events)
			}
		})
	}
}

func TestCodexDefaultTargetPolicyStartsNewThreadAcrossModelGroup(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(codexModelGroupSwitchInstance("deepseek-v4-flash"))
	surface := codexModelGroupSwitchSurface(t, svc)
	surface.CodexThreadPolicy = &state.CodexThreadPolicy{
		ThreadPolicyID:  "policy-default",
		ModelMode:       state.CodexThreadValueDefault,
		ReviewModelMode: state.CodexReviewModelConfig,
		ReasoningMode:   state.CodexThreadValueDefault,
		ContextMode:     state.CodexContextModeDefault,
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		ChatID:           surface.ChatID,
		ActorUserID:      surface.ActorUserID,
		MessageID:        "msg-1",
		Text:             "继续",
	})

	command := firstCodexCommand(events, agentproto.CommandPromptSend)
	if command == nil {
		t.Fatalf("expected prompt command, got %#v", events)
	}
	if command.Target.ExecutionMode != agentproto.PromptExecutionModeStartNew ||
		command.Target.ThreadID != "" ||
		!command.Target.CreateThreadIfMissing {
		t.Fatalf("expected codex default target policy to start a new thread, got %#v", command.Target)
	}
	if command.CodexResume == nil || command.CodexResume.ModelMode != state.CodexThreadValueDefault {
		t.Fatalf("expected codex default policy to remain default-owned, got %#v", command.CodexResume)
	}
	if got := command.Overrides.Model; got != "" {
		t.Fatalf("expected default target policy to drop old thread model override, got %q", got)
	}
}

func TestCodexExplicitTargetPolicyWinsOverStaleOverrideForModelGroupSwitch(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 12, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(codexModelGroupSwitchInstance("gpt-5.5"))
	surface := codexModelGroupSwitchSurface(t, svc)
	surface.PromptOverride = state.ModelConfigRecord{Model: "gpt-5.5"}
	surface.CodexThreadPolicy = &state.CodexThreadPolicy{
		ThreadPolicyID:  "policy-deepseek",
		ModelMode:       state.CodexThreadValueExplicit,
		Model:           "deepseek-v4-flash",
		ReviewModelMode: state.CodexReviewModelConfig,
		ReasoningMode:   state.CodexThreadValueDefault,
		ContextMode:     state.CodexContextModeDefault,
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		ChatID:           surface.ChatID,
		ActorUserID:      surface.ActorUserID,
		MessageID:        "msg-1",
		Text:             "继续",
	})

	command := firstCodexCommand(events, agentproto.CommandPromptSend)
	if command == nil {
		t.Fatalf("expected prompt command, got %#v", events)
	}
	if command.Target.ExecutionMode != agentproto.PromptExecutionModeStartNew ||
		command.Target.ThreadID != "" ||
		!command.Target.CreateThreadIfMissing {
		t.Fatalf("expected explicit target policy to start a new thread, got %#v", command.Target)
	}
	if command.CodexResume == nil || command.CodexResume.Model != "deepseek-v4-flash" {
		t.Fatalf("expected explicit target policy model to win, got %#v", command.CodexResume)
	}
	if got := command.Overrides.Model; got != "" {
		t.Fatalf("expected stale model override to be dropped, got %q", got)
	}
}

func TestCodexUnknownThreadModelKeepsThread(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	inst := codexModelGroupSwitchInstance("")
	inst.Threads["thread-1"].CodexEffectiveThread = nil
	svc.UpsertInstance(inst)
	surface := codexModelGroupSwitchSurface(t, svc)
	surface.CodexThreadPolicy = &state.CodexThreadPolicy{
		ThreadPolicyID:  "policy-gpt",
		ModelMode:       state.CodexThreadValueExplicit,
		Model:           "gpt-5.5",
		ReviewModelMode: state.CodexReviewModelConfig,
		ReasoningMode:   state.CodexThreadValueDefault,
		ContextMode:     state.CodexContextModeDefault,
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		ChatID:           surface.ChatID,
		ActorUserID:      surface.ActorUserID,
		MessageID:        "msg-1",
		Text:             "继续",
	})

	command := firstCodexCommand(events, agentproto.CommandPromptSend)
	if command == nil {
		t.Fatalf("expected prompt command, got %#v", events)
	}
	if command.Target.ExecutionMode != agentproto.PromptExecutionModeResumeExisting ||
		command.Target.ThreadID != "thread-1" {
		t.Fatalf("expected unknown source model to keep existing thread, got %#v", command.Target)
	}
	if notice := noticeByCode(events, "codex_model_group_new_thread"); notice != nil {
		t.Fatalf("expected no cross-model-group notice for unknown source model, got %#v", events)
	}
}

func codexModelGroupSwitchInstance(threadModel string) *state.InstanceRecord {
	return &state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "repo",
		WorkspaceRoot:           "/data/dl/repo",
		WorkspaceKey:            "/data/dl/repo",
		ShortName:               "repo",
		Backend:                 agentproto.BackendCodex,
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		CodexConnectionContract: &state.CodexConnectionContract{
			ConnectionContractID: "conn-target",
			ModelProviderID:      "codex_remote_profile_target",
		},
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID:      "thread-1",
				Name:          "现有会话",
				CWD:           "/data/dl/repo",
				Loaded:        true,
				ExplicitModel: threadModel,
				CodexEffectiveThread: &agentproto.CodexEffectiveThreadContract{
					ConnectionContractID: "conn-source",
					ModelProviderID:      "codex_remote_profile_source",
					Model:                threadModel,
				},
			},
		},
	}
}

func codexModelGroupSwitchSurface(t *testing.T, svc *Service) *state.SurfaceConsoleRecord {
	t.Helper()
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	if notice := noticeByCode(events, "attached"); notice == nil {
		t.Fatalf("test setup failed: attach did not emit attached notice: %#v", events)
	}
	surface := svc.root.Surfaces["surface-1"]
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendCodex
	surface.CodexConnectionContract = &state.CodexConnectionContract{
		ConnectionContractID: "conn-target",
		ModelProviderID:      "codex_remote_profile_target",
	}
	return surface
}

func noticeByCode(events []eventcontract.Event, code string) *control.Notice {
	for i := range events {
		if events[i].Notice != nil && events[i].Notice.Code == code {
			return events[i].Notice
		}
	}
	return nil
}
