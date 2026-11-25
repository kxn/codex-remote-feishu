package codex

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func oauthAttemptCommand(id, server, thread string) agentproto.Command {
	return agentproto.Command{CommandID: id, Kind: agentproto.CommandMCPOAuthLogin, Origin: agentproto.Origin{Surface: "surface"}, MCP: agentproto.MCPCommand{OAuthLogin: &agentproto.MCPOAuthLoginCommand{ServerName: server, ThreadID: thread}}}
}
func oauthObserve(t *testing.T, tr *Translator, raw string) Result {
	t.Helper()
	out, err := tr.ObserveServer([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func oauthCompletionFor(out Result, id string) bool {
	for _, e := range out.Events {
		if e.Kind == agentproto.EventMCPOAuthLoginCompleted && e.CommandID == id {
			return true
		}
	}
	return false
}
func TestMCPOAuthAttemptIDGuardsRetryAndOptionalThread(t *testing.T) {
	tr := NewTranslator("instance")
	if _, err := tr.TranslateCommand(oauthAttemptCommand("first", "docs", "thread")); err != nil {
		t.Fatal(err)
	}
	oauthObserve(t, tr, `{"id":"relay-mcp-oauth-login-0","result":{"authorizationUrl":"https://example.invalid/a","loginId":"A"}}`)
	if out := oauthObserve(t, tr, `{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","loginId":"A","success":true}}`); !oauthCompletionFor(out, "first") {
		t.Fatal("matching loginId without optional thread was not completed")
	}
	if _, err := tr.TranslateCommand(oauthAttemptCommand("second", "docs", "thread")); err != nil {
		t.Fatal(err)
	}
	oauthObserve(t, tr, `{"id":"relay-mcp-oauth-login-1","result":{"authorizationUrl":"https://example.invalid/b","loginId":"B"}}`)
	for _, raw := range []string{`{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","threadId":"thread","loginId":"A","success":true}}`, `{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","threadId":"thread","loginId":"unknown","success":true}}`, `{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","threadId":"thread","success":true}}`, `{"method":"mcpServer/oauthLogin/completed","params":{"name":"other","loginId":"B","success":true}}`, `{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","threadId":"other","loginId":"B","success":true}}`} {
		if out := oauthObserve(t, tr, raw); oauthCompletionFor(out, "second") || len(out.Events) != 0 {
			t.Fatalf("unverified completion assigned: %+v", out)
		}
	}
	if out := oauthObserve(t, tr, `{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","loginId":"B","success":true}}`); !oauthCompletionFor(out, "second") {
		t.Fatal("exact second attempt did not complete")
	}
}

func TestMCPOAuthRetrySupersedesUncompletedAttempt(t *testing.T) {
	tr := NewTranslator("instance")
	if _, err := tr.TranslateCommand(oauthAttemptCommand("first", "docs", "thread")); err != nil {
		t.Fatal(err)
	}
	oauthObserve(t, tr, `{"id":"relay-mcp-oauth-login-0","result":{"authorizationUrl":"https://example.invalid/a","loginId":"A"}}`)
	foreign := oauthAttemptCommand("foreign", "docs", "thread")
	foreign.Origin.Surface = "other-surface"
	if _, err := tr.TranslateCommand(foreign); err == nil {
		t.Fatal("foreign surface replaced login attempt")
	}
	if _, err := tr.TranslateCommand(oauthAttemptCommand("retry", "docs", "thread")); err != nil {
		t.Fatal(err)
	}
	if out := oauthObserve(t, tr, `{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","loginId":"A","success":true}}`); len(out.Events) != 0 {
		t.Fatalf("old attempt completed retry: %+v", out.Events)
	}
	oauthObserve(t, tr, `{"id":"relay-mcp-oauth-login-1","result":{"authorizationUrl":"https://example.invalid/b","loginId":"B"}}`)
	if out := oauthObserve(t, tr, `{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","loginId":"A","success":true}}`); len(out.Events) != 0 {
		t.Fatalf("old attempt completed retry: %+v", out.Events)
	}
	if out := oauthObserve(t, tr, `{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","loginId":"B","success":true}}`); !oauthCompletionFor(out, "retry") {
		t.Fatalf("retry not completed: %+v", out.Events)
	}
}
func TestMCPOAuthUnknownIDDoesNotCreatePermanentPending(t *testing.T) {
	tr := NewTranslator("instance")
	if _, err := tr.TranslateCommand(oauthAttemptCommand("first", "docs", "thread")); err != nil {
		t.Fatal(err)
	}
	out := oauthObserve(t, tr, `{"id":"relay-mcp-oauth-login-0","result":{"authorizationUrl":"https://example.invalid/a"}}`)
	if len(out.Events) != 2 || out.Events[0].Kind != agentproto.EventMCPOAuthLoginURLReady || out.Events[1].Problem == nil || !strings.Contains(out.Events[1].Problem.Message, "/mcpoauth") {
		t.Fatalf("missing usable URL and explicit retry: %+v", out.Events)
	}
	if _, err := tr.TranslateCommand(oauthAttemptCommand("retry", "docs", "thread")); err != nil {
		t.Fatalf("unknown attempt blocked retry: %v", err)
	}
	if out = oauthObserve(t, tr, `{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","threadId":"thread","success":true}}`); oauthCompletionFor(out, "retry") || len(out.Events) != 0 {
		t.Fatal("missing ID completed retry")
	}
}
func TestMCPOAuthEarlyCompletionNeedsExactResponseAndRestartNeverGuesses(t *testing.T) {
	tr := NewTranslator("instance")
	if _, err := tr.TranslateCommand(oauthAttemptCommand("current", "docs", "thread")); err != nil {
		t.Fatal(err)
	}
	if out := oauthObserve(t, tr, `{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","loginId":"A","success":true}}`); len(out.Events) != 0 {
		t.Fatal("early unverified completion emitted")
	}
	out := oauthObserve(t, tr, `{"id":"relay-mcp-oauth-login-0","result":{"authorizationUrl":"https://example.invalid/a","loginId":"A"}}`)
	if len(out.Events) != 2 || out.Events[0].Kind != agentproto.EventMCPOAuthLoginURLReady || !oauthCompletionFor(out, "current") {
		t.Fatalf("exact early fact not completed: %+v", out.Events)
	}
	restarted := NewTranslator("instance")
	if _, err := restarted.TranslateCommand(oauthAttemptCommand("new", "docs", "thread")); err != nil {
		t.Fatal(err)
	}
	if out = oauthObserve(t, restarted, `{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","loginId":"A","success":true}}`); oauthCompletionFor(out, "new") || len(out.Events) != 0 {
		t.Fatal("restart guessed old attempt")
	}
	out = oauthObserve(t, restarted, `{"id":"relay-mcp-oauth-login-0","result":{"authorizationUrl":"https://example.invalid/b","loginId":"B"}}`)
	if oauthCompletionFor(out, "new") {
		t.Fatal("old attempt bound to new ID")
	}
	if out = oauthObserve(t, restarted, `{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","loginId":"B","success":false,"error":"cancelled"}}`); !oauthCompletionFor(out, "new") || out.Events[0].Status != "failed" {
		t.Fatalf("exact failure not completed: %+v", out.Events)
	}
	if _, err := restarted.TranslateCommand(oauthAttemptCommand("retry", "docs", "thread")); err != nil {
		t.Fatalf("failure blocked retry: %v", err)
	}
}
