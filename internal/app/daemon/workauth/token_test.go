package workauth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func workerClaims() Claims {
	return Claims{Purpose: "worker", RoleID: "engineer", BotGeneration: 3, ScopeID: "project-a", ContextID: "context-a", WorkerID: "worker-a", SessionGeneration: 4, ControllerGeneration: 2}
}
func TestSignedCredentialIntegrityAndExpiry(t *testing.T) {
	now := time.Unix(1700000000, 0)
	key := []byte(strings.Repeat("k", 32))
	s, err := New(key, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.Issue(workerClaims(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	key[0] = 'x' // The signer must own a copy of its key.
	c, err := s.Verify(token)
	if err != nil || c.WorkerID != "worker-a" {
		t.Fatalf("verify: %+v %v", c, err)
	}
	parts := strings.Split(token, ".")
	parts[0] += "A"
	if _, err = s.Verify(strings.Join(parts, ".")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("tampering accepted: %v", err)
	}
	other, _ := New([]byte(strings.Repeat("z", 32)), func() time.Time { return now })
	if _, err = other.Verify(token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("other daemon key accepted token")
	}
	now = now.Add(time.Hour)
	if _, err = s.Verify(token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expiry accepted: %v", err)
	}
}

func TestBootstrapCannotBecomeWorkerOrAnotherChat(t *testing.T) {
	now := time.Now()
	s, _ := New([]byte(strings.Repeat("s", 32)), func() time.Time { return now })
	bootstrap := Claims{Purpose: "bootstrap", RoleID: "leader", BotGeneration: 1, Tenant: "tenant", ChatID: "chat", PrincipalID: "human", OperationID: "creation"}
	token, err := s.Issue(bootstrap, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := s.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	binding := work.RoleBotBinding{RoleID: "leader", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}
	if err = AuthorizeBootstrap(claims, "tenant", "chat", "human", "creation", binding); err != nil {
		t.Fatal(err)
	}
	if err = AuthorizeBootstrap(claims, "tenant", "other-chat", "human", "creation", binding); err == nil {
		t.Fatal("bootstrap crossed chat")
	}
	bootstrap.ScopeID = "project"
	if _, err = s.Issue(bootstrap, time.Hour); err == nil {
		t.Fatal("bootstrap promoted itself")
	}
}

func TestAuthorizeRunRejectsLateCrossScopeAndPausedCalls(t *testing.T) {
	c := workerClaims()
	worker := work.WorkerSession{ID: c.WorkerID, ContextID: c.ContextID, ScopeID: c.ScopeID, RoleID: c.RoleID, Backend: work.BackendCodex, State: work.WorkerBusy, Generation: 4, Controller: work.ControllerAutomatic, ControllerGeneration: 2, Revision: 1}
	attempt := work.ExecutionAttempt{ID: "attempt", ContextID: c.ContextID, ScopeID: c.ScopeID, RunRef: work.RunRef{WorkerID: c.WorkerID, Generation: 4, RunID: "run-current"}, State: work.AttemptRunning, CommandID: "command", Revision: 1}
	binding := work.RoleBotBinding{RoleID: c.RoleID, GatewayRef: "gw", AppID: "app", IdentityGeneration: 3}
	if err := AuthorizeRun(c, "run-current", worker, attempt, binding, nil); err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"", "run-previous"} {
		if err := AuthorizeRun(c, run, worker, attempt, binding, nil); err == nil {
			t.Fatalf("accepted run %q", run)
		}
	}
	other := c
	other.ScopeID = "project-b"
	if err := AuthorizeRun(other, "run-current", worker, attempt, binding, nil); err == nil {
		t.Fatal("cross-project call allowed")
	}
	gate := work.PauseGate{ID: "pause", ScopeID: c.ScopeID, Source: work.GateProject, SourceID: c.ScopeID, State: work.GateClosed, Revision: 1}
	if err := AuthorizeRun(c, "run-current", worker, attempt, binding, []work.PauseGate{gate}); err == nil {
		t.Fatal("paused call allowed")
	}
	binding.IdentityGeneration++
	if err := AuthorizeRun(c, "run-current", worker, attempt, binding, nil); err == nil {
		t.Fatal("old bot credential allowed")
	}
	binding.IdentityGeneration--
	worker.ControllerGeneration++
	if err := AuthorizeRun(c, "run-current", worker, attempt, binding, nil); err == nil {
		t.Fatal("old controller allowed")
	}
}
