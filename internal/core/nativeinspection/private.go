package nativeinspection

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// VerifyPrivateOwnerStopped validates the frozen owner-wide native, goal, and
// managed-background observations. It does not prove OS exit or a write fence.
func VerifyPrivateOwnerStopped(owner agentproto.NativeOwner, r agentproto.RuntimeInspectionResult) error {
	return VerifyPrivateOwnerStoppedWithTerminalProof(owner, r, nil)
}

// The optional proof resolves unbound stream terminal events from the durable
// application journal using an accepted exact session and turn association.
func VerifyPrivateOwnerStoppedWithTerminalProof(owner agentproto.NativeOwner, r agentproto.RuntimeInspectionResult, proof func(agentproto.Event) bool) error {
	fail := func() error { return fmt.Errorf("private native quiescence evidence incomplete") }
	b := r.Background
	if owner.Validate() != nil || r.Version != agentproto.RuntimeInspectionProtocolVersion || r.CarrierIncarnation != owner.CarrierIncarnation || r.InstanceGeneration != owner.InstanceGeneration || r.ProcessGeneration != owner.ProcessGeneration || r.LocationGeneration != 0 || r.Problem != nil || r.ObservedAt.IsZero() || r.NativeWriteWatermark < 0 || r.OwnerScope == nil || !r.OwnerScope.KnownSessionsComplete || !ValidBootstrap(owner.Backend, r.OwnerScope.Bootstrap) {
		return fail()
	}
	if owner.Backend == agentproto.BackendOpenCode {
		var init struct {
			Result struct {
				AgentInfo struct{ Name, Version string } `json:"agentInfo"`
			} `json:"result"`
		}
		if json.Unmarshal(r.OwnerScope.Bootstrap.Response, &init) != nil || !strings.EqualFold(init.Result.AgentInfo.Name, "OpenCode") || !agentproto.SupportedPrivateOpenCodeVersion(init.Result.AgentInfo.Version) {
			return fail()
		}
	}
	var evidence map[string]json.RawMessage
	if b.CarrierIncarnation != owner.CarrierIncarnation || b.ProcessGeneration != owner.ProcessGeneration || b.LocationGeneration != 0 || !b.Complete || !b.NativeTaskSetComplete || !b.NativeInputsComplete || !b.ContinuationInventoryKnown || b.PendingNativeInputs != 0 || len(b.RunningProcessIDs) != 0 || len(b.PendingTaskIDs) != 0 || len(b.ActiveNativeTurnIDs) != 0 || len(b.ActiveGoalSessionIDs) != 0 || len(b.ExternalResourceUncertainties) != 0 || json.Unmarshal(b.Evidence, &evidence) != nil || len(evidence) == 0 {
		return fail()
	}
	seen := map[string]bool{}
	for _, session := range r.OwnerScope.Sessions {
		if session.SessionID == "" || seen[session.SessionID] {
			return fail()
		}
		seen[session.SessionID] = true
		switch owner.Backend {
		case agentproto.BackendCodex:
			raw, err := MatchRPC(session.Native, "thread/read", session.SessionID)
			if err != nil {
				return err
			}
			var native struct {
				Thread struct {
					ID     string `json:"id"`
					Status struct {
						Type string `json:"type"`
					} `json:"status"`
				} `json:"thread"`
			}
			if json.Unmarshal(raw, &native) != nil || native.Thread.ID != session.SessionID || native.Thread.Status.Type != "idle" {
				return fail()
			}
			raw, err = MatchRPC(session.Continuation, "thread/goal/get", session.SessionID)
			if err != nil {
				return err
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil {
				return fail()
			}
			goal, ok := fields["goal"]
			if !ok {
				return fail()
			}
			if string(goal) != "null" {
				var g struct {
					Status string `json:"status"`
				}
				if json.Unmarshal(goal, &g) != nil || (g.Status != "paused" && g.Status != "completed" && g.Status != "budget_exhausted") {
					return fail()
				}
			}
		case agentproto.BackendClaude, agentproto.BackendOpenCode:
			if session.NoNativeInputObserved {
				continue
			}
			e := session.StreamTerminal
			if e == nil || e.Kind != agentproto.EventTurnCompleted || e.TurnCompletionOrigin != agentproto.TurnCompletionOriginRuntime || e.ThreadID != session.SessionID || e.TurnID == "" || e.Journal == nil || e.Journal.CarrierIncarnation != owner.CarrierIncarnation || (e.NativeBinding == nil && (proof == nil || !proof(*e)) || e.NativeBinding != nil && e.NativeBinding.Owner != owner) || (e.Status != "completed" && e.Status != "failed" && e.Status != "interrupted" && !(owner.Backend == agentproto.BackendOpenCode && e.Status == "cancelled")) {
				return fail()
			}
			hash, err := agentproto.RuntimeEventPayloadHash(*e)
			if err != nil || hash != e.Journal.PayloadHash {
				return fail()
			}
		default:
			return fail()
		}
	}
	return nil
}
