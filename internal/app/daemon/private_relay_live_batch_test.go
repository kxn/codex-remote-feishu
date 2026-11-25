package daemon

import (
	"errors"
	"reflect"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateRelayBatchRejectsLiveWithoutDroppingDurableTerminal(t *testing.T) {
	events := []agentproto.Event{{Kind: agentproto.EventItemDelta, Live: &agentproto.RuntimeLiveIdentity{Sequence: 1}}, {Kind: agentproto.EventItemCompleted}, {Kind: agentproto.EventTurnCompleted}}
	var seen, rejected []agentproto.EventKind
	applyPrivateEventBatch(events, func(e agentproto.Event) error {
		seen = append(seen, e.Kind)
		if e.Live != nil {
			return errors.New("live turn is not projected yet")
		}
		return nil
	}, func(e agentproto.Event, _ error) { rejected = append(rejected, e.Kind) })
	if !reflect.DeepEqual(seen, []agentproto.EventKind{agentproto.EventItemDelta, agentproto.EventItemCompleted, agentproto.EventTurnCompleted}) || !reflect.DeepEqual(rejected, []agentproto.EventKind{agentproto.EventItemDelta}) {
		t.Fatalf("events=%v rejected=%v", seen, rejected)
	}
}

func TestPrivateRelayBatchKeepsDurableOrderOnFailure(t *testing.T) {
	events := []agentproto.Event{{Kind: agentproto.EventItemCompleted}, {Kind: agentproto.EventTurnCompleted}}
	var seen []agentproto.EventKind
	applyPrivateEventBatch(events, func(e agentproto.Event) error {
		seen = append(seen, e.Kind)
		return errors.New("durable commit failed")
	}, func(agentproto.Event, error) {})
	if !reflect.DeepEqual(seen, []agentproto.EventKind{agentproto.EventItemCompleted}) {
		t.Fatal(seen)
	}
}
