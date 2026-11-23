package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type predictionEventRecorder struct{ events chan []agentproto.Event }

func (r predictionEventRecorder) SendEvents(events []agentproto.Event) error {
	r.events <- events
	return nil
}

func predictionRequest() agentproto.Command {
	return agentproto.Command{Kind: agentproto.CommandThreadPredictionRequest, CommandID: "prediction:test", Target: agentproto.Target{ThreadID: "thread"}, Prediction: agentproto.PredictionCommand{SourceTurnID: "turn"}}
}

func predictionFrame(t *testing.T, frames <-chan []byte) map[string]any {
	t.Helper()
	select {
	case raw := <-frames:
		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatal(err)
		}
		return frame
	case <-time.After(2 * time.Second):
		t.Fatal("native prediction frame did not arrive")
		return nil
	}
}

func predictionReply(t *testing.T, runtime *codexBackendRuntime, frame map[string]any, result any) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"id": frame["id"], "result": result})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := runtime.ObserveServer(raw)
	if err != nil || !observed.Suppress || len(observed.Events) != 0 || len(observed.OutboundToParent) != 0 {
		t.Fatalf("private native response escaped: %#v, %v", observed, err)
	}
}

func TestPredictionPreparationUsesOfficialConfigAndOriginalThread(t *testing.T) {
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	runtime.predictions = newTestPredictionRuntime()
	runtime.nativeUserAgent = "Codex Desktop/0.167.0-alpha.4.1 (Mac OS 26.7.2; arm64)"
	runtime.predictions.desktopContext = func(context.Context, string) (map[string]any, error) { return map[string]any{"locale": "zh-CN"}, nil }
	runtime.predictions.httpClient = &http.Client{Transport: predictionRoundTrip(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/statsig/bootstrap") {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(controlsPayload("user", "account")))}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"is_enabled":true,"prompt":"official dynamic prompt","prompt_with_schema":"  official prepared prompt  ","prompt_version":"5","prediction_reasoning_effort":"high","live_sample_rate":1}`))}, nil
	})}
	frames := make(chan []byte, 16)
	events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !runtime.HandlePrediction(ctx, predictionRequest(), frames, events) {
		t.Fatal("request not intercepted")
	}
	auth := predictionFrame(t, frames)
	if auth["method"] != "getAuthStatus" {
		t.Fatalf("first method = %v", auth["method"])
	}
	// Preparation is asynchronous and does not hold the ordinary command mutex.
	ordinary := make(chan struct{})
	go func() { runtime.Capabilities(); runtime.mu.Lock(); runtime.mu.Unlock(); close(ordinary) }()
	select {
	case <-ordinary:
	case <-time.After(time.Second):
		t.Fatal("preparation blocked ordinary commands")
	}
	predictionReply(t, runtime, auth, map[string]any{"authMethod": "chatgpt", "authToken": predictionToken(t, "user", "account")})
	account := predictionFrame(t, frames)
	if account["method"] != "account/read" {
		t.Fatalf("account method = %v", account["method"])
	}
	predictionReply(t, runtime, account, map[string]any{"account": map[string]any{"type": "chatgpt"}, "workspaceRouting": map[string]any{"chatgptAccountId": "account", "backendOrigin": "https://chatgpt.com", "accountRoutingOverride": "NO_CONSTRAINT"}})
	requirements := predictionFrame(t, frames)
	if requirements["method"] != "configRequirements/read" {
		t.Fatalf("requirements method = %v", requirements["method"])
	}
	predictionReply(t, runtime, requirements, map[string]any{"requirements": nil})
	resume := predictionFrame(t, frames)
	if resume["method"] != "thread/resume" || resume["params"].(map[string]any)["threadId"] != "thread" {
		t.Fatalf("wrong resume: %#v", resume)
	}
	predictionReply(t, runtime, resume, map[string]any{"modelProvider": "openai", "model": "gpt-supported", "reasoningEffort": "low", "collaborationMode": map[string]any{"mode": "default"}})
	sample := predictionFrame(t, frames)
	params := sample["params"].(map[string]any)
	if sample["method"] != "thread/sample" || params["prompt"] != "  official prepared prompt  " || params["effort"] != "high" || params["threadId"] != "thread" || params["expectedTurnId"] != "turn" || params["interruptOnCancel"] != true {
		t.Fatalf("sample differs from official options: %#v", params)
	}
	if params["responsesapiClientMetadata"].(map[string]any)["prediction_trigger"] != "live" {
		t.Fatal("not a live generation")
	}
	if _, err := runtime.ObserveServer([]byte(`{"method":"turn/moderationMetadata","params":{"threadId":"thread","turnId":"turn","metadata":{"prompt":{"provider":{"outputs":[{"results":[{"labels":["official-user-label"]}]}]}}}}}`)); err != nil {
		t.Fatal(err)
	}
	response, _ := json.Marshal(map[string]any{"id": sample["id"], "result": map[string]any{"sourceTurnId": "turn", "text": `{"suggestion":"native suggestion"}`}})
	observed, err := runtime.ObserveServer(response)
	if err != nil || len(observed.Events) != 1 || observed.Events[0].Prediction.Suggestion != "" {
		t.Fatalf("official source filter was not applied: %+v %v", observed, err)
	}

}

