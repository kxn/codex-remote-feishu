package workengine

import (
	"context"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

type heldPreparation struct {
	started chan WorkerNeed
	release chan struct{}
}

func (p *heldPreparation) Prepare(_ context.Context, need WorkerNeed) error {
	p.started <- need
	<-p.release
	return nil
}

func TestPreparationIsBoundedFairAndDoesNotBlockTick(t *testing.T) {
	s, _ := fixture(t, "a", "b")
	transaction(t, s, func(tx *workstore.Tx) error {
		for n := 0; n < 100; n++ {
			scope := "a"
			if n%2 == 1 {
				scope = "b"
			}
			id := fmt.Sprintf("pending-context:%03d", n)
			if e := tx.PutContext(work.WorkContext{ID: id, ScopeID: scope, RoleID: "leader", Workstream: id, Revision: 1}, 0); e != nil {
				return e
			}
			if e := tx.PutInbox(work.Inbox{ID: "input:" + id, ScopeID: scope, RecipientRoleID: "leader", RecipientContextID: id, EventID: id, State: work.InboxPending, Revision: 1, AvailableAt: testNow}, 0); e != nil {
				return e
			}
		}
		return nil
	})
	p := &heldPreparation{started: make(chan WorkerNeed, 100), release: make(chan struct{})}
	e, err := New(s, Config{MaxConcurrent: 3, Provisioner: p, Now: func() time.Time { return testNow }}, &testPlanner{}, &testDispatcher{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		close(p.release)
		if err := e.DrainPreparations(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	finished := make(chan error, 1)
	go func() { _, err := e.Tick(t.Context()); finished <- err }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow host launch blocked scheduler")
	}
	scopes := map[string]bool{}
	for n := 0; n < 3; n++ {
		select {
		case need := <-p.started:
			scopes[need.ScopeID] = true
		case <-time.After(time.Second):
			t.Fatal("bounded preparation not started")
		}
	}
	if len(scopes) != 2 {
		t.Fatalf("preparation was not fair across scopes: %v", scopes)
	}
	for n := 0; n < 3; n++ {
		if _, err = e.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case extra := <-p.started:
		t.Fatalf("100 pending inputs caused unbounded launch: %+v", extra)
	default:
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err = e.DrainPreparations(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("drain claimed stuck Start had returned: %v", err)
	}
	if _, err = e.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case extra := <-p.started:
		t.Fatalf("preparation started after shutdown fence: %+v", extra)
	default:
	}
}
