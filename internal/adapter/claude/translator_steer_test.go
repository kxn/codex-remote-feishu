package claude

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeTranslatorTurnSteerAcceptsImageOnlyWithinActiveTurn(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "default")
	imagePath := filepath.Join(t.TempDir(), "steer-only.png")
	if err := os.WriteFile(imagePath, []byte("steer-image"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	payloads, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-steer-image-only",
		Kind:      agentproto.CommandTurnSteer,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target:    agentproto.Target{ThreadID: threadID, TurnID: turnID},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{
			{Type: agentproto.InputLocalImage, Path: imagePath, MIMEType: "image/png"},
		}},
	})
	if err != nil {
		t.Fatalf("translate turn steer: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one steer payload, got %#v", payloads)
	}
	payload := decodeFrame(t, payloads[0])
	message := testMapValue(payload["message"])
	content := testSliceMapValue(message["content"])
	if len(content) != 1 || content[0]["type"] != "image" {
		t.Fatalf("expected one image block in steer payload, got %#v", message["content"])
	}
	source := testMapValue(content[0]["source"])
	if source["type"] != "base64" || lookupStringFromAny(source["media_type"]) != "image/png" || lookupStringFromAny(source["data"]) != base64.StdEncoding.EncodeToString([]byte("steer-image")) {
		t.Fatalf("unexpected image block source: %#v", source)
	}
	if tr.activeTurn == nil || tr.activeTurn.ThreadID != threadID || tr.activeTurn.TurnID != turnID {
		t.Fatalf("expected active turn to remain unchanged, got %#v", tr.activeTurn)
	}
	if len(tr.pendingTurns) != 0 {
		t.Fatalf("expected steer not to create pending turns, got %#v", tr.pendingTurns)
	}
}

func TestClaudeTranslatorTurnSteerPreservesMixedInputOrderWithinActiveTurn(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "default")
	imagePath := filepath.Join(t.TempDir(), "steer-mixed.png")
	if err := os.WriteFile(imagePath, []byte("steer-mixed-image"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	payloads, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-steer-mixed",
		Kind:      agentproto.CommandTurnSteer,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target:    agentproto.Target{ThreadID: threadID, TurnID: turnID},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{
			{Type: agentproto.InputText, Text: "附带参考文件：/tmp/notes.txt"},
			{Type: agentproto.InputLocalImage, Path: imagePath, MIMEType: "image/png"},
			{Type: agentproto.InputText, Text: "请结合图片继续处理"},
		}},
	})
	if err != nil {
		t.Fatalf("translate turn steer: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one steer payload, got %#v", payloads)
	}
	payload := decodeFrame(t, payloads[0])
	message := testMapValue(payload["message"])
	content := testSliceMapValue(message["content"])
	if len(content) != 3 {
		t.Fatalf("expected text + image + text blocks, got %#v", message["content"])
	}
	if content[0]["type"] != "text" || lookupStringFromAny(content[0]["text"]) != "附带参考文件：/tmp/notes.txt" {
		t.Fatalf("unexpected leading text block: %#v", content[0])
	}
	if content[1]["type"] != "image" {
		t.Fatalf("unexpected image block: %#v", content[1])
	}
	source := testMapValue(content[1]["source"])
	if source["type"] != "base64" || lookupStringFromAny(source["media_type"]) != "image/png" || lookupStringFromAny(source["data"]) != base64.StdEncoding.EncodeToString([]byte("steer-mixed-image")) {
		t.Fatalf("unexpected image block source: %#v", source)
	}
	if content[2]["type"] != "text" || lookupStringFromAny(content[2]["text"]) != "请结合图片继续处理" {
		t.Fatalf("unexpected trailing text block: %#v", content[2])
	}
}

func TestClaudeTranslatorTurnSteerRejectsUnsupportedInputs(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "default")

	_, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-steer-remote-image",
		Kind:      agentproto.CommandTurnSteer,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target:    agentproto.Target{ThreadID: threadID, TurnID: turnID},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{
			{Type: agentproto.InputRemoteImage, URL: "https://example.test/reply.png", MIMEType: "image/png"},
		}},
	})
	problem := expectClaudeCommandError(t, err)
	if problem.Code != "claude_steer_inputs_unsupported" || !strings.Contains(problem.Details, "unsupported prompt input type") {
		t.Fatalf("unexpected problem: %#v", problem)
	}
}
