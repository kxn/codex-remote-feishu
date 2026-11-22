package workpublish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type snapshotSender struct {
	calls, patches int
	bodies         []string
	losePatch      bool
}

func (s *snapshotSender) Resolve(context.Context, work.RoleBotBinding) (Sender, error) { return s, nil }
func (s *snapshotSender) PublishText(_ context.Context, r feishu.ProjectPublishRequest) (feishu.ProjectPublishReceipt, error) {
	s.calls++
	s.bodies = append(s.bodies, r.Text)
	return feishu.ProjectPublishReceipt{ChatID: r.ChatID, MessageID: fmt.Sprintf("om_%d", s.calls)}, nil
}
func (s *snapshotSender) PublishCard(_ context.Context, r feishu.ProjectCardPublishRequest) (feishu.ProjectPublishReceipt, error) {
	s.calls++
	s.bodies = append(s.bodies, string(r.Card))
	return feishu.ProjectPublishReceipt{ChatID: r.ChatID, MessageID: fmt.Sprintf("om_%d", s.calls)}, nil
}
func (s *snapshotSender) PatchCard(_ context.Context, r feishu.ProjectCardPatchRequest) (feishu.ProjectPublishReceipt, error) {
	s.patches++
	s.bodies = append(s.bodies, string(r.Card))
	if s.losePatch {
		return feishu.ProjectPublishReceipt{}, errors.New("lost PATCH response")
	}
	return feishu.ProjectPublishReceipt{ChatID: r.ChatID, MessageID: r.MessageID}, nil
}