func TestPredictionCancelBeforeRequestPreventsPreparation(t *testing.T) {
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	t.Cleanup(runtime.CancelPredictions)
	ctx := context.Background()
	frames := make(chan []byte, 4)
	events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
	request := predictionRequest()
	cancel := agentproto.Command{
		Kind: agentproto.CommandThreadPredictionCancel, CommandID: "prediction-cancel:test",
		Prediction: agentproto.PredictionCommand{RequestCommandID: request.CommandID},
	}

	if !runtime.HandlePrediction(ctx, cancel, frames, events) {
		t.Fatal("early cancel was not intercepted")
	}
	for range 2 {
		if !runtime.HandlePrediction(ctx, request, frames, events) {
			t.Fatal("late request was not intercepted")
		}
	}
	runtime.mu.Lock()
	job := runtime.predictions.jobs[request.CommandID]
	runtime.mu.Unlock()
	if job != nil {
		t.Fatal("late canceled request started native preparation")
	}

	request.CommandID = "prediction:new"
	runtime.HandlePrediction(ctx, request, frames, events)
	if frame := predictionFrame(t, frames); frame["method"] != "getAuthStatus" {
		t.Fatalf("an unrelated request did not start preparation: %#v", frame)
	}
}

func TestPredictionCancelDuringPreparationSuppressesLateAuth(t *testing.T) {
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	frames := make(chan []byte, 16)
	events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
	runtime.HandlePrediction(context.Background(), predictionRequest(), frames, events)
	auth := predictionFrame(t, frames)
	cancel := agentproto.Command{Kind: agentproto.CommandThreadPredictionCancel, CommandID: "prediction-cancel:test", Prediction: agentproto.PredictionCommand{RequestCommandID: "prediction:test"}}
	runtime.HandlePrediction(context.Background(), cancel, frames, events)
	predictionReply(t, runtime, auth, map[string]any{"authMethod": "chatgpt", "authToken": predictionToken(t, "user", "account")})
	select {
	case frame := <-frames:
		t.Fatalf("canceled preparation sent more native frames: %s", frame)
	case <-time.After(30 * time.Millisecond):
	}
	select {
	case event := <-events.events:
		t.Fatalf("canceled request emitted events: %#v", event)
	default:
	}
}

