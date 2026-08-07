package orchestrator

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const pendingTextInputTTL = 2 * time.Minute

// storePendingTextInput saves a user text message for later replay when the
// blocking condition is resolved (e.g., user selects a thread from the picker).
func (s *Service) storePendingTextInput(surface *state.SurfaceConsoleRecord, text string, inputs []agentproto.Input, sourceMessageID, actorUserID, replyToMessageID string, stagedMessageIDs []string) {
	if surface == nil {
		return
	}
	surface.PendingTextInput = &state.PendingTextInputRecord{
		Text:             text,
		Inputs:           cloneInputs(inputs),
		SourceMessageID:  sourceMessageID,
		ActorUserID:      actorUserID,
		ReplyToMessageID: replyToMessageID,
		StagedMessageIDs: append([]string(nil), stagedMessageIDs...),
		CreatedAt:        s.now(),
		ExpiresAt:        s.now().Add(pendingTextInputTTL),
	}
}

// takePendingTextInput returns and clears the saved pending text input if it
// exists and has not expired. Returns nil if nothing is pending or expired.
func (s *Service) takePendingTextInput(surface *state.SurfaceConsoleRecord) *state.PendingTextInputRecord {
	if surface == nil || surface.PendingTextInput == nil {
		return nil
	}
	pending := surface.PendingTextInput
	surface.PendingTextInput = nil
	if !pending.ExpiresAt.IsZero() && s.now().After(pending.ExpiresAt) {
		return nil
	}
	return pending
}

// clearPendingTextInput discards any saved pending text input.
func (s *Service) clearPendingTextInput(surface *state.SurfaceConsoleRecord) {
	if surface != nil {
		surface.PendingTextInput = nil
	}
}

// hasPendingTextInput reports whether the surface has a non-expired pending
// text input.
func (s *Service) hasPendingTextInput(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil || surface.PendingTextInput == nil {
		return false
	}
	if !surface.PendingTextInput.ExpiresAt.IsZero() && s.now().After(surface.PendingTextInput.ExpiresAt) {
		return false
	}
	return true
}

func cloneInputs(inputs []agentproto.Input) []agentproto.Input {
	if len(inputs) == 0 {
		return nil
	}
	cloned := make([]agentproto.Input, len(inputs))
	copy(cloned, inputs)
	return cloned
}
