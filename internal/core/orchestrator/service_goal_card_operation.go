package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func goalUserFingerprint(goal *agentproto.ThreadGoalUpdate) goalFingerprint {
	fingerprint := goalFingerprint{GoalMissing: goal == nil || goal.Cleared}
	if goal != nil && !goal.Cleared {
		fingerprint.CreatedAt = goal.CreatedAt
		fingerprint.Objective = strings.TrimSpace(goal.Objective)
		if goal.TokenBudget != nil {
			value := *goal.TokenBudget
			fingerprint.TokenBudget = &value
		}
	}
	return fingerprint
}

func goalCardOperationKey(surface *state.SurfaceConsoleRecord, sub, operationID string) string {
	return "card|" + surface.SurfaceSessionID + "|" + surface.AttachedInstanceID + "|" + surface.SelectedThreadID + "|" + sub + "|" + operationID
}

func (s *Service) goalCardOperationAllows(surface *state.SurfaceConsoleRecord, action control.Action, sub string) bool {
	operationID := strings.TrimSpace(action.GoalOperationID)
	if operationID == "" {
		return false
	}
	key := goalCardOperationKey(surface, sub, operationID)
	pending, ok := s.pendingGoalFingerprints[key]
	if !ok {
		return false
	}
	delete(s.pendingGoalFingerprints, key)
	if s.now().Sub(pending.CapturedAt) > goalPendingFingerprintTTL {
		return false
	}
	goal := s.surfaceGoalForPage(surface)
	if !goalUserFingerprintMatches(pending.Fingerprint, goal) {
		return false
	}
	if goal == nil || goal.Cleared {
		return true
	}
	if pending.Fingerprint.TokenBudget == nil && goal.TokenBudget != nil {
		return false
	}
	return pending.Fingerprint.CreatedAt == goal.CreatedAt && pending.Status == goal.Status
}

func (s *Service) goalCommandButton(surface *state.SurfaceConsoleRecord, label, commandText string) control.CommandCatalogButton {
	return control.CommandCatalogButton{
		Label:       label,
		Kind:        control.CommandCatalogButtonCallbackAction,
		CommandText: commandText,
		CommandID:   control.FeishuCommandGoal,
		CallbackValue: s.goalScopedPayload(surface, commandText, frontstagecontract.ActionPayloadPageAction(
			string(control.ActionGoalCommand), control.FeishuActionArgumentText(commandText))),
	}
}

func (s *Service) goalScopedPayload(surface *state.SurfaceConsoleRecord, commandText string, payload map[string]any) map[string]any {
	if surface != nil {
		payload[frontstagecontract.CardActionPayloadKeyThreadID] = strings.TrimSpace(surface.SelectedThreadID)
		payload[frontstagecontract.CardActionPayloadKeyInstanceID] = strings.TrimSpace(surface.AttachedInstanceID)
		if parts := strings.Fields(commandText); len(parts) > 1 {
			operationID := s.nextAgentCommandID()
			payload[frontstagecontract.CardActionPayloadKeyGoalOperationID] = operationID
			if s.pendingGoalFingerprints == nil {
				s.pendingGoalFingerprints = map[string]pendingGoalFingerprint{}
			}
			for key, pending := range s.pendingGoalFingerprints {
				if strings.HasPrefix(key, "card|") && s.now().Sub(pending.CapturedAt) > goalPendingFingerprintTTL {
					delete(s.pendingGoalFingerprints, key)
				}
			}
			goal := s.surfaceGoalForPage(surface)
			pending := pendingGoalFingerprint{Fingerprint: goalUserFingerprint(goal), CapturedAt: s.now()}
			if goal != nil {
				pending.Status = goal.Status
			}
			s.pendingGoalFingerprints[goalCardOperationKey(surface, parts[1], operationID)] = pending
		}
	}
	if resolved, ok := control.ResolveFeishuTextCommand(control.CatalogContext{Backend: agentproto.BackendCodex}, commandText); ok {
		payload = frontstagecontract.ActionPayloadWithCatalog(payload, resolved.FamilyID, resolved.VariantID, string(resolved.Backend))
	}
	return payload
}