func TestPredictionPreparationFailureEndsRoundQuietly(t *testing.T) {
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	frames := make(chan []byte, 16)
	events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
	runtime.HandlePrediction(context.Background(), predictionRequest(), frames, events)
	auth := predictionFrame(t, frames)
	predictionReply(t, runtime, auth, map[string]any{"authMethod": "apiKey", "authToken": "must-not-use"})
	select {
	case result := <-events.events:
		if len(result) != 1 || result[0].Prediction == nil || result[0].Prediction.Suggestion != "" || result[0].CommandID != "prediction:test" || result[0].ErrorMessage != "" {
			t.Fatalf("failure escaped as ordinary error: %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("preparation failure left round pending")
	}
}

func TestPredictionFullQueueDiscardsUnsentPreparation(t *testing.T) {
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	runtime.predictions = newTestPredictionRuntime()
	frames := make(chan []byte, 1)
	frames <- []byte("occupied")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := runtime.predictionRPC(ctx, frames, "getAuthStatus", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("preparation blocked on full writer: %v", err)
	}
	if got := len(runtime.predictions.pending); got != 0 {
		t.Fatalf("unsent preparations retained in pending map: %d", got)
	}
	if raw := <-frames; string(raw) != "occupied" {
		t.Fatalf("full queue unexpectedly received a preparation frame: %s", raw)
	}
}

func TestPredictionFullQueueDiscardsUnsentSample(t *testing.T) {
	runtime, frames, events := predictionTimeoutFixture(t)
	predictionAuthenticate(t, runtime, frames, "user", "account", false)
	resume := predictionFrame(t, frames)
	if resume["method"] != "thread/resume" {
		t.Fatalf("unexpected preparation: %#v", resume)
	}
	// Fill the native write queue before letting the resume response advance to sampling.
	for range cap(frames) {
		frames <- []byte("occupied")
	}
	predictionReply(t, runtime, resume, map[string]any{"modelProvider": "openai", "model": "supported"})

	request := predictionRequest()
	select {
	case batch := <-events.events:
		if len(batch) != 1 || batch[0].Kind != agentproto.EventThreadPredictionResult ||
			batch[0].CommandID != request.CommandID || batch[0].Prediction == nil ||
			batch[0].Prediction.SourceTurnID != request.Prediction.SourceTurnID || batch[0].Prediction.Suggestion != "" {
			t.Fatalf("full queue must end its prediction quietly: %#v", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("full native queue stalled prediction completion")
	}
	for range cap(frames) {
		if frame := <-frames; string(frame) != "occupied" {
			t.Fatalf("full queue unexpectedly received a prediction frame: %s", frame)
		}
	}

	runtime.mu.Lock()
	_, err := runtime.translator.TranslatePredictionRequest(request, codex.PredictionOptions{Prompt: "official prompt"})
	runtime.mu.Unlock()
	if err != nil {
		t.Fatalf("translator retained the prediction that never reached native: %v", err)
	}
}

func TestPredictionCancelNeverBlocksFullChildQueue(t *testing.T) {
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	runtime.predictions = newTestPredictionRuntime()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := predictionRequest()
	outbound, err := runtime.translator.TranslatePredictionRequest(command, codex.PredictionOptions{Prompt: "official"})
	if err != nil {
		t.Fatal(err)
	}
	var sample map[string]any
	if err := json.Unmarshal(outbound[0], &sample); err != nil {
		t.Fatal(err)
	}
	index, err := strconv.Atoi(strings.TrimPrefix(sample["id"].(string), "relay-prediction-sample-"))
	if err != nil {
		t.Fatal(err)
	}
	frames := make(chan []byte, 1)
	runtime.predictions.jobs[command.CommandID] = &predictionJob{ctx: ctx, cancel: cancel, command: command, sampled: true, frames: frames}
	frames <- []byte("occupied")
	done := make(chan struct{})
	go func() {
		runtime.HandlePrediction(context.Background(), agentproto.Command{Kind: agentproto.CommandThreadPredictionCancel, Prediction: agentproto.PredictionCommand{RequestCommandID: command.CommandID}}, frames, predictionEventRecorder{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel blocked behind full queue")
	}
	if frame := <-frames; string(frame) != "occupied" {
		t.Fatalf("full queue unexpectedly received a cancel frame: %s", frame)
	}
	// The already-sent sample stays private, while a dropped cancel has no response to consume.
	predictionReply(t, runtime, sample, map[string]any{})
	cancelID := "relay-prediction-cancel-" + strconv.Itoa(index+1)
	result, err := runtime.ObserveServer([]byte(`{"id":"` + cancelID + `","result":{}}`))
	if err != nil || result.Suppress {
		t.Fatalf("unsent cancel is still owned: %#v, %v", result, err)
	}
}

func TestPredictionDisconnectCancelsNativeSample(t *testing.T) {
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	runtime.predictions = newTestPredictionRuntime()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := predictionRequest()
	if _, err := runtime.translator.TranslatePredictionRequest(command, codex.PredictionOptions{Prompt: "official"}); err != nil {
		t.Fatal(err)
	}
	frames := make(chan []byte, 2)
	runtime.predictions.jobs[command.CommandID] = &predictionJob{ctx: ctx, cancel: cancel, command: command, sampled: true, frames: frames}
	runtime.CancelPredictions()
	frame := predictionFrame(t, frames)
	if frame["method"] != "thread/sample/cancel" {
		t.Fatalf("disconnect did not cancel native sample: %#v", frame)
	}
}

func TestPredictionAccountStatusRefreshPreservesPreparation(t *testing.T) {
	for _, method := range []string{"account/updated", "account/login/completed", "accountLoginCompleted"} {
		t.Run(method, func(t *testing.T) {
			runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
			defer runtime.CancelPredictions()
			frames := make(chan []byte, 16)
			events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
			runtime.HandlePrediction(context.Background(), predictionRequest(), frames, events)
			auth := predictionFrame(t, frames)
			raw, _ := json.Marshal(map[string]any{"method": method, "params": map[string]any{"authMode": "chatgpt"}})
			if _, err := runtime.ObserveServer(raw); err != nil {
				t.Fatal(err)
			}
			predictionReply(t, runtime, auth, map[string]any{"authMethod": "chatgpt", "authToken": predictionToken(t, "user", "account")})
			if frame := predictionFrame(t, frames); frame["method"] != "account/read" {
				t.Fatalf("status refresh canceled initial auth preparation: %#v", frame)
			}
		})
	}
}

func predictionAuthenticate(t *testing.T, runtime *codexBackendRuntime, frames <-chan []byte, user, account string, refresh bool) {
	t.Helper()
	auth := predictionFrame(t, frames)
	if auth["method"] != "getAuthStatus" || auth["params"].(map[string]any)["refreshToken"] != refresh || auth["params"].(map[string]any)["includeToken"] != true {
		t.Fatalf("auth flags differ from desktop: %#v", auth)
	}
	predictionReply(t, runtime, auth, map[string]any{"authMethod": "chatgptAuthTokens", "authToken": predictionToken(t, user, account)})
	accountFrame := predictionFrame(t, frames)
	if accountFrame["method"] != "account/read" {
		t.Fatal("expected account/read")
	}
	predictionReply(t, runtime, accountFrame, map[string]any{"account": map[string]any{"type": "chatgpt"}, "workspaceRouting": map[string]any{"chatgptAccountId": account, "backendOrigin": "https://chatgpt.com", "accountRoutingOverride": "NO_CONSTRAINT"}})
	requirements := predictionFrame(t, frames)
	if requirements["method"] != "configRequirements/read" {
		t.Fatal("expected configRequirements/read")
	}
	predictionReply(t, runtime, requirements, map[string]any{"requirements": nil})
}

func TestPredictionOfficialRefreshIsBoundToOriginalPrincipal(t *testing.T) {
	for _, account := range []string{"account", "other-account"} {
		t.Run(account, func(t *testing.T) {
			runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
			runtime.predictions = newTestPredictionRuntime()
			requests := 0
			runtime.predictions.httpClient = &http.Client{Transport: predictionRoundTrip(func(req *http.Request) (*http.Response, error) {
				requests++
				if requests == 1 {
					return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"is_enabled":false,"prompt":"official","prompt_version":"5","prediction_reasoning_effort":"high"}`))}, nil
			})}
			frames := make(chan []byte, 16)
			events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
			runtime.HandlePrediction(context.Background(), predictionRequest(), frames, events)
			predictionAuthenticate(t, runtime, frames, "user", "account", false)
			refresh := predictionFrame(t, frames)
			if refresh["method"] != "getAuthStatus" || refresh["params"].(map[string]any)["refreshToken"] != true {
				t.Fatal("401 did not refresh native token")
			}
			predictionReply(t, runtime, refresh, map[string]any{"authMethod": "chatgpt", "authToken": predictionToken(t, "user", account)})
			select {
			case <-events.events:
			case <-time.After(time.Second):
				t.Fatal("config refresh left round pending")
			}
			want := 1
			if account == "account" {
				want = 2
			}
			if requests != want {
				t.Fatalf("config requests=%d; want %d", requests, want)
			}
		})
	}
}

func TestPredictionAccountNotificationCancelsOnlyConfigAuthScope(t *testing.T) {
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	runtime.predictions = newTestPredictionRuntime()
	started := make(chan struct{})
	runtime.predictions.httpClient = &http.Client{Transport: predictionRoundTrip(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	frames := make(chan []byte, 16)
	events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
	runtime.HandlePrediction(context.Background(), predictionRequest(), frames, events)
	predictionAuthenticate(t, runtime, frames, "user", "account", false)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("config HTTP did not start")
	}
	_, err := runtime.ObserveServer([]byte(`{"method":"account/updated","params":{"authMode":"chatgpt","planType":"plus"}}`))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-events.events:
	case <-time.After(time.Second):
		t.Fatal("backend-auth abort did not finish query quietly")
	}
	runtime.predictions.configMu.Lock()
	cached, exists := runtime.predictions.cache["user\x00account"]
	runtime.predictions.configMu.Unlock()
	if !exists || cached.config.Enabled {
		t.Fatal("backend-auth abort with live query must cache desktop null")
	}
}

func TestPredictionAccountNotificationDoesNotCancelSampleScope(t *testing.T) {
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	runtime.predictions = newTestPredictionRuntime()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := predictionRequest()
	outbound, err := runtime.translator.TranslatePredictionRequest(command, codex.PredictionOptions{Prompt: "official"})
	if err != nil {
		t.Fatal(err)
	}
	var sample map[string]any
	if err = json.Unmarshal(outbound[0], &sample); err != nil {
		t.Fatal(err)
	}
	frames := make(chan []byte, 2)
	runtime.predictions.jobs[command.CommandID] = &predictionJob{ctx: ctx, cancel: cancel, command: command, sampled: true, frames: frames}
	if _, err = runtime.ObserveServer([]byte(`{"method":"account/updated","params":{"authMode":"chatgpt","planType":"plus"}}`)); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("backend-auth invalidation canceled sample query")
	}
	raw, _ := json.Marshal(map[string]any{"id": sample["id"], "result": map[string]any{"sourceTurnId": "turn", "text": `{"suggestion":"native result"}`}})
	observed, err := runtime.ObserveServer(raw)
	if err != nil || len(observed.Events) != 1 || observed.Events[0].Prediction == nil || observed.Events[0].Prediction.Suggestion != "native result" {
		t.Fatalf("valid sample disappeared after backend-auth invalidation: %#v %v", observed, err)
	}
	select {
	case frame := <-frames:
		t.Fatalf("auth notification canceled native sample: %s", frame)
	default:
	}
}

func accountPredictionSample(t *testing.T) (*codexBackendRuntime, context.Context, chan []byte, []byte) {
	t.Helper()
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	runtime.predictions = newTestPredictionRuntime()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	command := predictionRequest()
	outbound, err := runtime.translator.TranslatePredictionRequest(command, codex.PredictionOptions{Prompt: "official"})
	if err != nil {
		t.Fatal(err)
	}
	var sample map[string]any
	if err := json.Unmarshal(outbound[0], &sample); err != nil {
		t.Fatal(err)
	}
	frames := make(chan []byte, 2)
	job := &predictionJob{ctx: ctx, cancel: cancel, command: command, sampled: true, frames: frames}
	runtime.predictions.jobs[command.CommandID] = job
	runtime.rememberPredictionPrincipal(job, predictionIdentity{UserID: "user", AccountID: "account"})
	response, err := json.Marshal(map[string]any{"id": sample["id"], "result": map[string]any{"sourceTurnId": "turn", "text": `{"suggestion":"native suggestion"}`}})
	if err != nil {
		t.Fatal(err)
	}
	return runtime, ctx, frames, response
}

func TestPredictionAccountLogoutCancelsSampleAndInvalidatesDrafts(t *testing.T) {
	for _, params := range []string{`{"authMode":null,"planType":null}`, `{}`, `{"authMode":"apikey"}`} {
		t.Run(params, func(t *testing.T) {
			runtime, ctx, frames, response := accountPredictionSample(t)
			observed, err := runtime.ObserveServer([]byte(`{"method":"account/updated","params":` + params + `}`))
			if err != nil {
				t.Fatal(err)
			}
			if ctx.Err() == nil || !slices.ContainsFunc(observed.Events, func(event agentproto.Event) bool {
				return event.Kind == agentproto.EventThreadPredictionInvalidated
			}) {
				t.Fatalf("account logout must cancel jobs and invalidate drafts: %#v", observed.Events)
			}
			if cancellation := predictionFrame(t, frames); cancellation["method"] != "thread/sample/cancel" {
				t.Fatalf("wrong native cancellation: %#v", cancellation)
			}
			observed, err = runtime.ObserveServer(response)
			if err != nil || len(observed.Events) != 0 || !observed.Suppress {
				t.Fatalf("old native response must stay private and discarded: %#v %v", observed, err)
			}
		})
	}
}

func TestPredictionAccountVerifiesPrincipalBeforePublishingSample(t *testing.T) {
	for _, test := range []struct {
		name, user, account, notification string
		changed                           bool
	}{
		{"same principal", "user", "account", `{"method":"account/updated","params":{"authMode":"chatgpt"}}`, false},
		{"different account", "user", "new-account", `{"method":"account/updated","params":{"authMode":"chatgptAuthTokens"}}`, true},
		{"different user", "new-user", "account", `{"method":"account/login/completed","params":{"success":true}}`, true},
		{"unverified", "", "", `{"method":"account/updated","params":{"authMode":"chatgpt"}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime, ctx, frames, response := accountPredictionSample(t)
			observed, err := runtime.ObserveServer([]byte(test.notification))
			if err != nil || observed.Suppress {
				t.Fatalf("native account notification must continue to the client: %#v %v", observed, err)
			}
			probe := predictionFrame(t, frames)
			params, _ := probe["params"].(map[string]any)
			if probe["method"] != "getAuthStatus" || params["includeToken"] != true || params["refreshToken"] != false {
				t.Fatalf("account verification must quietly read native identity: %#v", probe)
			}
			observed, err = runtime.ObserveServer(response)
			if err != nil || !observed.Suppress || len(observed.Events) != 0 || ctx.Err() != nil {
				t.Fatalf("sample must wait for the identity check: %#v %v", observed, err)
			}
			authToken := ""
			if test.user != "" {
				authToken = predictionToken(t, test.user, test.account)
			}
			reply, err := json.Marshal(map[string]any{"id": probe["id"], "result": map[string]any{"authMethod": "chatgpt", "authToken": authToken}})
			if err != nil {
				t.Fatal(err)
			}
			observed, err = runtime.ObserveServer(reply)
			if err != nil || !observed.Suppress || len(observed.OutboundToParent) != 0 || len(observed.Events) != 1 || ctx.Err() == nil {
				t.Fatalf("native credential response must stay private and settle the held sample: %#v %v", observed, err)
			}
			event := observed.Events[0]
			if test.changed {
				if event.Kind != agentproto.EventThreadPredictionInvalidated || event.Prediction != nil {
					t.Fatalf("old suggestion survived confirmed principal change: %#v", event)
				}
			} else {
				want := "native suggestion"
				if test.user == "" {
					want = ""
				}
				if event.Kind != agentproto.EventThreadPredictionResult || event.Prediction == nil || event.Prediction.Suggestion != want {
					t.Fatalf("unexpected suggestion after native principal check: %#v", event)
				}
			}
		})
	}
}

func TestPredictionAccountProbeDoesNotBlockFullChildQueue(t *testing.T) {
	runtime, ctx, frames, response := accountPredictionSample(t)
	for range cap(frames) {
		frames <- []byte("ordinary frame")
	}
	done := make(chan error, 1)
	go func() {
		_, err := runtime.ObserveServer([]byte(`{"method":"account/updated","params":{"authMode":"chatgpt"}}`))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("full native queue blocked account notification")
	}
	if ctx.Err() == nil {
		t.Fatal("unverifiable in-flight sample was not canceled")
	}
	observed, err := runtime.ObserveServer(response)
	if err != nil || len(observed.Events) != 0 || !observed.Suppress {
		t.Fatalf("late unverifiable sample must stay private and discarded: %#v %v", observed, err)
	}
}

type predictionScopeProbeContext struct {
	context.Context
	derived int
}

func (c *predictionScopeProbeContext) Done() <-chan struct{} { c.derived++; return nil }

func TestPredictionCachedConfigDoesNotCreateBackendAuthScope(t *testing.T) {
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	runtime.predictions = newTestPredictionRuntime()
	runtime.predictions.cache["user\x00account"] = predictionConfigCache{config: predictionConfig{Enabled: true, Prompt: "cached official prompt"}, expires: time.Now().Add(time.Minute)}
	ctx := &predictionScopeProbeContext{Context: context.Background()}
	job := &predictionJob{ctx: ctx, command: predictionRequest()}
	config, err := runtime.predictionConfigForJob(job, "unused on cache hit", predictionIdentity{UserID: "user", AccountID: "account"}, predictionRouting{})
	if err != nil || !config.Enabled {
		t.Fatalf("cached config = %#v %v", config, err)
	}
	if ctx.derived != 0 {
		t.Fatal("cache hit derived a backend-auth scope without HTTP")
	}
}

func TestPredictionEligibilityUsesResumedThreadAndOfficialModelList(t *testing.T) {
	for _, tc := range []struct{ name, config, provider, model, mode string }{
		{"plan", `"live_sample_rate":1`, "openai", "supported", "plan"},
		{"provider", `"live_sample_rate":1`, "other", "supported", "default"},
		{"model", `"live_sample_rate":1,"unsupported_models":["blocked"]`, "openai", "blocked", "default"},
		{"sample-rate-zero", `"live_sample_rate":0`, "openai", "supported", "default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
			runtime.predictions = newTestPredictionRuntime()
			runtime.predictions.httpClient = &http.Client{Transport: predictionRoundTrip(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"is_enabled":true,"prompt":"official","prompt_version":"5","prediction_reasoning_effort":"",` + tc.config + `}`))}, nil
			})}
			frames := make(chan []byte, 16)
			events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
			runtime.HandlePrediction(context.Background(), predictionRequest(), frames, events)
			predictionAuthenticate(t, runtime, frames, "user", "account", false)
			if tc.name != "sample-rate-zero" {
				resume := predictionFrame(t, frames)
				if resume["method"] != "thread/resume" {
					t.Fatal("eligibility did not resume original thread")
				}
				predictionReply(t, runtime, resume, map[string]any{"modelProvider": tc.provider, "model": tc.model, "collaborationMode": map[string]any{"mode": tc.mode}})
			}
			select {
			case <-events.events:
			case <-time.After(time.Second):
				t.Fatal("ineligible prediction did not finish quietly")
			}
			select {
			case frame := <-frames:
				t.Fatalf("ineligible prediction sent sample: %s", frame)
			default:
			}
		})
	}
}
