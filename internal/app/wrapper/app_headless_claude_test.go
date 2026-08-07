package wrapper

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestClaudeBootstrapInitializeFrameCarriesAppendSystemPrompt(t *testing.T) {
	t.Setenv(config.ClaudeAppendSystemPromptEnv, "你是一个乐于助人的助手。")

	raw, err := (&App{}).claudeBootstrapInitializeFrame()
	if err != nil {
		t.Fatalf("claudeBootstrapInitializeFrame: %v", err)
	}
	var frame struct {
		Type    string `json:"type"`
		Request struct {
			Subtype            string `json:"subtype"`
			AppendSystemPrompt string `json:"appendSystemPrompt"`
		} `json:"request"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("decode initialize frame: %v", err)
	}
	if frame.Type != "control_request" || frame.Request.Subtype != "initialize" {
		t.Fatalf("unexpected initialize frame: %#v", frame)
	}
	if frame.Request.AppendSystemPrompt != "你是一个乐于助人的助手。" {
		t.Fatalf("appendSystemPrompt = %q, want role prompt", frame.Request.AppendSystemPrompt)
	}
}

func TestClaudeBootstrapInitializeFrameOmitsAppendSystemPromptWhenEmpty(t *testing.T) {
	t.Setenv(config.ClaudeAppendSystemPromptEnv, "")

	raw, err := (&App{}).claudeBootstrapInitializeFrame()
	if err != nil {
		t.Fatalf("claudeBootstrapInitializeFrame: %v", err)
	}
	var frame map[string]any
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("decode initialize frame: %v", err)
	}
	request, ok := frame["request"].(map[string]any)
	if !ok {
		t.Fatalf("initialize frame missing request: %#v", frame)
	}
	if _, present := request["appendSystemPrompt"]; present {
		t.Fatalf("expected no appendSystemPrompt for empty instruction: %#v", request)
	}
}
