package daemon

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestPrivateProjectShareRequiresPersonCardConfirmation(t *testing.T) {
	reducer := privateHostReducer{}
	action := control.Action{Kind: control.ActionPrivateProject, GatewayID: "g", SurfaceSessionID: "surface", Text: "/project confirm draft digest"}
	events, err := reducer.projectAction(action)
	if err != nil || len(events) != 1 || events[0].Kind != eventcontract.KindNotice {
		t.Fatalf("text confirmation should be refused: %+v %v", events, err)
	}
	action.Inbound = &control.ActionInboundMeta{CardCallback: true, OpenMessageID: "card"}
	events, err = reducer.projectAction(action)
	if err != nil || len(events) != 2 || events[0].Kind != eventcontract.KindPage || !events[0].InlineReplaceCurrentCard || events[1].DaemonCommand == nil || events[1].DaemonCommand.Kind != control.DaemonCommandPrivateProjectShare {
		t.Fatalf("card confirmation: %+v %v", events, err)
	}
}
