package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"testing"
)

func TestLegacyQueuedShellPayloadDelegatesOriginalEncoding(t *testing.T) {
	for _, inputs := range [][]agentproto.Input{nil, {{Type: agentproto.InputText, Text: " \n "}}, {{Type: agentproto.InputText, Text: ""}, {Type: agentproto.InputText, Text: "caption"}, {Type: agentproto.InputLocalImage, Path: " /tmp/a b.png ", MIMEType: " image/png "}}, {{Type: agentproto.InputRemoteImage, Path: " ", URL: " https://example.invalid/image "}}, {{Type: agentproto.InputLocalImage, Path: " "}}, {{Type: agentproto.InputRemoteImage, URL: " "}}} {
		got, ok, err := queuedItemShellPayload(&state.QueueItemRecord{Inputs: inputs})
		want, wantOK, wantErr := agentproto.QueuedInputShellPayload(inputs)
		if got != want || ok != wantOK || (err == nil) != (wantErr == nil) {
			t.Fatalf("legacy caller diverged: %q %v %v; want %q %v %v", got, ok, err, want, wantOK, wantErr)
		}
	}
	if got, ok, err := queuedItemShellPayload(nil); got != "" || ok || err != nil {
		t.Fatal("nil queue item changed")
	}
}
