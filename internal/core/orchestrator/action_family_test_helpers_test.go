package orchestrator

import "github.com/kxn/codex-remote-feishu/internal/core/control"

func testRequestAction(requestID, requestType, optionID string, answers map[string][]string, revision int) *control.ActionRequestResponse {
	return &control.ActionRequestResponse{
		RequestID:       requestID,
		RequestType:     requestType,
		RequestOptionID: optionID,
		Answers:         answers,
		RequestRevision: revision,
	}
}

func testOwnerFlow(flowID, optionID string) *control.ActionOwnerCardFlow {
	return &control.ActionOwnerCardFlow{FlowID: flowID, OptionID: optionID}
}
