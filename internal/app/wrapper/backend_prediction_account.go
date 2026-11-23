package wrapper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const (
	predictionAccountProbePrefix = "remote-prediction-account:"
	predictionPreparationPrefix  = "remote-prediction-prep:"
)

type predictionAccountState struct {
	principal     predictionIdentity
	epoch         uint64
	probeID       string
	probeDone     chan struct{}
	probeTimer    *time.Timer
	probeDeadline time.Time
	frames        chan<- []byte
	sender        relayEventSender
}

func samePredictionPrincipal(a, b predictionIdentity) bool {
	return a.UserID == b.UserID && a.AccountID == b.AccountID
}

// Bind a credential response in stdout observation order, before another
// account notification or the next preparation stage can overtake it.
func (r *codexBackendRuntime) observePredictionAuthReadLocked(pending *pendingPredictionRPC, response predictionRPCResponse) []agentproto.Event {
	job := pending.authJob
	if job == nil || r.predictions.jobs[job.command.CommandID] != job || job.ctx.Err() != nil {
		return nil
	}
	identity, verified := predictionResponsePrincipal(response)
	if !verified {
		return nil
	}
	state := &r.predictionAccount
	if !samePredictionPrincipal(state.principal, identity) && state.probeID == "" {
		if pending.authEpoch != state.epoch {
			// A credential read started before a newer account notification.
			return nil
		}
		if state.principal.UserID != "" {
			return r.invalidatePredictionAccountLocked(identity)
		}
		state.principal = identity
	}
	job.principal = identity
	return nil
}

// lockVerifiedPredictionAccount leaves the wrapper locked only on success.
func (r *codexBackendRuntime) lockVerifiedPredictionAccount(job *predictionJob) bool {
	for {
		r.mu.Lock()
		if r.predictions.jobs[job.command.CommandID] != job || job.ctx.Err() != nil {
			r.mu.Unlock()
			return false
		}
		state := &r.predictionAccount
		if state.probeDone == nil {
			if job.principal.UserID != "" && samePredictionPrincipal(state.principal, job.principal) {
				return true
			}
			r.mu.Unlock()
			return false
		}
		done := state.probeDone
		r.mu.Unlock()
		select {
		case <-done:
		case <-job.ctx.Done():
			return false
		}
	}
}

func (r *codexBackendRuntime) observePredictionAccountLocked(line []byte) ([]agentproto.Event, bool) {
	if r.predictions == nil {
		return nil, false
	}
	frame := predictionWriteFrame(line)
	state := &r.predictionAccount
	if frame.Method == "" && strings.HasPrefix(frame.ID, predictionAccountProbePrefix) {
		if frame.ID != state.probeID {
			return nil, true
		}
		var response predictionRPCResponse
		_ = json.Unmarshal(line, &response)
		return r.finishPredictionAccountProbeLocked(response), true
	}
	switch frame.Method {
	case "account/updated", "account/login/completed", "accountLoginCompleted":
		for _, job := range r.predictions.jobs {
			if job.authCancel != nil {
				job.authCancel()
			}
		}
	default:
		return nil, false
	}
	var notice struct {
		Params struct {
			AuthMode string `json:"authMode"`
			Success  bool   `json:"success"`
		} `json:"params"`
	}
	if json.Unmarshal(line, &notice) != nil {
		return nil, false
	}
	if frame.Method == "account/updated" && notice.Params.AuthMode != "chatgpt" && notice.Params.AuthMode != "chatgptAuthTokens" {
		return r.invalidatePredictionAccountLocked(predictionIdentity{}), false
	}
	if state.frames == nil || (frame.Method != "account/updated" && !notice.Params.Success) {
		return nil, false
	}

	// AccountUpdated lacks user and account IDs. Read the same native process
	// without refreshing its token or blocking stdout on a full write queue.
	state.epoch++
	deadline := state.probeDeadline
	if deadline.IsZero() {
		deadline = time.Now().Add(predictionResponseTimeout)
	}
	r.clearPredictionAccountProbeLocked()
	r.predictions.sequence++
	id := fmt.Sprintf("%s%d", predictionAccountProbePrefix, r.predictions.sequence)
	probe, _ := json.Marshal(map[string]any{"id": id, "method": "getAuthStatus", "params": map[string]bool{"includeToken": true, "refreshToken": false}})
	select {
	case state.frames <- append(probe, '\n'):
		state.probeID = id
		state.probeDone = make(chan struct{})
		state.probeDeadline = deadline
		state.probeTimer = time.AfterFunc(time.Until(deadline), func() { r.expirePredictionAccountProbe(id) })
		return nil, false
	default:
		return r.invalidatePredictionAccountLocked(predictionIdentity{}), false
	}
}

