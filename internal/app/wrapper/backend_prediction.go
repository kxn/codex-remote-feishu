package wrapper

import (
	"context"
	securerand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// Desktop sendInternalRequest and sampleThread each wait 30 seconds for their
// own response; preparation and relay delivery do not consume the sample budget.
const predictionResponseTimeout = 30 * time.Second

const predictionCanceledRequestLimit = 128

type predictionJob struct {
	ctx           context.Context
	preference    *predictionPreferenceState
	cancel        context.CancelFunc
	command       agentproto.Command
	sampled       bool
	frames        chan<- []byte
	authCancel    context.CancelFunc
	accountHeld   *agentproto.Event
	principal     predictionIdentity
	sampleID      string
	sampleSent    bool
	delayedSample []byte
}

type pendingPredictionRPC struct {
	ctx       context.Context
	response  chan predictionRPCResponse
	authJob   *predictionJob
	authEpoch uint64
}

type predictionRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func newPredictionRuntime() *predictionRuntime {
	return &predictionRuntime{
		jobs: make(map[string]*predictionJob), pending: make(map[string]*pendingPredictionRPC),
		preparationPrefix:  predictionPreparationPrefix + securerand.Text() + ":",
		accountProbePrefix: predictionAccountProbePrefix + securerand.Text() + ":",
		cache:              make(map[string]predictionConfigCache), httpClient: newPredictionHTTPClient(),
		now: time.Now, sample: rand.Float64, desktopContext: readPredictionDesktopContext,
		desktopPreferences: readPredictionDesktopPreferences,
	}
}

// HandlePrediction returns immediately so native preparation never blocks relay commands.
func (r *codexBackendRuntime) HandlePrediction(ctx context.Context, command agentproto.Command, frames chan<- []byte, sender relayEventSender) bool {
	if command.Kind != agentproto.CommandThreadPredictionRequest && command.Kind != agentproto.CommandThreadPredictionCancel {
		return false
	}
	r.mu.Lock()
	if r.predictions == nil {
		r.predictions = newPredictionRuntime()
	}
	if command.Kind == agentproto.CommandThreadPredictionCancel {
		requestID := command.Prediction.RequestCommandID
		// Relay dispatch can deliver the cancel while the request is still on its way.
		if requestID != "" && !slices.Contains(r.predictions.canceledRequests[:], requestID) {
			next := r.predictions.nextCanceledRequest
			r.predictions.canceledRequests[next] = requestID
			r.predictions.nextCanceledRequest = (next + 1) % predictionCanceledRequestLimit
		}
		job := r.predictions.jobs[requestID]
		if job != nil {
			r.endPredictionJobLocked(job, false)
			r.cancelNativePredictionLocked(job)
		}
		r.mu.Unlock()
		return true
	}
	if r.predictions.jobs[command.CommandID] != nil ||
		(command.CommandID != "" && slices.Contains(r.predictions.canceledRequests[:], command.CommandID)) {
		r.mu.Unlock()
		return true
	}
	jobCtx, cancel := context.WithCancel(ctx)
	job := &predictionJob{ctx: jobCtx, cancel: cancel, command: command, frames: frames}
	r.predictions.jobs[command.CommandID] = job
	r.predictionAccount.frames, r.predictionAccount.sender = frames, sender
	r.mu.Unlock()
	go r.preparePrediction(ctx, job, frames, sender)
	return true
}

func (r *codexBackendRuntime) predictionRPC(ctx context.Context, frames chan<- []byte, method string, params any) (json.RawMessage, error) {
	return r.predictionRPCWithAuthOwner(ctx, frames, method, params, nil)
}

func (r *codexBackendRuntime) predictionRPCWithAuthOwner(ctx context.Context, frames chan<- []byte, method string, params any, authJob *predictionJob) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, predictionResponseTimeout)
	defer cancel()
	r.mu.Lock()
	p := r.predictions
	p.sequence++
	id := fmt.Sprintf("%s%d", p.preparationPrefix, p.sequence)
	response := make(chan predictionRPCResponse, 1)
	p.pending[id] = &pendingPredictionRPC{ctx: ctx, response: response, authJob: authJob, authEpoch: r.predictionAccount.epoch}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(p.pending, id)
		r.mu.Unlock()
	}()
	raw, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case frames <- append(raw, '\n'):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case result := <-response:
		if len(result.Error) > 0 && string(result.Error) != "null" {
			return nil, errors.New("native prediction preparation failed")
		}
		if len(result.Result) == 0 {
			return nil, errors.New("native prediction preparation missing result")
		}
		return result.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *codexBackendRuntime) observePredictionPreparationLocked(raw []byte) ([]agentproto.Event, bool) {
	if r.predictions == nil {
		return nil, false
	}
	var frame struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		predictionRPCResponse
	}
	if json.Unmarshal(raw, &frame) != nil || frame.Method != "" ||
		!strings.HasPrefix(frame.ID, r.predictions.preparationPrefix) {
		return nil, false
	}
	pending := r.predictions.pending[frame.ID]
	delete(r.predictions.pending, frame.ID)
	if pending == nil || pending.ctx.Err() != nil {
		// A private late response can still contain credentials.
		return nil, true
	}
	events := r.observePredictionAuthReadLocked(pending, frame.predictionRPCResponse)
	pending.response <- frame.predictionRPCResponse
	return events, true
}

