package workruntime

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type RPCProbe = agentproto.RPCProbe

type BackgroundInspection = agentproto.BackgroundInspection

type ProtocolInspection struct {
	Native       RPCProbe
	Continuation RPCProbe
	// Stream backends have no read RPC. Their exact journaled runtime terminal plus the
	// host's empty native input/task set proves the stream reached its boundary.
	StreamTerminal *agentproto.Event
	Background     BackgroundInspection
	ObservedAt     time.Time
}

// ObserveProtocol constructs facts from explicit protocol fields and host
// inspection. The coordinator still checks current generations before release.
func ObserveProtocol(request InspectionRequest, input ProtocolInspection) (NativeObservation, error) {
	observation := NativeObservation{CarrierIncarnation: request.Carrier.Incarnation, InstanceID: request.Carrier.OriginInstanceID, InstanceGeneration: request.Carrier.InstanceGeneration, ProcessGeneration: request.Carrier.ProcessGeneration, LocationGeneration: request.LocationGeneration, SessionID: request.SessionID, TurnID: request.TurnID, ObservedAt: input.ObservedAt}
	background := input.Background
	if background.CarrierIncarnation != request.Carrier.Incarnation || background.ProcessGeneration != request.Carrier.ProcessGeneration || background.LocationGeneration != request.LocationGeneration || input.ObservedAt.IsZero() {
		return observation, fmt.Errorf("host inspection generation mismatch")
	}
	observation.BackgroundEvidence, _ = json.Marshal(background)
	observation.BackgroundWorkStopped = background.Complete && background.NativeTaskSetComplete && len(background.RunningProcessIDs) == 0 && len(background.PendingTaskIDs) == 0 && validInspectionEvidence(background.Evidence) && len(background.ExternalResourceUncertainties) == 0
	switch request.Carrier.Backend {
	case work.BackendCodex:
		result, err := matchRPC(input.Native, "thread/read", request.SessionID)
		if err != nil {
			return observation, err
		}
		var native struct {
			Thread struct {
				ID     string `json:"id"`
				Status struct {
					Type string `json:"type"`
				} `json:"status"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(result, &native); err != nil {
			return observation, err
		}
		observation.NativeStopped = native.Thread.ID == request.SessionID && native.Thread.Status.Type == "idle" && background.NativeInputsComplete && background.PendingNativeInputs == 0 && len(background.ActiveNativeTurnIDs) == 0
		observation.NativeEvidence, _ = json.Marshal(input.Native)
		result, err = matchRPC(input.Continuation, "thread/goal/get", request.SessionID)
		if err != nil {
			return observation, err
		}
		var continuation struct {
			Goal *struct {
				Status    string `json:"status"`
				UpdatedAt int64  `json:"updatedAt"`
			} `json:"goal"`
		}
		if err := json.Unmarshal(result, &continuation); err != nil {
			return observation, err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(result, &fields); err != nil {
			return observation, err
		}
		if _, exists := fields["goal"]; !exists {
			return observation, fmt.Errorf("goal probe lacks an explicit goal field")
		}
		if continuation.Goal == nil {
			observation.ContinuationStopped = true
		} else {
			observation.GoalVersion = continuation.Goal.UpdatedAt
			switch continuation.Goal.Status {
			case "active":
				observation.GoalActive = true
			case "paused", "completed", "budget_exhausted":
				observation.ContinuationStopped = true
			}
		}
		observation.ContinuationEvidence, _ = json.Marshal(input.Continuation)
	case work.BackendClaude, work.BackendOpenCode:
		terminal := input.StreamTerminal
		if terminal == nil || terminal.Kind != agentproto.EventTurnCompleted || terminal.TurnCompletionOrigin != agentproto.TurnCompletionOriginRuntime || terminal.ThreadID != request.SessionID || terminal.TurnID != request.TurnID || terminal.Journal == nil {
			return observation, fmt.Errorf("native stream needs an exact journaled runtime result boundary")
		}
		hash, err := agentproto.RuntimeEventPayloadHash(*terminal)
		if err != nil || hash != terminal.Journal.PayloadHash || terminal.Journal.CarrierIncarnation != request.Carrier.Incarnation {
			return observation, fmt.Errorf("Native stream result identity mismatch")
		}
		if terminal.Execution != nil {
			ownerOnly := request.Binding.AttemptID == "" && request.Binding.RunRef == ""
			if (ownerOnly && !request.Binding.SameOwner(*terminal.Execution)) || (!ownerOnly && *terminal.Execution != request.Binding) {
				return observation, fmt.Errorf("Native stream terminal execution mismatch")
			}
		}
		observation.NativeStopped = (terminal.Status == "completed" || terminal.Status == "failed" || terminal.Status == "interrupted") && background.NativeInputsComplete && background.PendingNativeInputs == 0 && len(background.ActiveNativeTurnIDs) == 0
		observation.NativeEvidence, _ = json.Marshal(terminal)
		observation.ContinuationStopped = observation.NativeStopped && background.NativeTaskSetComplete && len(background.PendingTaskIDs) == 0
		observation.ContinuationEvidence, _ = json.Marshal(map[string]any{"protocol": string(request.Carrier.Backend) + ".runtime-terminal", "native_goal_capability": false, "pending_native_inputs": background.PendingNativeInputs, "pending_background_tasks": background.PendingTaskIDs})
	default:
		return observation, fmt.Errorf("unsupported native inspection backend")
	}
	if len(background.ActiveGoalSessionIDs) > 0 {
		observation.GoalActive = true
	}
	observation.ContinuationStopped = observation.ContinuationStopped && background.ContinuationInventoryKnown && len(background.ActiveGoalSessionIDs) == 0
	return observation, nil
}

func matchRPC(probe RPCProbe, method, sessionID string) (json.RawMessage, error) {
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ThreadID string `json:"threadId"`
		} `json:"params"`
	}
	var response struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal(probe.Request, &request) != nil || json.Unmarshal(probe.Response, &response) != nil || len(request.ID) == 0 || string(request.ID) != string(response.ID) || request.Method != method || request.Params.ThreadID != sessionID || (len(response.Error) > 0 && string(response.Error) != "null") || len(response.Result) == 0 {
		return nil, fmt.Errorf("native probe request/response correlation failed")
	}
	return response.Result, nil
}
