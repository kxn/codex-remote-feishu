package work

import (
	"fmt"
	"time"
)

type ActorKind string

const (
	ActorPrincipal ActorKind = "principal"
	ActorRole      ActorKind = "role"
	ActorSystem    ActorKind = "system"
)

type Actor struct {
	Kind ActorKind `json:"kind"`
	ID   string    `json:"id"`
}

func (v Actor) Validate() error {
	if !oneOf(v.Kind, ActorPrincipal, ActorRole, ActorSystem) {
		return fmt.Errorf("invalid actor kind")
	}
	return required(v.ID)
}

type MessageKey struct {
	Tenant    string `json:"tenant"`
	ChatID    string `json:"chat_id"`
	MessageID string `json:"message_id"`
}

func (v MessageKey) Validate() error { return required(v.Tenant, v.ChatID, v.MessageID) }

type Observation struct {
	AppID              string    `json:"app_id"`
	IdentityGeneration int64     `json:"identity_generation"`
	ReceivedAt         time.Time `json:"received_at"`
}
type Message struct {
	RecalledEvidenceRef string              `json:"recalled_evidence_ref,omitempty"`
	Attachments         []MessageAttachment `json:"attachments,omitempty"`
	DerivedFrom         *SourceRef          `json:"derived_from,omitempty"`
	ID                  string              `json:"id"`
	ScopeID             string              `json:"scope_id"`
	ConversationID      string              `json:"conversation_id"`
	Source              Actor               `json:"source"`
	PlatformKey         *MessageKey         `json:"platform_key,omitempty"`
	BodyRef             string              `json:"body_ref"`
	Observations        []Observation       `json:"observations"`
	Revision            int64               `json:"revision"`
	CreatedAt           time.Time           `json:"created_at"`
}

func (v Message) Validate() error {
	for _, attachment := range v.Attachments {
		if err := attachment.Validate(); err != nil {
			return err
		}
	}
	if err := required(v.ID, v.ScopeID, v.ConversationID, v.BodyRef); err != nil {
		return err
	}
	if err := v.Source.Validate(); err != nil {
		return err
	}
	if v.DerivedFrom != nil {
		if err := v.DerivedFrom.Validate(); err != nil {
			return err
		}
		if v.PlatformKey != nil {
			return fmt.Errorf("derived message cannot own original platform key")
		}
	}
	if v.PlatformKey != nil {
		if err := v.PlatformKey.Validate(); err != nil {
			return err
		}
	}
	for _, o := range v.Observations {
		if err := required(o.AppID); err != nil {
			return err
		}
		if err := positive("observation identity generation", o.IdentityGeneration); err != nil {
			return err
		}
	}
	return positive("message revision", v.Revision)
}

type InboxState string

const (
	InboxPending   InboxState = "pending"
	InboxClaimed   InboxState = "claimed"
	InboxProcessed InboxState = "processed"
	InboxCancelled InboxState = "cancelled"
)

type Inbox struct {
	DirectedReplyIngressID     string            `json:"directed_reply_ingress_id,omitempty"`
	DirectedReplyBotGeneration int64             `json:"directed_reply_bot_generation,omitempty"`
	AutomationEpisodeID        string            `json:"automation_episode_id,omitempty"`
	SessionGoalIntentID        string            `json:"session_goal_intent_id,omitempty"`
	ControlOfInputID           string            `json:"control_of_input_id,omitempty"`
	RelatedTaskID              string            `json:"related_task_id,omitempty"`
	ContentState               InputContentState `json:"content_state,omitempty"`
	TaskID                     string            `json:"task_id,omitempty"`
	ID                         string            `json:"id"`
	ScopeID                    string            `json:"scope_id"`
	RecipientContextID         string            `json:"recipient_context_id,omitempty"`
	RecipientRoleID            string            `json:"recipient_role_id"`
	EventID                    string            `json:"event_id"`
	MessageID                  string            `json:"message_id,omitempty"`
	State                      InboxState        `json:"state"`
	ClaimedAttemptID           string            `json:"claimed_attempt_id,omitempty"`
	ReadAt                     *time.Time        `json:"read_at,omitempty"`
	ProcessedAt                *time.Time        `json:"processed_at,omitempty"`
	ProcessingOutcomeRef       string            `json:"processing_outcome_ref,omitempty"`
	AvailableAt                time.Time         `json:"available_at"`
	Revision                   int64             `json:"revision"`
}

func (v Inbox) Validate() error {
	if (v.DirectedReplyIngressID == "") != (v.DirectedReplyBotGeneration == 0) || v.DirectedReplyBotGeneration < 0 ||
		(v.DirectedReplyIngressID != "" && (v.MessageID == "" || v.ControlOfInputID != "" || v.AutomationEpisodeID != "" || v.SessionGoalIntentID != "")) {
		return fmt.Errorf("invalid directed human reply evidence")
	}
	if !oneOf(v.ContentState, InputContentNone, InputContentPending, InputContentReady, InputContentPartial) {
		return fmt.Errorf("invalid input content state")
	}
	if err := required(v.ID, v.ScopeID, v.RecipientRoleID, v.EventID); err != nil {
		return err
	}
	if err := ValidateInboxTransition(v.State, v.State); err != nil {
		return err
	}
	if err := positive("inbox revision", v.Revision); err != nil {
		return err
	}
	if oneOf(v.State, InboxClaimed, InboxProcessed) && v.ClaimedAttemptID == "" {
		return fmt.Errorf("claimed input needs exact attempt")
	}
	if v.State == InboxProcessed && (v.ProcessedAt == nil || v.ProcessingOutcomeRef == "") {
		return fmt.Errorf("processed input requires timestamp and durable outcome")
	}
	if v.State != InboxProcessed && (v.ProcessedAt != nil || v.ProcessingOutcomeRef != "") {
		return fmt.Errorf("unprocessed input cannot carry processing receipt")
	}
	if oneOf(v.State, InboxPending, InboxCancelled) && v.ClaimedAttemptID != "" {
		return fmt.Errorf("unclaimed input cannot carry attempt")
	}
	return nil
}

// ValidateInputConsumption prevents a result from clearing inputs that arrived
// after the attempt started or belonged to a different attempt.
func ValidateInputConsumption(attempt ExecutionAttempt, inputs []Inbox, processedIDs []string) error {
	admitted := map[string]bool{}
	for _, id := range attempt.InputIDs {
		admitted[id] = true
	}
	byID := map[string]Inbox{}
	for _, input := range inputs {
		if _, ok := byID[input.ID]; ok {
			return fmt.Errorf("duplicate input")
		}
		byID[input.ID] = input
	}
	seen := map[string]bool{}
	for _, id := range processedIDs {
		if seen[id] {
			return fmt.Errorf("duplicate processed input")
		}
		seen[id] = true
		input, ok := byID[id]
		if !ok || !admitted[id] {
			return fmt.Errorf("input %q was not admitted", id)
		}
		if input.State != InboxClaimed || input.ClaimedAttemptID != attempt.ID || input.ScopeID != attempt.ScopeID || input.RecipientContextID != "" && input.RecipientContextID != attempt.ContextID {
			return fmt.Errorf("input %q claim mismatch", id)
		}
	}
	return nil
}
