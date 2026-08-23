package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestHandleUIEventsConvertsLocalImagesToVisionAssistPathPromptForOpenCode(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.VisionAssist = config.VisionAssistSettings{Protocol: "openai_chat", BaseURL: "http://vision.example"}
	cfg.OpenCode.Profiles = []config.OpenCodeAPIProfileRecord{{
		ID:              "op_text",
		CurrentRevision: 1,
		Revisions: []config.OpenCodeAPIProfileSecretConfig{{
			ID: "op_text", Revision: 1, CredentialGeneration: 1, ConnectionGeneration: 1,
			Name: "Text OpenCode", ProviderType: config.OpenCodeProviderTypeGoogleGemini,
			APIKey: "k", Model: "gemini-text",
		}},
	}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	var sent []agentproto.Command
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-op" {
			t.Fatalf("unexpected instance id %q", instanceID)
		}
		sent = append(sent, command)
		return nil
	}
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.Surface("surface-1").AttachedInstanceID = "inst-op"
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-op",
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_text",
		Source:            "headless",
		Managed:           true,
		Online:            true,
		Threads:           map[string]*state.ThreadRecord{},
	})

	app.handleUIEvents(context.Background(), []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: "surface-1",
		Command: &agentproto.Command{
			Kind: agentproto.CommandPromptSend,
			Prompt: agentproto.Prompt{Inputs: []agentproto.Input{
				{Type: agentproto.InputLocalImage, Path: "/tmp/shot.png", MIMEType: "image/png"},
				{Type: agentproto.InputText, Text: "请读这张图"},
			}},
		},
	}})

	if len(sent) != 1 {
		t.Fatalf("expected one command, got %#v", sent)
	}
	inputs := sent[0].Prompt.Inputs
	if len(inputs) != 2 {
		t.Fatalf("expected image path prompt + user text, got %#v", inputs)
	}
	if inputs[0].Type != agentproto.InputText || !strings.Contains(inputs[0].Text, "describe_image") ||
		!strings.Contains(inputs[0].Text, "img1") || !strings.Contains(inputs[0].Text, "/tmp/shot.png") ||
		!strings.Contains(inputs[0].Text, "image/png") {
		t.Fatalf("unexpected vision assist path prompt: %#v", inputs[0])
	}
	if inputs[1].Type != agentproto.InputText || inputs[1].Text != "请读这张图" {
		t.Fatalf("expected original user text after path prompt, got %#v", inputs[1])
	}
	for _, input := range inputs {
		if input.Type == agentproto.InputLocalImage {
			t.Fatalf("local image should not be sent to non-vision opencode profile with vision assist: %#v", inputs)
		}
	}
}

func TestHandleUIEventsConvertsLocalImagesToVisionAssistPathPromptForClaude(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.VisionAssist = config.VisionAssistSettings{Protocol: "openai_chat", BaseURL: "http://vision.example"}
	cfg.Claude.Profiles = []config.ClaudeProfileConfig{{
		ID: "claude_text", Name: "Text Claude", Model: "claude-text",
	}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	var sent []agentproto.Command
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-claude" {
			t.Fatalf("unexpected instance id %q", instanceID)
		}
		sent = append(sent, command)
		return nil
	}
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.Surface("surface-1").AttachedInstanceID = "inst-claude"
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-claude",
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "claude_text",
		Source:          "headless",
		Managed:         true,
		Online:          true,
		Threads:         map[string]*state.ThreadRecord{},
	})

	app.handleUIEvents(context.Background(), []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: "surface-1",
		Command: &agentproto.Command{
			Kind: agentproto.CommandPromptSend,
			Prompt: agentproto.Prompt{Inputs: []agentproto.Input{
				{Type: agentproto.InputLocalImage, Path: "/tmp/claude-shot.jpg", MIMEType: "image/jpeg"},
				{Type: agentproto.InputText, Text: "请读这张图"},
			}},
		},
	}})

	if len(sent) != 1 {
		t.Fatalf("expected one command, got %#v", sent)
	}
	inputs := sent[0].Prompt.Inputs
	if len(inputs) != 2 {
		t.Fatalf("expected image path prompt + user text, got %#v", inputs)
	}
	if inputs[0].Type != agentproto.InputText || !strings.Contains(inputs[0].Text, "describe_image") ||
		!strings.Contains(inputs[0].Text, "img1") || !strings.Contains(inputs[0].Text, "/tmp/claude-shot.jpg") ||
		!strings.Contains(inputs[0].Text, "image/jpeg") {
		t.Fatalf("unexpected vision assist path prompt: %#v", inputs[0])
	}
	for _, input := range inputs {
		if input.Type == agentproto.InputLocalImage {
			t.Fatalf("local image should not be sent to non-vision claude profile with vision assist: %#v", inputs)
		}
	}
}