func (r *codexBackendRuntime) predictionAuthToken(ctx context.Context, job *predictionJob, refresh bool) (string, predictionIdentity, error) {
	raw, err := r.predictionRPCWithAuthOwner(ctx, job.frames, "getAuthStatus", map[string]any{"includeToken": true, "refreshToken": refresh}, job)
	if err != nil {
		return "", predictionIdentity{}, err
	}
	return decodePredictionAuthToken(raw)
}

func decodePredictionAuthToken(raw json.RawMessage) (string, predictionIdentity, error) {
	var auth struct {
		Method string `json:"authMethod"`
		Token  string `json:"authToken"`
	}
	if json.Unmarshal(raw, &auth) != nil || (auth.Method != "chatgpt" && auth.Method != "chatgptAuthTokens") {
		return "", predictionIdentity{}, errors.New("prediction requires native ChatGPT authentication")
	}
	identity, err := parsePredictionIdentity(auth.Token)
	return auth.Token, identity, err
}

func (r *codexBackendRuntime) predictionAuthentication(job *predictionJob) (string, predictionIdentity, predictionRouting, error) {
	token, identity, err := r.predictionAuthToken(job.ctx, job, false)
	if err != nil {
		return "", predictionIdentity{}, predictionRouting{}, err
	}
	if !r.lockVerifiedPredictionAccount(job) {
		return "", predictionIdentity{}, predictionRouting{}, context.Canceled
	}
	r.mu.Unlock()
	account, err := r.predictionRPC(job.ctx, job.frames, "account/read", map[string]any{"refreshToken": false})
	if err != nil {
		return "", predictionIdentity{}, predictionRouting{}, err
	}
	requirements, err := r.predictionRPC(job.ctx, job.frames, "configRequirements/read", map[string]any{})
	if err != nil {
		return "", predictionIdentity{}, predictionRouting{}, err
	}
	routing, err := parsePredictionRouting(account, requirements, identity)
	return token, identity, routing, err
}

func (r *codexBackendRuntime) predictionConfigForJob(job *predictionJob, token string, identity predictionIdentity, routing predictionRouting) (config predictionConfig, err error) {
	if cached, exists := r.predictions.cachedConfig(identity); exists {
		return cached, nil
	}
	r.mu.Lock()
	authCtx, authCancel := context.WithCancel(job.ctx)
	job.authCancel = authCancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		scopeErr := authCtx.Err()
		job.authCancel = nil
		r.mu.Unlock()
		authCancel()
		if err == nil && scopeErr != nil {
			err = scopeErr
		}
		if err != nil && job.ctx.Err() == nil {
			_ = r.predictions.cacheConfig(job.ctx, identity, predictionConfig{})
		}
	}()
	config, err = r.predictions.fetchConfigWithAuthScope(job.ctx, authCtx, token, identity, routing)
	if !errors.Is(err, errPredictionConfigUnauthorized) {
		return config, err
	}
	// Desktop refreshes only the token; this auth scope retains its original routing.
	token, refreshed, err := r.predictionAuthToken(authCtx, job, true)
	if err != nil {
		return predictionConfig{}, err
	}
	if identity.UserID != refreshed.UserID || identity.AccountID != refreshed.AccountID || routing.ChatGPTAccountID != refreshed.RoutingAccountID {
		return predictionConfig{}, errors.New("native prediction principal changed")
	}
	return r.predictions.fetchConfigWithAuthScope(job.ctx, authCtx, token, refreshed, routing)
}