func predictionResponsePrincipal(response predictionRPCResponse) (predictionIdentity, bool) {
	_, identity, err := decodePredictionAuthToken(response.Result)
	return identity, err == nil && (len(response.Error) == 0 || string(response.Error) == "null")
}

func (r *codexBackendRuntime) finishPredictionAccountProbeLocked(response predictionRPCResponse) []agentproto.Event {
	state := &r.predictionAccount
	identity, verified := predictionResponsePrincipal(response)
	if !verified {
		return r.invalidatePredictionAccountLocked(predictionIdentity{})
	}
	if state.principal.UserID != "" && !samePredictionPrincipal(state.principal, identity) {
		return r.invalidatePredictionAccountLocked(identity)
	}
	state.principal = identity
	r.clearPredictionAccountProbeLocked()
	var events []agentproto.Event
	for id, job := range r.predictions.jobs {
		if job.accountHeld != nil {
			if !r.predictionResultAllowedLocked(job, *job.accountHeld) {
				return []agentproto.Event{r.invalidatePredictionPreferenceLocked()}
			}
			job.cancel()
			delete(r.predictions.jobs, id)
			events = append(events, *job.accountHeld)
		} else if job.delayedSample != nil {
			select {
			case job.frames <- job.delayedSample:
				job.delayedSample = nil
			default:
				job.cancel()
				delete(r.predictions.jobs, id)
				r.cancelNativePredictionLocked(job)
				events = append(events, agentproto.Event{Kind: agentproto.EventThreadPredictionResult, CommandID: id,
					ThreadID: job.command.Target.ThreadID, Prediction: &agentproto.PredictionResult{SourceTurnID: job.command.Prediction.SourceTurnID}})
			}
		}
	}
	return events
}

func (r *codexBackendRuntime) expirePredictionAccountProbe(id string) {
	r.mu.Lock()
	if r.predictionAccount.probeID != id {
		r.mu.Unlock()
		return
	}
	sender := r.predictionAccount.sender
	events := r.invalidatePredictionAccountLocked(predictionIdentity{})
	r.mu.Unlock()
	if sender != nil {
		_ = sender.SendEvents(events)
	}
}

func (r *codexBackendRuntime) invalidatePredictionAccountLocked(identity predictionIdentity) []agentproto.Event {
	r.predictionAccount.principal = identity
	r.predictionAccount.epoch++
	r.clearPredictionAccountProbeLocked()
	r.cancelPredictionJobsLocked()
	r.stopPredictionPreferenceWatchLocked()
	return []agentproto.Event{{Kind: agentproto.EventThreadPredictionInvalidated}}
}

func (r *codexBackendRuntime) clearPredictionAccountProbeLocked() {
	state := &r.predictionAccount
	if state.probeTimer != nil {
		state.probeTimer.Stop()
	}
	if state.probeDone != nil {
		close(state.probeDone)
	}
	state.probeID, state.probeDone, state.probeTimer = "", nil, nil
	state.probeDeadline = time.Time{}
}

type predictionNativeFrame struct {
	ID     string `json:"id"`
	Method string `json:"method"`
}

func predictionWriteFrame(raw []byte) predictionNativeFrame {
	var frame predictionNativeFrame
	_ = json.Unmarshal(raw, &frame)
	return frame
}

// Recheck ownership at dequeue. A shared child write queue can outlive the
// prediction account scope or even the child generation that queued its frame.
func (r *codexBackendRuntime) allowPredictionWrite(raw []byte) bool {
	if !bytes.Contains(raw, []byte(`"`+predictionAccountProbePrefix)) &&
		!bytes.Contains(raw, []byte(`"`+predictionPreparationPrefix)) &&
		!bytes.Contains(raw, []byte(`"`+codex.PredictionRPCIDPrefix)) {
		return true
	}
	frame := predictionWriteFrame(raw)
	if frame.Method == "" {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case frame.Method == "getAuthStatus" && strings.HasPrefix(frame.ID, predictionAccountProbePrefix):
		return r.predictionAccount.probeID == frame.ID
	case strings.HasPrefix(frame.ID, predictionPreparationPrefix):
		if r.predictions == nil {
			return false
		}
		pending := r.predictions.pending[frame.ID]
		if pending != nil && pending.ctx.Err() == nil {
			return true
		}
		delete(r.predictions.pending, frame.ID)
		return false
	case frame.Method == "thread/sample" && strings.HasPrefix(frame.ID, codex.PredictionRPCIDPrefix):
		if r.predictions == nil {
			return false
		}
		for _, job := range r.predictions.jobs {
			if job.sampleID != frame.ID || job.ctx.Err() != nil || job.sampleSent {
				continue
			}
			if r.predictionAccount.probeDone != nil {
				job.delayedSample = raw
				return false
			}
			if samePredictionPrincipal(r.predictionAccount.principal, job.principal) {
				job.sampleSent = true
				return true
			}
		}
		return false
	default:
		return true
	}
}
