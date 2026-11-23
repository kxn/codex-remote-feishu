package workstore

import (
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Persistence-only fixture: authenticated creation and native dispatch are
// exercised through the daemon's real SDK-source/Driver/Coordinator tests.
func TestPrivateShellUnknownReservationPersistsAcrossReopenWithoutReplay(t *testing.T) {
	s, path := openTestStore(t)
	seedPersonal(t, s)
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: "s", ContextID: "c-r", RoleID: "r", WorkerSessionID: "w", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	requireTx(t, s, func(tx *Tx) error {
		if e := tx.PutInbox(testInput("shell-source", "r", "c-r"), 0); e != nil {
			return e
		}
		v := PrivateShellOperation{ID: "shell-op", CommandID: "shell-command", InputID: "shell-source", InputIDs: []string{"shell-source"}, ScopeID: "s", ContextID: "c-r", PrincipalID: "p", Execution: owner, Incarnation: "incarnation", SessionID: "native-session", State: "unknown", Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(), LastError: "transport receipt unavailable"}
		if e := tx.savePrivateShellOperation(v, 0); e != nil {
			return e
		}
		// Initial reservation is committed in sending state; unknown is a later
		// monotone transition, never an unregistered implicit creation.
		v.State = "sending"
		v.Revision++
		if e := tx.savePrivateShellOperation(v, 1); e != nil {
			return e
		}
		input, e := tx.GetInbox(v.InputID)
		if e != nil {
			return e
		}
		input.ShellOperationID = v.ID
		input.Revision++
		if e = tx.PutInbox(input, 1); e != nil {
			return e
		}
		v.State = "unknown"
		v.Revision++
		return tx.savePrivateShellOperation(v, 2)
	})
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	requireTx(t, reopened, func(tx *Tx) error {
		v, e := tx.GetPrivateShellOperation("shell-command")
		if e != nil {
			return e
		}
		if v.State != "unknown" || v.LastError == "" {
			t.Fatal("restart lost unknown result evidence")
		}
		input, e := tx.GetInbox(v.InputID)
		if e != nil {
			return e
		}
		if input.State != work.InboxPending || input.ShellOperationID != v.ID || input.ClaimedAttemptID != "" {
			t.Fatal("restart consumed source or invented model ownership")
		}
		ready, e := tx.ListPendingInputs("s", "r")
		if e != nil {
			return e
		}
		if len(ready) != 0 {
			t.Fatal("restart made unknown shell source automatically dispatchable")
		}
		command := agentproto.Command{CommandID: v.CommandID, Kind: agentproto.CommandThreadShellCommand, Execution: &owner, ShellCommand: agentproto.ShellCommand{OperationID: v.ID}}
		if e = tx.ValidatePrivateShellCommand(command, v.Incarnation); !errors.Is(e, ErrScopeDenied) {
			t.Fatal("unknown shell became sendable after restart", e)
		}
		return nil
	})
}
