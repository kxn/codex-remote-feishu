package wrapper

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
)

func TestPredictionControlsRefreshRequiresModernRoutingAccount(t *testing.T) {
	p := newTestPredictionRuntime()
	p.desktopContext = func(context.Context, string) (map[string]any, error) { return map[string]any{"locale": "en-US"}, nil }
	runtime := &codexBackendRuntime{translator: codex.NewTranslator("instance"), predictions: p}
	command := predictionRequest()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames := make(chan []byte, 2)
	job := &predictionJob{ctx: ctx, cancel: cancel, command: command, frames: frames}
	p.jobs[command.CommandID] = job
	calls := 0
	p.httpClient = &http.Client{Transport: predictionRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(controlsPayload("user", "account")))}, nil
	})}
	done := make(chan predictionControls, 1)
	go func() {
		done <- runtime.predictionControlsForJob(job, "old-token", predictionIdentity{UserID: "user", AccountID: "account", RoutingAccountID: "account"}, predictionRouting{ChatGPTAccountID: "account", BackendOrigin: "https://native.example", AccountRoutingOverride: "NO_CONSTRAINT"})
	}()
	refresh := predictionFrame(t, frames)
	raw, _ := json.Marshal(map[string]any{"exp": 9999999999, "https://api.openai.com/auth": map[string]any{"user_id": "user", "account_id": "account"}})
	token := "header." + base64.RawURLEncoding.EncodeToString(raw) + ".signature"
	predictionReply(t, runtime, refresh, map[string]any{"authMethod": "chatgpt", "authToken": token})
	controls := <-done
	if calls != 1 || controls.InterruptOnCancel {
		t.Fatal("refresh accepted account_id fallback without matching modern routing claim")
	}
}

type predictionControlsCommitContext struct {
	context.Context
	checks      int
	commitCheck chan struct{}
}

func (c *predictionControlsCommitContext) Err() error {
	err := c.Context.Err()
	c.checks++
	if c.checks == 2 {
		close(c.commitCheck)
	}
	return err
}
func TestPredictionControlsCanceledBeforeCacheCommit(t *testing.T) {
	p := newTestPredictionRuntime()
	p.desktopContext = func(context.Context, string) (map[string]any, error) { return map[string]any{"locale": "en-US"}, nil }
	runtime := &codexBackendRuntime{translator: codex.NewTranslator("instance"), predictions: p}
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &predictionControlsCommitContext{Context: base, commitCheck: make(chan struct{})}
	transportLocked := make(chan struct{})
	command := predictionRequest()
	job := &predictionJob{ctx: ctx, command: command}
	p.jobs[command.CommandID] = job
	p.httpClient = &http.Client{Transport: predictionRoundTrip(func(req *http.Request) (*http.Response, error) {
		p.configMu.Lock()
		close(transportLocked)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(controlsPayload("user", "account")))}, nil
	})}
	done := make(chan struct{})
	go func() {
		runtime.predictionControlsForJob(job, "native-token", predictionIdentity{UserID: "user", AccountID: "account", RoutingAccountID: "account"}, predictionRouting{ChatGPTAccountID: "account", BackendOrigin: "https://native.example", AccountRoutingOverride: "NO_CONSTRAINT"})
		close(done)
	}()
	select {
	case <-transportLocked:
	case <-time.After(time.Second):
		t.Fatal("bootstrap HTTP did not reach cache-lock fixture")
	}
	select {
	case <-ctx.commitCheck:
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	p.configMu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("controls job did not finish")
	}
	p.configMu.Lock()
	defer p.configMu.Unlock()
	if len(p.controls) != 0 {
		t.Fatal("canceled controls result committed after the pre-lock context check")
	}
}