func queueSnapshot(t *testing.T, store *workstore.Store, id, role, body string, purpose workservice.PublicationPurpose, now time.Time, origin ...string) {
	t.Helper()
	err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		scope := "group_" + role
		generation := int64(1)
		if role != "role" {
			generation = 2
		}
		if _, err := tx.GetScope(scope); errors.Is(err, workstore.ErrNotFound) {
			if role != "role" {
				if err = tx.PutRole(work.Role{ID: role, Name: role, Description: "role", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
					return err
				}
				if err = tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: role, GatewayRef: role, AppID: role, IdentityGeneration: 1}, 0); err != nil {
					return err
				}
			}
			if err = tx.PutScope(work.WorkScope{ID: scope, Kind: work.ScopeBootstrap, RoleID: role, PrincipalID: "human", Tenant: "tenant", ChatID: "oc_group", BootstrapOperationID: scope + ":bootstrap", BindingGeneration: generation, Revision: 1}, 0); err != nil {
				return err
			}
			content := work.NewContent(scope+":bootstrap-body", scope, "text/plain", []byte("establish"), now)
			if err = tx.PutContent(content); err != nil {
				return err
			}
			op := work.Operation{ID: scope + ":bootstrap", ScopeID: scope, Actor: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, Kind: "project.bootstrap", TargetID: "oc_group", TargetGeneration: generation, IdempotencyKey: scope, RequestRef: content.ID, RequestHash: content.SHA256, State: work.OperationPrepared, Revision: 1, CreatedAt: now, UpdatedAt: now}
			if _, _, err = tx.PutOperation(op, 0); err != nil {
				return err
			}
		}
		binding, err := tx.GetRoleBotBinding(role)
		if err != nil {
			return err
		}
		projection := workservice.MessageProjection{RoleID: role, GatewayRef: binding.GatewayRef, AppID: binding.AppID, BotGeneration: 1, Tenant: "tenant", ChatID: "oc_group", ChannelGeneration: generation, Body: body, Purpose: purpose}
		contextID, authority, err := testPublicationAuthority(tx, scope, role, "", id, "tenant", "oc_group", 1)
		if err != nil {
			return err
		}
		if len(origin) > 0 {
			original, e := tx.GetOperation(origin[0])
			if e != nil {
				return e
			}
			frozen, e := workservice.ReadMessageProjection(tx, original)
			if e != nil {
				return e
			}
			contextID, authority = original.ContextID, frozen.Authority
		}
		projection.Authority = authority
		raw, err := json.Marshal(projection)
		if err != nil {
			return err
		}
		content := work.NewContent(id+":body", scope, "application/json", raw, now)
		if err = tx.PutContent(content); err != nil {
			return err
		}
		op := work.Operation{ID: id, ScopeID: scope, ContextID: contextID, Actor: work.Actor{Kind: work.ActorRole, ID: role}, Kind: "feishu.publish", TargetID: "oc_group", TargetGeneration: 1, IdempotencyKey: id, RequestRef: content.ID, RequestHash: content.SHA256, State: work.OperationPrepared, Revision: 1, CreatedAt: now, UpdatedAt: now}
		if _, _, err = tx.PutOperation(op, 0); err != nil {
			return err
		}
		return tx.PutOutbox(work.Outbox{ID: id, ScopeID: scope, OperationID: id, Target: "oc_group", EventID: id, PayloadRef: content.ID, State: work.OutboxPending, Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGroupSnapshotsCoalesceShareBudgetAndPatchOneCard(t *testing.T) {
	store, _, _, now := publisherFixture(t)
	sender := &snapshotSender{}
	p, _ := New(store, sender, func() time.Time { return *now })
	queueSnapshot(t, store, "a", "role", "old", workservice.PublicationProgress, *now)
	queueSnapshot(t, store, "b", "role", "latest", workservice.PublicationProgress, now.Add(time.Millisecond))
	queueSnapshot(t, store, "c", "role2", "other bot", workservice.PublicationProgress, now.Add(2*time.Millisecond))
	if n, err := p.RunOnce(t.Context(), 10); err != nil || n != 1 {
		t.Fatalf("first batch n=%d err=%v", n, err)
	}
	if sender.calls != 1 || sender.patches != 0 {
		t.Fatalf("calls=%d patches=%d", sender.calls, sender.patches)
	}
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		op, e := tx.GetOperation("a")
		if e == nil && op.State != work.OperationTerminal {
			t.Fatal("obsolete snapshot retained")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// A new publisher instance shares persisted recipient windows.
	p, _ = New(store, sender, func() time.Time { return *now })
	*now = now.Add(9 * time.Second)
	if n, err := p.RunOnce(t.Context(), 10); err != nil || n != 0 {
		t.Fatalf("cross-bot budget bypass n=%d err=%v", n, err)
	}
	*now = now.Add(time.Second)
	if n, err := p.RunOnce(t.Context(), 10); err != nil || n != 1 {
		t.Fatalf("second bot n=%d err=%v", n, err)
	}
	queueSnapshot(t, store, "d", "role", "new snapshot", workservice.PublicationProgress, *now)
	*now = now.Add(20 * time.Second)
	if n, err := p.RunOnce(t.Context(), 10); err != nil || n != 1 {
		t.Fatalf("snapshot update n=%d err=%v", n, err)
	}
	if sender.calls != 2 || sender.patches != 1 {
		t.Fatalf("status appended instead of patched calls=%d patches=%d", sender.calls, sender.patches)
	}
}

func TestFinalDiscardsQueuedProgressAndUnknownPatchIsNeverReplayed(t *testing.T) {
	store, _, _, now := publisherFixture(t)
	sender := &snapshotSender{}
	p, _ := New(store, sender, func() time.Time { return *now })
	queueSnapshot(t, store, "a", "role", "start", workservice.PublicationProgress, *now)
	if _, err := p.RunOnce(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	queueSnapshot(t, store, "b", "role", "obsolete", workservice.PublicationProgress, now.Add(time.Second))
	queueSnapshot(t, store, "c", "role", "done", workservice.PublicationFinal, now.Add(2*time.Second))
	if n, err := p.RunOnce(t.Context(), 10); err != nil || n != 1 {
		t.Fatalf("final blocked by background n=%d err=%v", n, err)
	}
	*now = now.Add(31 * time.Second)
	queueSnapshot(t, store, "d", "role", "next segment", workservice.PublicationProgress, *now)
	if _, err := p.RunOnce(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 3 || sender.patches != 0 {
		t.Fatal("final result was overwritten")
	}
	*now = now.Add(31 * time.Second)
	sender.losePatch = true
	queueSnapshot(t, store, "e", "role", "patch", workservice.PublicationProgress, *now)
	if _, err := p.RunOnce(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if err := p.RetryUnknown(t.Context(), "e"); err == nil {
		t.Fatal("unknown dynamic PATCH replayed")
	}
	if sender.patches != 1 {
		t.Fatal("duplicate PATCH")
	}
	*now = now.Add(31 * time.Second)
	sender.losePatch = false
	queueSnapshot(t, store, "f", "role", "fresh state after uncertainty", workservice.PublicationProgress, *now)
	if n, err := p.RunOnce(t.Context(), 10); err != nil || n != 1 {
		t.Fatalf("uncertain old segment stranded new progress n=%d err=%v", n, err)
	}
	if sender.calls != 4 || sender.patches != 1 {
		t.Fatal("uncertain card was overwritten or new segment missing")
	}
}
