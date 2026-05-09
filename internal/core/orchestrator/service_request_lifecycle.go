package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	requestLifecycleQueuedInactive         = "queued_inactive"
	requestLifecycleAwaitingVisibility     = "awaiting_visibility"
	requestLifecycleEditingVisible         = "editing_visible"
	requestLifecycleSubmitting             = "submitting"
	requestLifecycleAwaitingBackendConsume = "awaiting_backend_consume"
	requestLifecycleResolved               = "resolved"
	requestLifecycleAborted                = "aborted"
)

func normalizeRequestLifecycleState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case requestLifecycleQueuedInactive:
		return requestLifecycleQueuedInactive
	case requestLifecycleAwaitingVisibility:
		return requestLifecycleAwaitingVisibility
	case requestLifecycleEditingVisible:
		return requestLifecycleEditingVisible
	case requestLifecycleSubmitting:
		return requestLifecycleSubmitting
	case requestLifecycleAwaitingBackendConsume:
		return requestLifecycleAwaitingBackendConsume
	case requestLifecycleResolved:
		return requestLifecycleResolved
	case requestLifecycleAborted:
		return requestLifecycleAborted
	default:
		return ""
	}
}

func inferRequestLifecycleState(record *state.RequestPromptRecord) string {
	if record == nil {
		return requestLifecycleEditingVisible
	}
	if normalizeRequestVisibilityState(record.VisibilityState) == requestVisibilityResolved {
		return requestLifecycleResolved
	}
	if strings.TrimSpace(record.PendingDispatchCommandID) != "" {
		return requestLifecycleSubmitting
	}
	if frontstagecontract.NormalizePhase(record.Phase) == frontstagecontract.PhaseWaitingDispatch {
		return requestLifecycleAwaitingBackendConsume
	}
	return requestLifecycleEditingVisible
}

func requestLifecycleUsesWaitingDispatchPhase(request *state.RequestPromptRecord) bool {
	if request == nil {
		return false
	}
	switch normalizeRequestLifecycleState(request.LifecycleState) {
	case requestLifecycleSubmitting, requestLifecycleAwaitingBackendConsume:
		return true
	default:
		return false
	}
}

func requestLifecycleBlocksInteractiveResponse(request *state.RequestPromptRecord) bool {
	if request == nil {
		return false
	}
	switch normalizeRequestLifecycleState(request.LifecycleState) {
	case requestLifecycleSubmitting, requestLifecycleAwaitingBackendConsume:
		return true
	default:
		return false
	}
}

func setRequestLifecycleState(request *state.RequestPromptRecord, lifecycle string) {
	if request == nil {
		return
	}
	switch normalizeRequestLifecycleState(lifecycle) {
	case requestLifecycleQueuedInactive:
		request.LifecycleState = requestLifecycleQueuedInactive
		request.Phase = frontstagecontract.PhaseEditing
	case requestLifecycleAwaitingVisibility:
		request.LifecycleState = requestLifecycleAwaitingVisibility
		request.Phase = frontstagecontract.PhaseEditing
	case requestLifecycleEditingVisible:
		request.LifecycleState = requestLifecycleEditingVisible
		request.Phase = frontstagecontract.PhaseEditing
	case requestLifecycleSubmitting:
		request.LifecycleState = requestLifecycleSubmitting
		request.Phase = frontstagecontract.PhaseWaitingDispatch
	case requestLifecycleAwaitingBackendConsume:
		request.LifecycleState = requestLifecycleAwaitingBackendConsume
		request.Phase = frontstagecontract.PhaseWaitingDispatch
	case requestLifecycleResolved:
		request.LifecycleState = requestLifecycleResolved
	case requestLifecycleAborted:
		request.LifecycleState = requestLifecycleAborted
	}
}

func markRequestQueuedInactive(request *state.RequestPromptRecord) {
	if request == nil {
		return
	}
	request.PendingDispatchCommandID = ""
	setRequestLifecycleState(request, requestLifecycleQueuedInactive)
}

func markRequestAwaitingVisibility(request *state.RequestPromptRecord) {
	if request == nil {
		return
	}
	request.PendingDispatchCommandID = ""
	setRequestLifecycleState(request, requestLifecycleAwaitingVisibility)
}

func markRequestVisibleEditing(request *state.RequestPromptRecord) {
	if request == nil {
		return
	}
	request.PendingDispatchCommandID = ""
	setRequestLifecycleState(request, requestLifecycleEditingVisible)
}

func markRequestSubmitting(request *state.RequestPromptRecord, commandID string) {
	if request == nil {
		return
	}
	request.PendingDispatchCommandID = strings.TrimSpace(commandID)
	setRequestLifecycleState(request, requestLifecycleSubmitting)
}

func markRequestAwaitingBackendConsume(request *state.RequestPromptRecord) {
	if request == nil {
		return
	}
	request.PendingDispatchCommandID = ""
	setRequestLifecycleState(request, requestLifecycleAwaitingBackendConsume)
}

func markRequestResolvedLifecycle(request *state.RequestPromptRecord) {
	if request == nil {
		return
	}
	request.PendingDispatchCommandID = ""
	request.VisibilityState = requestVisibilityResolved
	setRequestLifecycleState(request, requestLifecycleResolved)
}

func markRequestAborted(request *state.RequestPromptRecord, phase frontstagecontract.Phase) {
	if request == nil {
		return
	}
	request.PendingDispatchCommandID = ""
	request.Phase = frontstagecontract.NormalizePhase(phase)
	setRequestLifecycleState(request, requestLifecycleAborted)
}
