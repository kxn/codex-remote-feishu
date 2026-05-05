package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type terminalCause string

const (
	terminalCauseCompleted            terminalCause = "completed"
	terminalCauseUserInterrupted      terminalCause = "user_interrupted"
	terminalCauseAutoContinueEligible terminalCause = "autocontinue_eligible_failure"
	terminalCauseStartupFailed        terminalCause = "startup_failed"
	terminalCauseNonRetryableFailure  terminalCause = "nonretryable_failure"
	terminalCauseTransportLost        terminalCause = "transport_lost"
)

type remoteTurnOutcome struct {
	Binding            *remoteTurnBinding
	Surface            *state.SurfaceConsoleRecord
	Item               *state.QueueItemRecord
	InstanceID         string
	ThreadID           string
	TurnID             string
	Status             string
	ErrorMessage       string
	Problem            *agentproto.ErrorInfo
	FinalText          string
	Summary            *control.FileChangeSummary
	CompletionOrigin   agentproto.TurnCompletionOrigin
	StartAccepted      bool
	InterruptRequested bool
	AnyOutputSeen      bool
	Cause              terminalCause
}

func remoteTurnCompletionOrigin(event agentproto.Event) agentproto.TurnCompletionOrigin {
	if event.Kind != agentproto.EventTurnCompleted {
		return ""
	}
	if event.TurnCompletionOrigin != "" {
		return event.TurnCompletionOrigin
	}
	return agentproto.TurnCompletionOriginRuntime
}

func remoteTurnStartAccepted(origin agentproto.TurnCompletionOrigin) bool {
	switch origin {
	case agentproto.TurnCompletionOriginTurnStartRejected,
		agentproto.TurnCompletionOriginThreadStartRejected,
		agentproto.TurnCompletionOriginThreadResumeRejected:
		return false
	default:
		return true
	}
}

func isTransportLostProblem(problem *agentproto.ErrorInfo) bool {
	if problem == nil {
		return false
	}
	switch strings.TrimSpace(problem.Layer) {
	case "relay", "wrapper", "daemon", "instance":
		return true
	default:
		return false
	}
}

func classifyRemoteTurnTerminalCause(outcome *remoteTurnOutcome) terminalCause {
	if outcome == nil {
		return terminalCauseNonRetryableFailure
	}
	if strings.TrimSpace(outcome.Status) == "completed" {
		return terminalCauseCompleted
	}
	if outcome.InterruptRequested {
		return terminalCauseUserInterrupted
	}
	if !outcome.StartAccepted {
		return terminalCauseStartupFailed
	}
	if isAutoContinueEligibleProblem(outcome.Problem) {
		return terminalCauseAutoContinueEligible
	}
	if isTransportLostProblem(outcome.Problem) {
		return terminalCauseTransportLost
	}
	return terminalCauseNonRetryableFailure
}

func (s *Service) deriveRemoteTurnOutcome(instanceID string, event agentproto.Event, finalText string, summary *control.FileChangeSummary) *remoteTurnOutcome {
	binding := s.lookupRemoteTurn(instanceID, event.ThreadID, event.TurnID)
	if binding == nil {
		return nil
	}
	if threadID := strings.TrimSpace(event.ThreadID); threadID != "" {
		binding.ThreadID = threadID
		binding.DurableThreadReady = true
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil || surface.ActiveQueueItemID == "" {
		s.clearRemoteTurn(instanceID, event.TurnID)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil {
		s.clearRemoteTurn(instanceID, event.TurnID)
		return nil
	}
	if threadID := strings.TrimSpace(event.ThreadID); threadID != "" {
		if inst := s.root.Instances[instanceID]; inst != nil {
			s.materializeRemoteTurnThread(inst, threadID, event.CWD, binding, item)
		}
	}
	outcome := &remoteTurnOutcome{
		Binding:            binding,
		Surface:            surface,
		Item:               item,
		InstanceID:         instanceID,
		ThreadID:           event.ThreadID,
		TurnID:             event.TurnID,
		Status:             event.Status,
		ErrorMessage:       strings.TrimSpace(event.ErrorMessage),
		Problem:            cloneProblem(event.Problem),
		FinalText:          strings.TrimSpace(finalText),
		Summary:            summary,
		CompletionOrigin:   remoteTurnCompletionOrigin(event),
		StartAccepted:      remoteTurnStartAccepted(remoteTurnCompletionOrigin(event)),
		InterruptRequested: binding.InterruptRequested,
		AnyOutputSeen:      binding.AnyOutputSeen,
	}
	outcome.Cause = classifyRemoteTurnTerminalCause(outcome)
	return outcome
}

func cloneProblem(problem *agentproto.ErrorInfo) *agentproto.ErrorInfo {
	if problem == nil {
		return nil
	}
	copy := *problem
	return &copy
}

func (s *Service) remoteTurnFailureEvent(outcome *remoteTurnOutcome) eventcontract.Event {
	notice := &control.Notice{
		Code:                  "turn_failed",
		TemporarySessionLabel: s.temporarySessionLabel(outcome.Surface, outcome.InstanceID, outcome.ThreadID, outcome.TurnID),
		Text:                  firstNonEmpty(strings.TrimSpace(outcome.ErrorMessage), "当前 turn 失败。"),
	}
	if outcome.Problem != nil {
		problemNotice := NoticeForProblem(*outcome.Problem)
		problemNotice.Code = "turn_failed"
		problemNotice.TemporarySessionLabel = s.temporarySessionLabel(outcome.Surface, outcome.InstanceID, outcome.ThreadID, outcome.TurnID)
		notice = &problemNotice
	}
	event := eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: outcome.Surface.SurfaceSessionID,
		SourceMessageID:  strings.TrimSpace(firstNonEmpty(outcome.Binding.ReplyToMessageID, outcome.Item.ReplyToMessageID, outcome.Item.SourceMessageID)),
		Notice:           notice,
	}
	if strings.TrimSpace(event.SourceMessageID) != "" {
		event.Meta.MessageDelivery = eventcontract.ReplyThreadAppendOnlyDelivery()
	}
	return event
}

func remoteTurnEventShowsOutput(event agentproto.Event) bool {
	switch event.Kind {
	case agentproto.EventItemStarted,
		agentproto.EventItemDelta,
		agentproto.EventItemCompleted,
		agentproto.EventRequestStarted,
		agentproto.EventTurnPlanUpdated:
		return true
	default:
		return false
	}
}

func (s *Service) observeRemoteTurnActivity(instanceID string, event agentproto.Event) {
	if !remoteTurnEventShowsOutput(event) {
		return
	}
	binding := s.lookupRemoteTurn(instanceID, event.ThreadID, event.TurnID)
	if binding == nil {
		return
	}
	binding.AnyOutputSeen = true
	if binding.AutoContinueEpisodeID != "" {
		if surface := s.root.Surfaces[binding.SurfaceSessionID]; surface != nil {
			if episode := activeAutoContinueEpisode(surface); episode != nil && strings.TrimSpace(episode.EpisodeID) == strings.TrimSpace(binding.AutoContinueEpisodeID) {
				episode.CurrentAttemptOutputSeen = true
			}
		}
	}
}
