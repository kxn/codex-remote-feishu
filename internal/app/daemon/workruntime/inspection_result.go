package workruntime

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ObserveRuntimeInspection handles bound turns, precise pre-turn rejections and
// owner-only idle-carrier checks without fabricating a business attempt.
func ObserveRuntimeInspection(request InspectionRequest, result agentproto.RuntimeInspectionResult) (NativeObservation, error) {
	if result.Version != agentproto.RuntimeInspectionProtocolVersion || result.CarrierIncarnation != request.Carrier.Incarnation || result.Problem != nil {
		return NativeObservation{}, fmt.Errorf("runtime inspection incomplete or from another carrier")
	}
	if result.InstanceGeneration != request.Carrier.InstanceGeneration || result.ProcessGeneration != request.Carrier.ProcessGeneration || result.LocationGeneration != request.LocationGeneration {
		return NativeObservation{}, fmt.Errorf("runtime inspection carrier generations changed")
	}
	input := ProtocolInspection{Native: result.Native, Continuation: result.Continuation, StreamTerminal: result.StreamTerminal, Background: result.Background, ObservedAt: result.ObservedAt}
	if result.OwnerScope == nil && result.NativeRejection == nil {
		return ObserveProtocol(request, input)
	}
	observation := NativeObservation{CarrierIncarnation: request.Carrier.Incarnation, InstanceID: request.Carrier.OriginInstanceID, InstanceGeneration: request.Carrier.InstanceGeneration, ProcessGeneration: request.Carrier.ProcessGeneration, LocationGeneration: request.LocationGeneration, SessionID: request.SessionID, TurnID: request.TurnID, ObservedAt: result.ObservedAt}
	background := result.Background
	if background.CarrierIncarnation != request.Carrier.Incarnation || background.ProcessGeneration != request.Carrier.ProcessGeneration || background.LocationGeneration != request.LocationGeneration || result.ObservedAt.IsZero() {
		return observation, fmt.Errorf("inspection host generation mismatch")
	}
	observation.BackgroundEvidence, _ = json.Marshal(background)
	observation.BackgroundWorkStopped = background.Complete && background.NativeTaskSetComplete && len(background.RunningProcessIDs) == 0 && len(background.PendingTaskIDs) == 0 && len(background.ExternalResourceUncertainties) == 0 && validInspectionEvidence(background.Evidence)
	inputsStopped := background.NativeInputsComplete && background.PendingNativeInputs == 0 && len(background.ActiveNativeTurnIDs) == 0
	if result.NativeRejection != nil {
		event := result.NativeRejection
		if event.CommandID != request.SubjectCommandID || event.RequestID == "" || event.Journal == nil || event.Journal.CarrierIncarnation != request.Carrier.Incarnation || event.Status != "failed" {
			return observation, fmt.Errorf("native rejection lacks precise subject command identity")
		}
		hash, err := agentproto.RuntimeEventPayloadHash(*event)
		if err != nil || hash != event.Journal.PayloadHash {
			return observation, fmt.Errorf("native rejection hash mismatch")
		}
		switch event.TurnCompletionOrigin {
		case agentproto.TurnCompletionOriginThreadStartRejected, agentproto.TurnCompletionOriginThreadResumeRejected, agentproto.TurnCompletionOriginTurnStartRejected:
		default:
			return observation, fmt.Errorf("event does not prove a rejected native start")
		}
		if !validBootstrap(request.Carrier.Backend, result.Bootstrap) {
			return observation, fmt.Errorf("native readiness is unproven")
		}
		observation.NativeStopped = inputsStopped
		observation.ContinuationStopped = background.ContinuationInventoryKnown && len(background.ActiveGoalSessionIDs) == 0
		observation.NativeEvidence, _ = json.Marshal(event)
		observation.ContinuationEvidence, _ = json.Marshal(map[string]any{"native_start_rejected": true, "known_goal_sessions": background.ActiveGoalSessionIDs, "inventory_complete": background.ContinuationInventoryKnown})
		return observation, nil
	}
	scope := result.OwnerScope
	if request.Binding.AttemptID != "" || request.Binding.RunRef != "" || !scope.KnownSessionsComplete || !validBootstrap(request.Carrier.Backend, scope.Bootstrap) {
		return observation, fmt.Errorf("owner-only inspection lacks a complete initialized scope")
	}
	observation.NativeStopped = inputsStopped
	observation.ContinuationStopped = background.ContinuationInventoryKnown && len(background.ActiveGoalSessionIDs) == 0
	var nativeProbes []RPCProbe
	var goalProbes []RPCProbe
	var terminals []*agentproto.Event
	for _, session := range scope.Sessions {
		if session.SessionID == "" {
			return observation, fmt.Errorf("owner scope contains an unidentified native session")
		}
		if request.Carrier.Backend == work.BackendCodex {
			local := request
			local.SessionID = session.SessionID
			part, err := ObserveProtocol(local, ProtocolInspection{Native: session.Native, Continuation: session.Continuation, Background: background, ObservedAt: result.ObservedAt})
			if err != nil {
				return observation, err
			}
			observation.NativeStopped = observation.NativeStopped && part.NativeStopped
			observation.ContinuationStopped = observation.ContinuationStopped && part.ContinuationStopped
			observation.GoalActive = observation.GoalActive || part.GoalActive
			nativeProbes = append(nativeProbes, session.Native)
			goalProbes = append(goalProbes, session.Continuation)
		} else {
			if session.NoNativeInputObserved {
				continue
			}
			if session.StreamTerminal == nil {
				return observation, fmt.Errorf("Native stream owner scope lacks a journaled native boundary")
			}
			local := request
			local.SessionID = session.SessionID
			local.TurnID = session.StreamTerminal.TurnID
			part, err := ObserveProtocol(local, ProtocolInspection{StreamTerminal: session.StreamTerminal, Background: background, ObservedAt: result.ObservedAt})
			if err != nil {
				return observation, err
			}
			observation.NativeStopped = observation.NativeStopped && part.NativeStopped
			observation.ContinuationStopped = observation.ContinuationStopped && part.ContinuationStopped
			terminals = append(terminals, session.StreamTerminal)
		}
	}
	observation.NativeEvidence, _ = json.Marshal(map[string]any{"bootstrap": scope.Bootstrap, "native_read_probes": nativeProbes, "stream_terminals": terminals, "complete_session_inventory": true})
	observation.ContinuationEvidence, _ = json.Marshal(map[string]any{"goal_probes": goalProbes, "native_goal_capability": request.Carrier.Backend == work.BackendCodex, "task_inventory": background.PendingTaskIDs, "complete_goal_inventory": background.ContinuationInventoryKnown})
	return observation, nil
}

func validInspectionEvidence(raw json.RawMessage) bool {
	var record map[string]json.RawMessage
	return json.Unmarshal(raw, &record) == nil && len(record) > 0
}

func validBootstrap(backend work.Backend, probe RPCProbe) bool {
	if backend == work.BackendCodex || backend == work.BackendOpenCode {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		var response struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		return json.Unmarshal(probe.Request, &request) == nil && json.Unmarshal(probe.Response, &response) == nil && request.Method == "initialize" && len(request.ID) > 0 && string(request.ID) == string(response.ID) && len(response.Result) > 0 && (len(response.Error) == 0 || string(response.Error) == "null")
	}
	var request struct {
		Type    string `json:"type"`
		ID      string `json:"request_id"`
		Request struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	var response struct {
		Type     string `json:"type"`
		Response struct {
			ID      string `json:"request_id"`
			Subtype string `json:"subtype"`
		} `json:"response"`
	}
	return json.Unmarshal(probe.Request, &request) == nil && json.Unmarshal(probe.Response, &response) == nil && request.Type == "control_request" && request.ID != "" && request.Request.Subtype == "initialize" && response.Type == "control_response" && response.Response.ID == request.ID && response.Response.Subtype == "success"
}
