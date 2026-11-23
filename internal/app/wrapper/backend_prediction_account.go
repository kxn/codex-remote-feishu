package wrapper

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const predictionAccountProbePrefix = "remote-prediction-account:"

type predictionAccountState struct {
	principal predictionIdentity
	probeID   string
	frames    chan<- []byte
}

func (r *codexBackendRuntime) rememberPredictionPrincipal(job *predictionJob, identity predictionIdentity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.predictionAccount.principal.UserID == "" && r.predictions.jobs[job.command.CommandID] == job && job.ctx.Err() == nil {
		r.predictionAccount.principal = identity
		r.predictionAccount.frames = job.frames
	}
}

func (r *codexBackendRuntime) observePredictionAccountLocked(line []byte) ([]agentproto.Event, bool) {
	if r.predictions == nil {
		return nil, false
	}
	var frame struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(line, &frame) != nil {
		return nil, false
	}
	state := &r.predictionAccount
	if frame.Method == "" && len(frame.ID) != 0 {
		var id string
		if json.Unmarshal(frame.ID, &id) == nil && strings.HasPrefix(id, predictionAccountProbePrefix) {
			if id != state.probeID {
				return nil, true
			}
			var response predictionRPCResponse
			_ = json.Unmarshal(line, &response)
			return r.finishPredictionAccountProbeLocked(response), true
		}
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
		r.invalidatePredictionAccountLocked(predictionIdentity{})
		return []agentproto.Event{{Kind: agentproto.EventThreadPredictionInvalidated}}, false
	}
	if state.principal.UserID == "" || (frame.Method != "account/updated" && !notice.Params.Success) {
		return nil, false
	}
	// The notification carries mode and plan only. Query the same native process for the authenticated principal.
	r.predictions.sequence++
	id := fmt.Sprintf("%s%d", predictionAccountProbePrefix, r.predictions.sequence)
	probe, _ := json.Marshal(map[string]any{"id": id, "method": "getAuthStatus", "params": map[string]bool{"includeToken": true, "refreshToken": false}})
	select {
	case state.frames <- append(probe, '\n'):
		state.probeID = id
		return nil, false
	default:
		// A full write queue must not stall native stdout while processing account notifications.
		return r.finishPredictionAccountProbeLocked(predictionRPCResponse{}), false
	}
}

func (r *codexBackendRuntime) finishPredictionAccountProbeLocked(response predictionRPCResponse) []agentproto.Event {
	state := &r.predictionAccount
	state.probeID = ""
	_, identity, err := decodePredictionAuthToken(response.Result)
	verified := err == nil && (len(response.Error) == 0 || string(response.Error) == "null")
	if verified && (state.principal.UserID != identity.UserID || state.principal.AccountID != identity.AccountID) {
		r.invalidatePredictionAccountLocked(identity)
		return []agentproto.Event{{Kind: agentproto.EventThreadPredictionInvalidated}}
	}
	var events []agentproto.Event
	for id, job := range r.predictions.jobs {
		if !verified {
			// An unresolved principal cannot authorize candidates from an in-flight query.
			events = append(events, agentproto.Event{Kind: agentproto.EventThreadPredictionResult, CommandID: id,
				ThreadID: job.command.Target.ThreadID, Prediction: &agentproto.PredictionResult{SourceTurnID: job.command.Prediction.SourceTurnID}})
			continue
		}
		if job.accountHeld != nil {
			job.cancel()
			delete(r.predictions.jobs, id)
			events = append(events, *job.accountHeld)
		}
	}
	if !verified {
		r.cancelPredictionJobsLocked()
	}
	return events
}

func (r *codexBackendRuntime) invalidatePredictionAccountLocked(identity predictionIdentity) {
	r.predictionAccount.principal = identity
	r.predictionAccount.probeID = ""
	r.cancelPredictionJobsLocked()
}
