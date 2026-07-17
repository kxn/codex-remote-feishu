package agentproto

import "strings"

type ModelVerificationRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type TurnModelVerification struct {
	ThreadID      string                    `json:"threadId,omitempty"`
	TurnID        string                    `json:"turnId,omitempty"`
	Verifications []ModelVerificationRecord `json:"verifications,omitempty"`
}

type TurnModelSafetyBuffering struct {
	ThreadID        string   `json:"threadId,omitempty"`
	TurnID          string   `json:"turnId,omitempty"`
	Model           string   `json:"model,omitempty"`
	UseCases        []string `json:"useCases,omitempty"`
	Reasons         []string `json:"reasons,omitempty"`
	ShowBufferingUI bool     `json:"showBufferingUi,omitempty"`
	FasterModel     string   `json:"fasterModel,omitempty"`
}

func NormalizeTurnModelVerification(verification *TurnModelVerification) *TurnModelVerification {
	if verification == nil {
		return nil
	}
	normalized := &TurnModelVerification{
		ThreadID: strings.TrimSpace(verification.ThreadID),
		TurnID:   strings.TrimSpace(verification.TurnID),
	}
	for _, record := range verification.Verifications {
		item := ModelVerificationRecord{
			ID:      strings.TrimSpace(record.ID),
			Type:    strings.TrimSpace(record.Type),
			Message: strings.TrimSpace(record.Message),
			Reason:  strings.TrimSpace(record.Reason),
		}
		if item.ID == "" && item.Type == "" && item.Message == "" && item.Reason == "" {
			continue
		}
		normalized.Verifications = append(normalized.Verifications, item)
	}
	if normalized.ThreadID == "" && normalized.TurnID == "" && len(normalized.Verifications) == 0 {
		return nil
	}
	return normalized
}

func CloneTurnModelVerification(verification *TurnModelVerification) *TurnModelVerification {
	if verification == nil {
		return nil
	}
	cloned := *verification
	cloned.Verifications = append([]ModelVerificationRecord(nil), verification.Verifications...)
	return &cloned
}

func NormalizeTurnModelSafetyBuffering(buffering *TurnModelSafetyBuffering) *TurnModelSafetyBuffering {
	if buffering == nil {
		return nil
	}
	normalized := &TurnModelSafetyBuffering{
		ThreadID:        strings.TrimSpace(buffering.ThreadID),
		TurnID:          strings.TrimSpace(buffering.TurnID),
		Model:           strings.TrimSpace(buffering.Model),
		ShowBufferingUI: buffering.ShowBufferingUI,
		FasterModel:     strings.TrimSpace(buffering.FasterModel),
	}
	normalized.UseCases = normalizeStringList(buffering.UseCases)
	normalized.Reasons = normalizeStringList(buffering.Reasons)
	if normalized.ThreadID == "" && normalized.TurnID == "" && normalized.Model == "" &&
		len(normalized.UseCases) == 0 && len(normalized.Reasons) == 0 &&
		!normalized.ShowBufferingUI && normalized.FasterModel == "" {
		return nil
	}
	return normalized
}

func CloneTurnModelSafetyBuffering(buffering *TurnModelSafetyBuffering) *TurnModelSafetyBuffering {
	if buffering == nil {
		return nil
	}
	cloned := *buffering
	cloned.UseCases = append([]string(nil), buffering.UseCases...)
	cloned.Reasons = append([]string(nil), buffering.Reasons...)
	return &cloned
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if item := strings.TrimSpace(value); item != "" {
			normalized = append(normalized, item)
		}
	}
	return normalized
}
