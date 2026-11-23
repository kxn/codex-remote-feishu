package daemon

import (
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestTeamOldSocketCannotAffectReplacement(t *testing.T) {
	owners := &teamExecutionOwnerAdapter{connections: map[string]teamConnection{"worker": {id: 2, lease: &teamExecutionLease{}}}}
	app := &App{applications: &applicationRuntime{teamOwners: owners}}
	handled, err := app.teamEvents(t.Context(), relayws.ConnectionMeta{ConnectionID: 1}, "worker", []agentproto.Event{{Kind: agentproto.EventItemDelta}})
	if !handled || !errors.Is(err, executionowner.ErrConflict) {
		t.Fatalf("stale event accepted: %v %v", handled, err)
	}
	handled, err = app.teamDisconnect(t.Context(), relayws.ConnectionMeta{ConnectionID: 1}, "worker")
	if !handled || err != nil || owners.connections["worker"].id != 2 {
		t.Fatal("stale disconnect removed current connection")
	}
}
