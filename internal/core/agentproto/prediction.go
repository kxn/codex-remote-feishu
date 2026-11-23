package agentproto

type PredictionCommand struct {
	SourceTurnID     string `json:"sourceTurnId,omitempty"`
	InstanceID       string `json:"instanceId,omitempty"`
	RequestCommandID string `json:"requestCommandId,omitempty"`
}

type PredictionResult struct {
	SourceTurnID string `json:"sourceTurnId,omitempty"`
	Suggestion   string `json:"suggestion,omitempty"`
	Unsupported  bool   `json:"unsupported,omitempty"`
}
