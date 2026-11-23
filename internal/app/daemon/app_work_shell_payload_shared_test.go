package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestTeamReactionShellPayloadUsesOriginalEncoding(t *testing.T) {
	for _, inputs := range [][]agentproto.Input{nil, {{Type: agentproto.InputText, Text: " \n "}}, {{Type: agentproto.InputText, Text: ""}, {Type: agentproto.InputText, Text: "caption"}, {Type: agentproto.InputLocalImage, Path: " /tmp/a b.png ", MIMEType: " image/png "}}, {{Type: agentproto.InputRemoteImage, Path: " ", URL: " https://example.invalid/image "}}, {{Type: agentproto.InputLocalImage, Path: " "}}, {{Type: agentproto.InputRemoteImage, URL: " "}}} {
		got, err := teamReactionShellPayload(inputs)
		want, _, wantErr := agentproto.QueuedInputShellPayload(inputs)
		if got != want || (err == nil) != (wantErr == nil) {
			t.Fatalf("team caller diverged: %q %v; want %q %v", got, err, want, wantErr)
		}
	}
}