func (r *codexBackendRuntime) preparePrediction(relayCtx context.Context, job *predictionJob, frames chan<- []byte, sender relayEventSender) {
	command := job.command
	completed := false
	defer func() {
		if completed {
			return
		}
		r.mu.Lock()
		current := r.predictions.jobs[command.CommandID] == job
		canceled := errors.Is(job.ctx.Err(), context.Canceled)
		if current {
			r.endPredictionJobLocked(job, false)
		}
		r.mu.Unlock()
		if current && !canceled {
			_ = sender.SendEvents([]agentproto.Event{{Kind: agentproto.EventThreadPredictionResult, CommandID: command.CommandID, ThreadID: command.Target.ThreadID, Prediction: &agentproto.PredictionResult{SourceTurnID: command.Prediction.SourceTurnID}}})
		}
	}()
	if strings.TrimSpace(command.CommandID) == "" || strings.TrimSpace(command.Target.ThreadID) == "" || strings.TrimSpace(command.Prediction.SourceTurnID) == "" {
		return
	}
	token, identity, routing, err := r.predictionAuthentication(job)
	if err != nil {
		return
	}
	preferences, allowed := r.predictionPreferencesForJob(job, identity)
	if !allowed {
		return
	}
	job.preference = preferences
	config, err := r.predictionConfigForJob(job, token, identity, routing)
	if err != nil || !config.Enabled || config.LiveSampleRate <= r.predictions.sample() {
		return
	}
	resume, err := r.predictionRPC(job.ctx, frames, "thread/resume", map[string]any{"threadId": command.Target.ThreadID})
	if err != nil {
		return
	}
	var state struct {
		ModelProvider     string  `json:"modelProvider"`
		Model             string  `json:"model"`
		ReasoningEffort   *string `json:"reasoningEffort"`
		CollaborationMode *struct {
			Mode string `json:"mode"`
		} `json:"collaborationMode"`
	}
	if json.Unmarshal(resume, &state) != nil || state.ModelProvider != "openai" || state.Model == "" || (state.CollaborationMode != nil && state.CollaborationMode.Mode == "plan") || slices.Contains(config.UnsupportedModels, state.Model) {
		return
	}
	controls := r.predictionControlsForJob(job, token, identity, routing)
	if !controls.Evaluated || !r.desktopPredictionAllowed(job.ctx, job.preference) {
		return
	}
	if !r.lockVerifiedPredictionAccount(job) {
		return
	}
	defer r.mu.Unlock()
	// Keep invalidation ahead of a new sample if the relay outbox was full.
	if watch := r.predictions.preferenceWatch; watch != nil && watch.invalidated {
		return
	}
	r.translator.SetPredictionPolicy(controls.ModerationPolicy)
	outbound, err := r.translator.TranslatePredictionRequest(command, codex.PredictionOptions{Prompt: config.Prompt, PromptWithSchema: config.PromptWithSchema, Effort: config.Effort, GenerationTrigger: "live", InterruptOnCancel: controls.InterruptOnCancel && predictionInterruptSupported(r.nativeUserAgent)})
	if err != nil {
		return
	}
	// The write queue is bounded; do not hold the native observer mutex waiting on it.
	frame := outbound[0]
	job.sampleID = predictionWriteFrame(frame).ID
	select {
	case frames <- frame:
	default:
		r.translator.DiscardUnsentPrediction(command.CommandID)
		return
	}
	job.sampled = true
	r.watchPredictionPreferenceLocked(relayCtx, job.preference, sender)
	completed = true
	timer := time.NewTimer(predictionResponseTimeout)
	go r.waitPredictionSample(job, timer, sender)
}

func (r *codexBackendRuntime) waitPredictionSample(job *predictionJob, timer *time.Timer, sender relayEventSender) {
	defer timer.Stop()
	select {
	case <-job.ctx.Done():
		return
	case <-timer.C:
	}
	r.mu.Lock()
	command := job.command
	if r.predictions.jobs[command.CommandID] != job || job.ctx.Err() != nil {
		r.mu.Unlock()
		return
	}
	r.endPredictionJobLocked(job, false)
	r.cancelNativePredictionLocked(job)
	r.mu.Unlock()
	_ = sender.SendEvents([]agentproto.Event{{Kind: agentproto.EventThreadPredictionResult, CommandID: command.CommandID,
		ThreadID: command.Target.ThreadID, Prediction: &agentproto.PredictionResult{SourceTurnID: command.Prediction.SourceTurnID}}})
}

func (r *codexBackendRuntime) CancelPredictions() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invalidatePredictionAccountLocked(predictionIdentity{})
	r.predictionAccount.frames, r.predictionAccount.sender = nil, nil
	r.stopPredictionPreferenceWatchLocked()
}

func (r *codexBackendRuntime) queuePredictionCancelLocked(requestID string, frames chan<- []byte) {
	outbound, _ := r.translator.TranslateCommand(agentproto.Command{
		Kind:       agentproto.CommandThreadPredictionCancel,
		Prediction: agentproto.PredictionCommand{RequestCommandID: requestID},
	})
	for _, frame := range outbound {
		select {
		case frames <- frame:
		default:
		}
	}
}

func (r *codexBackendRuntime) cancelPredictionJobsLocked() {
	if r.predictions == nil {
		return
	}
	for id, job := range r.predictions.jobs {
		job.cancel()
		r.cancelNativePredictionLocked(job)
		delete(r.predictions.jobs, id)
	}
}

func (r *codexBackendRuntime) cancelNativePredictionLocked(job *predictionJob) {
	if !job.sampled {
		return
	}
	if job.sampleID != "" && !job.sampleSent {
		r.translator.DiscardUnsentPrediction(job.command.CommandID)
		return
	}
	r.queuePredictionCancelLocked(job.command.CommandID, job.frames)
}

func cancelRuntimePredictions(runtime backendRuntime) {
	if predictions, ok := runtime.(interface{ CancelPredictions() }); ok {
		predictions.CancelPredictions()
	}
}
