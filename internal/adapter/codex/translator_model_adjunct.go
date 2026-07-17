package codex

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (t *Translator) observeModelRerouted(message map[string]any) Result {
	threadID := lookupString(message, "params", "threadId")
	turnID := lookupString(message, "params", "turnId")
	reroute := agentproto.NormalizeTurnModelReroute(&agentproto.TurnModelReroute{
		ThreadID:  threadID,
		TurnID:    turnID,
		FromModel: lookupString(message, "params", "fromModel"),
		ToModel:   lookupString(message, "params", "toModel"),
		Reason:    lookupString(message, "params", "reason"),
	})
	if reroute == nil || reroute.ThreadID == "" || reroute.TurnID == "" {
		return Result{}
	}
	return Result{Events: []agentproto.Event{{
		Kind:         agentproto.EventTurnModelRerouted,
		ThreadID:     reroute.ThreadID,
		TurnID:       reroute.TurnID,
		ModelReroute: reroute,
		TrafficClass: t.trafficClassForTurn(reroute.ThreadID, reroute.TurnID),
		Initiator:    t.initiatorForTurn(reroute.ThreadID, reroute.TurnID),
	}}}
}

func (t *Translator) observeModelVerification(message map[string]any) Result {
	verification := extractTurnModelVerification(message)
	if verification == nil || verification.ThreadID == "" || verification.TurnID == "" {
		return Result{}
	}
	return Result{Events: []agentproto.Event{{
		Kind:              agentproto.EventTurnModelVerification,
		ThreadID:          verification.ThreadID,
		TurnID:            verification.TurnID,
		ModelVerification: verification,
		TrafficClass:      t.trafficClassForTurn(verification.ThreadID, verification.TurnID),
		Initiator:         t.initiatorForTurn(verification.ThreadID, verification.TurnID),
	}}}
}

func (t *Translator) observeModelSafetyBuffering(message map[string]any) Result {
	buffering := extractTurnModelSafetyBuffering(message)
	if buffering == nil || buffering.ThreadID == "" || buffering.TurnID == "" {
		return Result{}
	}
	return Result{Events: []agentproto.Event{{
		Kind:                 agentproto.EventTurnModelSafetyBufferingUpdated,
		ThreadID:             buffering.ThreadID,
		TurnID:               buffering.TurnID,
		ModelSafetyBuffering: buffering,
		TrafficClass:         t.trafficClassForTurn(buffering.ThreadID, buffering.TurnID),
		Initiator:            t.initiatorForTurn(buffering.ThreadID, buffering.TurnID),
	}}}
}

func extractTurnModelVerification(message map[string]any) *agentproto.TurnModelVerification {
	params := lookupMap(message, "params")
	verification := &agentproto.TurnModelVerification{
		ThreadID: strings.TrimSpace(lookupStringFromAny(params["threadId"])),
		TurnID:   strings.TrimSpace(lookupStringFromAny(params["turnId"])),
	}
	for _, raw := range sliceAnyFromAny(params["verifications"]) {
		record, _ := raw.(map[string]any)
		if record == nil {
			continue
		}
		verification.Verifications = append(verification.Verifications, agentproto.ModelVerificationRecord{
			ID:      lookupStringFromAny(record["id"]),
			Type:    lookupStringFromAny(record["type"]),
			Message: firstNonEmptyString(lookupStringFromAny(record["message"]), lookupStringFromAny(record["description"])),
			Reason:  lookupStringFromAny(record["reason"]),
		})
	}
	return agentproto.NormalizeTurnModelVerification(verification)
}

func extractTurnModelSafetyBuffering(message map[string]any) *agentproto.TurnModelSafetyBuffering {
	params := lookupMap(message, "params")
	return agentproto.NormalizeTurnModelSafetyBuffering(&agentproto.TurnModelSafetyBuffering{
		ThreadID:        lookupStringFromAny(params["threadId"]),
		TurnID:          lookupStringFromAny(params["turnId"]),
		Model:           lookupStringFromAny(params["model"]),
		UseCases:        stringListFromAny(params["useCases"]),
		Reasons:         stringListFromAny(params["reasons"]),
		ShowBufferingUI: lookupBoolFromAny(params["showBufferingUi"]),
		FasterModel:     lookupStringFromAny(params["fasterModel"]),
	})
}

func stringListFromAny(raw any) []string {
	values := sliceAnyFromAny(raw)
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if item := strings.TrimSpace(lookupStringFromAny(value)); item != "" {
			result = append(result, item)
		}
	}
	return result
}
