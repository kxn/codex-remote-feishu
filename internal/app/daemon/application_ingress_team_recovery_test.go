package daemon

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestTeamPublicationRecoveryIgnoresPersonalHistoryAndValidatesGroupPatches(t *testing.T) {
	for _, mode := range []string{"valid", "missing_original", "unconfirmed_original", "foreign_actor", "wrong_patch_target"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			path := filepath.Join(t.TempDir(), "team.db")
			store, err := workstore.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = store.Close() }()
			now := time.Now().UTC()
			err = store.Within(ctx, func(tx *workstore.Tx) error {
				if e := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); e != nil {
					return e
				}
				if e := tx.PutRole(work.Role{ID: "role", Name: "Role", Description: "Work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
					return e
				}
				if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
					return e
				}
				if e := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "role", Revision: 1}, 0); e != nil {
					return e
				}
				if e := tx.PutScope(work.WorkScope{ID: "group", Kind: work.ScopeProject, TeamID: "team", ProjectID: "project", Revision: 1}, 0); e != nil {
					return e
				}
				if e := tx.PutProject(work.Project{ID: "project", ScopeID: "group", TeamID: "team", Name: "Project", LeaderRoleID: "role", State: work.ProjectActive, Revision: 1}, 0); e != nil {
					return e
				}
				if e := tx.PutScope(work.WorkScope{ID: "personal", Kind: work.ScopePersonal, PrincipalID: "human", RoleID: "role", Revision: 1}, 0); e != nil {
					return e
				}
				seed := func(scope, id, msg string, patch bool) error {
					projection := workservice.MessageProjection{RoleID: "role", GatewayRef: "gateway", AppID: "app", BotGeneration: 1, Tenant: "tenant", ChatID: "chat", ChannelGeneration: 1, Body: id, Purpose: workservice.PublicationProgress}
					raw, _ := json.Marshal(projection)
					original := work.NewContent(id+":original", scope, "application/json", raw, now)
					if e := tx.PutContent(original); e != nil {
						return e
					}
					frozen := projection
					if patch {
						frozen.ReplaceMessageID = msg
					}
					frozenRaw, _ := json.Marshal(frozen)
					snapshot := work.NewContent(id+":frozen", scope, "application/json", frozenRaw, now)
					if e := tx.PutContent(snapshot); e != nil {
						return e
					}
					receipt, _ := json.Marshal(feishu.ProjectPublishReceipt{MessageID: msg, ChatID: "chat"})
					op := work.Operation{ID: id, ScopeID: scope, Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: "feishu.publish", TargetID: "chat", TargetGeneration: 1, IdempotencyKey: id, RequestHash: original.SHA256, RequestRef: original.ID, State: work.OperationConfirmed, ExternalReceipt: string(receipt), Steps: []work.OperationStep{{ID: "send_projection", State: work.OperationConfirmed, Receipt: snapshot.ID}}, Revision: 1, CreatedAt: now, UpdatedAt: now}
					if _, _, e := tx.PutOperation(op, 0); e != nil {
						return e
					}
					return tx.PutOutbox(work.Outbox{ID: id + ":outbox", ScopeID: scope, OperationID: id, Target: "chat", EventID: id, PayloadRef: original.ID, State: work.OutboxSent, ExternalMessageID: msg, Revision: 1}, 0)
				}
				if e := seed("group", "group-original", "group-message", false); e != nil {
					return e
				}
				if e := tx.PutPlatformMessageMapping(work.PlatformMessageMapping{Tenant: "tenant", ChatID: "chat", MessageID: "group-message", ScopeID: "group", RootMessageID: "group-message", OperationID: "group-original", OutboxID: "group-original:outbox", RoleID: "role", BotGeneration: 1, ChannelGeneration: 1}); e != nil {
					return e
				}
				if e := seed("group", "group-update", "group-message", true); e != nil {
					return e
				}
				// Reproduce the observed legacy shape: original personal mapping plus
				// another confirmed update of the same actual message.
				if e := seed("personal", "private-original", "private-message", false); e != nil {
					return e
				}
				if e := tx.PutPlatformMessageMapping(work.PlatformMessageMapping{Tenant: "tenant", ChatID: "chat", MessageID: "private-message", ScopeID: "personal", RootMessageID: "private-message", OperationID: "private-original", OutboxID: "private-original:outbox", RoleID: "role", BotGeneration: 1}); e != nil {
					return e
				}
				return seed("personal", "private-update", "private-message", true)
			})
			if err != nil {
				t.Fatal(err)
			}
			if mode != "valid" {
				if err = store.Close(); err != nil {
					t.Fatal(err)
				}
				db, e := sql.Open("sqlite", path)
				if e != nil {
					t.Fatal(e)
				}
				var q string
				switch mode {
				case "missing_original":
					q = "DELETE FROM outbox WHERE id='group-original:outbox'"
				case "unconfirmed_original":
					q = "UPDATE operations SET payload=json_set(payload,'$.state','sending') WHERE id='group-original'"
				case "foreign_actor":
					q = "UPDATE operations SET payload=json_set(payload,'$.actor.id','other') WHERE id='group-original'"
				case "wrong_patch_target":
					var raw []byte
					if e = db.QueryRow("SELECT data FROM content WHERE id='group-update:frozen'").Scan(&raw); e != nil {
						t.Fatal(e)
					}
					var projection workservice.MessageProjection
					if e = json.Unmarshal(raw, &projection); e != nil {
						t.Fatal(e)
					}
					projection.ReplaceMessageID = "different-message"
					raw, e = json.Marshal(projection)
					if e != nil {
						t.Fatal(e)
					}
					hash := sha256.Sum256(raw)
					if _, e = db.Exec("UPDATE content SET data=?,sha256=? WHERE id='group-update:frozen'", raw, hex.EncodeToString(hash[:])); e != nil {
						t.Fatal(e)
					}
				}
				if _, e = db.Exec("PRAGMA foreign_keys=OFF"); e != nil {
					t.Fatal(e)
				}
				if q != "" {
					if _, e = db.Exec(q); e != nil {
						t.Fatal(e)
					}
				}
				db.Close()
				store, err = workstore.Open(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
			}
			binder := &teamPublicationBinding{store: store}
			routerPath := filepath.Join(t.TempDir(), "router.db")
			router, e := ingressowner.Open(routerPath, binder, nil)
			if e != nil {
				t.Fatal(e)
			}
			binder.router = router
			event := ingressowner.Event{Key: ingressowner.Key{Tenant: "tenant", AppID: "app", EventID: "group-event"}, Source: ingressowner.Source{GatewayID: "gateway", BindingGeneration: 1, ReceivedAt: now}, Kind: ingressowner.Message, ChatType: "group", ChatID: "chat", MessageID: "source", Operator: ingressowner.Operator{IDType: "open_id", ID: "human"}, Payload: json.RawMessage("{\"sdk\":\"fact\"}")}
			if _, _, e = router.Admit(ctx, event, applicationOwnerResolver{router: router}); e != nil {
				t.Fatal(e)
			}
			// Directly test the confirmed update, so a broken original cannot hide it.
			_, e = binder.confirmed(ctx, teamPublicationProof{Kind: "team_message", ID: "group-update:outbox"})
			if mode == "valid" && e != nil {
				t.Fatalf("valid patch: %v", e)
			}
			if mode != "valid" && e == nil {
				t.Fatalf("%s accepted", mode)
			}
			if mode == "valid" {
				if e = binder.ReconcileConfirmed(ctx); e != nil {
					t.Fatal(e)
				}
				if e = router.Close(); e != nil {
					t.Fatal(e)
				}
				router, e = ingressowner.Open(routerPath, binder, nil)
				if e != nil {
					t.Fatal(e)
				}
				binder.router = router
				if e = binder.ReconcileConfirmed(ctx); e != nil {
					t.Fatal(e)
				}
				actual, e := router.PublishedMessage(ctx, "tenant", "app", "chat", "group-message")
				if e != nil || actual.Owner.Application != ingressowner.Team {
					t.Fatalf("group owner after restart: %+v %v", actual, e)
				}
				if _, e = router.PublishedMessage(ctx, "tenant", "app", "chat", "private-message"); !errors.Is(e, sql.ErrNoRows) {
					t.Fatalf("legacy private message registered as team: %v", e)
				}
			} else if e = binder.ReconcileConfirmed(ctx); e == nil {
				t.Fatal(fmt.Sprintf("%s did not fail startup reconciliation", mode))
			}
			router.Close()
		})
	}
}
