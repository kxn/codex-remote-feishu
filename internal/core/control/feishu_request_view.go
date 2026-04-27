package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

// FeishuRequestView is the UI-owned request payload used by the Feishu adapter
// for approval / request_user_input / permissions / elicitation cards.
type FeishuRequestView struct {
	RequestID            string
	RequestType          string
	SemanticKind         string
	RequestRevision      int
	Title                string
	DetourLabel          string
	ThreadID             string
	ThreadTitle          string
	Sections             []FeishuCardTextSection
	Options              []RequestPromptOption
	Questions            []RequestPromptQuestion
	CurrentQuestionIndex int
	HintText             string
	Phase                frontstagecontract.Phase
	ActionPolicy         frontstagecontract.ActionPolicy
	StatusText           string
	Sealed               bool
}

func NormalizeFeishuRequestView(view FeishuRequestView) FeishuRequestView {
	frame := frontstagecontract.NormalizeFrame(frontstagecontract.Frame{
		OwnerKind:    frontstagecontract.OwnerCardRequest,
		Phase:        normalizeRequestPhase(view),
		ActionPolicy: view.ActionPolicy,
	})
	view.Phase = frame.Phase
	view.ActionPolicy = frame.ActionPolicy
	view.Sealed = frontstagecontract.SealedForPhase(frame.Phase)
	view.Title = strings.TrimSpace(view.Title)
	view.DetourLabel = strings.TrimSpace(view.DetourLabel)
	view.ThreadID = strings.TrimSpace(view.ThreadID)
	view.ThreadTitle = strings.TrimSpace(view.ThreadTitle)
	view.HintText = strings.TrimSpace(view.HintText)
	view.StatusText = strings.TrimSpace(view.StatusText)
	return view
}

func normalizeRequestPhase(view FeishuRequestView) frontstagecontract.Phase {
	if view.Phase != "" {
		return view.Phase
	}
	if view.Sealed {
		return frontstagecontract.PhaseWaitingDispatch
	}
	return frontstagecontract.PhaseEditing
}
