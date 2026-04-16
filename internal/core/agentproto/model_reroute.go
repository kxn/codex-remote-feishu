package agentproto

import "strings"

type TurnModelReroute struct {
	ThreadID  string `json:"threadId,omitempty"`
	TurnID    string `json:"turnId,omitempty"`
	FromModel string `json:"fromModel,omitempty"`
	ToModel   string `json:"toModel,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

func NormalizeTurnModelReroute(reroute *TurnModelReroute) *TurnModelReroute {
	if reroute == nil {
		return nil
	}
	normalized := &TurnModelReroute{
		ThreadID:  strings.TrimSpace(reroute.ThreadID),
		TurnID:    strings.TrimSpace(reroute.TurnID),
		FromModel: strings.TrimSpace(reroute.FromModel),
		ToModel:   strings.TrimSpace(reroute.ToModel),
		Reason:    strings.TrimSpace(reroute.Reason),
	}
	if normalized.ThreadID == "" && normalized.TurnID == "" &&
		normalized.FromModel == "" && normalized.ToModel == "" && normalized.Reason == "" {
		return nil
	}
	return normalized
}

func CloneTurnModelReroute(reroute *TurnModelReroute) *TurnModelReroute {
	if reroute == nil {
		return nil
	}
	cloned := *reroute
	return &cloned
}
