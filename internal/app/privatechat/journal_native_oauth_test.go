package privatechat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestNativeOAuthTranslatorRequiresPersistedRequestProof(t *testing.T) {
	for _, tc := range []struct{ name, response, outcome string }{
		{"accepted", `{"id":"relay-mcp-oauth-login-0","result":{"authorization_url":"https://auth.example/consent"}}`, "accepted"},
		{"rejected", `{"id":"relay-mcp-oauth-login-0","error":{"message":"OAuth is unavailable"}}`, "failed"},
		{"empty URL", `{"id":"relay-mcp-oauth-login-0","result":{}}`, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) { testNativeOAuthTranslatorRequestProof(t, tc.response, tc.outcome) })
	}
}

func testNativeOAuthTranslatorRequestProof(t *testing.T, response, outcome string) {
	ctx := t.Context()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	surface := Owner{"g", "s", "c", "u", "tenant", "app"}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, owner, surface, 1)
	if err != nil {
		t.Fatal(err)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner}
	cmd := agentproto.Command{CommandID: "oauth", Kind: agentproto.CommandMCPOAuthLogin, NativeBinding: &binding, MCP: agentproto.MCPCommand{OAuthLogin: &agentproto.MCPOAuthLoginCommand{ServerName: "docs", ThreadID: "thread"}}}
	dispatch := Dispatch{ID: "oauth", CommandID: "oauth", Lane: "inspect", Mode: "resume", Owner: surface, Generation: 1, Session: NativeSession{Backend: "codex", Namespace: "history", SessionID: "thread", CarrierID: "inc", CarrierGeneration: 1}}
	if err = lease.prepareNativeDispatch(ctx, dispatch, cmd); err != nil {
		t.Fatal(err)
	}
	if err = lease.MarkSending(ctx, dispatch.ID); err != nil {
		t.Fatal(err)
	}
	tr := codex.NewTranslator("instance")
	if _, err = tr.TranslateCommand(cmd); err != nil {
		t.Fatal(err)
	}
	result, err := tr.ObserveServer([]byte(response))
	if err != nil || len(result.Events) != 1 {
		t.Fatalf("response: %+v %v", result, err)
	}
	event := result.Events[0]
	event.NativeBinding = &binding
	event.Seq = 1
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "inc", Sequence: 1, PayloadHash: hash}
	if _, err = lease.ReceiveNativeEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if state, _ := j.DispatchState(ctx, dispatch.ID); state != "sending" {
		t.Fatalf("accepted without native frame proof: %s", state)
	}
	raw, _ := json.Marshal(cmd)
	sum := sha256.Sum256(raw)
	receipt := agentproto.RuntimeCommandReceipt{Version: 1, CarrierIncarnation: "inc", CommandID: cmd.CommandID, PayloadHash: hex.EncodeToString(sum[:]), PreparedFrames: 1, Frames: []agentproto.RuntimeFrameReceipt{{FrameID: 1, NativeRequestID: event.RequestID, NativeMethod: "mcpServer/oauth/login", Stage: "unknown"}}}
	if err = lease.recordCommandReceipt(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	if err = lease.ResolveNativeDispatch(ctx, NativeEventRef{"inc", 1, hash}); err != nil {
		t.Fatal(err)
	}
	if state, _ := j.DispatchState(ctx, dispatch.ID); state != outcome {
		t.Fatalf("native response did not resolve unknown write: %s", state)
	}
}