func TestHandleUIEventsKeepsLocalImagesForVisionProfile(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.VisionAssist = config.VisionAssistSettings{Protocol: "openai_chat", BaseURL: "http://vision.example"}
	cfg.OpenCode.Profiles = []config.OpenCodeAPIProfileRecord{{
		ID:              "op_vision",
		CurrentRevision: 1,
		Revisions: []config.OpenCodeAPIProfileSecretConfig{{
			ID: "op_vision", Revision: 1, CredentialGeneration: 1, ConnectionGeneration: 1,
			Name: "Vision OpenCode", ProviderType: config.OpenCodeProviderTypeGoogleGemini,
			APIKey: "k", Model: "gemini-vision", VisionSupported: true,
		}},
	}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	var sent []agentproto.Command
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-op" {
			t.Fatalf("unexpected instance id %q", instanceID)
		}
		sent = append(sent, command)
		return nil
	}
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.Surface("surface-1").AttachedInstanceID = "inst-op"
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-op",
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_vision",
		Source:            "headless",
		Managed:           true,
		Online:            true,
		Threads:           map[string]*state.ThreadRecord{},
	})

	app.handleUIEvents(context.Background(), []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: "surface-1",
		Command: &agentproto.Command{
			Kind: agentproto.CommandPromptSend,
			Prompt: agentproto.Prompt{Inputs: []agentproto.Input{
				{Type: agentproto.InputLocalImage, Path: "/tmp/shot.png", MIMEType: "image/png"},
				{Type: agentproto.InputText, Text: "请读这张图"},
			}},
		},
	}})

	if len(sent) != 1 {
		t.Fatalf("expected one command, got %#v", sent)
	}
	inputs := sent[0].Prompt.Inputs
	if len(inputs) != 2 || inputs[0].Type != agentproto.InputLocalImage || inputs[0].Path != "/tmp/shot.png" || inputs[1].Type != agentproto.InputText {
		t.Fatalf("expected original local image inputs for vision profile, got %#v", inputs)
	}
}

func TestHandleUIEventsKeepsLocalImagesForCodexOAuthProfile(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.VisionAssist = config.VisionAssistSettings{Protocol: "openai_chat", BaseURL: "http://vision.example"}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	var sent []agentproto.Command
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-codex-oauth" {
			t.Fatalf("unexpected instance id %q", instanceID)
		}
		sent = append(sent, command)
		return nil
	}
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.Surface("surface-1").AttachedInstanceID = "inst-codex-oauth"
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:     "inst-codex-oauth",
		Backend:        agentproto.BackendCodex,
		CodexProfileID: state.OAuthCodexProfileID,
		Source:         "headless",
		Managed:        true,
		Online:         true,
		Threads:        map[string]*state.ThreadRecord{},
	})

	app.handleUIEvents(context.Background(), []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: "surface-1",
		Command: &agentproto.Command{
			Kind: agentproto.CommandPromptSend,
			Prompt: agentproto.Prompt{Inputs: []agentproto.Input{
				{Type: agentproto.InputLocalImage, Path: "/tmp/chatgpt-shot.png", MIMEType: "image/png"},
				{Type: agentproto.InputText, Text: "请读这张图"},
			}},
		},
	}})

	if len(sent) != 1 {
		t.Fatalf("expected one command, got %#v", sent)
	}
	inputs := sent[0].Prompt.Inputs
	if len(inputs) != 2 || inputs[0].Type != agentproto.InputLocalImage || inputs[0].Path != "/tmp/chatgpt-shot.png" || inputs[1].Type != agentproto.InputText {
		t.Fatalf("expected original local image inputs for codex oauth profile, got %#v", inputs)
	}
}
