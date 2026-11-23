package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workreaction"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type migrationReactionResolver struct{ api *feishu.ProjectChatAPI }

func (r migrationReactionResolver) Resolve(context.Context, work.RoleBotBinding) (workpublish.Sender, error) {
	return r.api, nil
}

func TestPersonalReactionFenceWaitsForActualHTTPAndPreservesUnknown(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint("unknown=", unknown), func(t *testing.T) {
			ctx := t.Context()
			dbPath := filepath.Join(t.TempDir(), "team.db")
			store, err := workstore.Open(ctx, dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = store.Close() }()
			a := &App{teamRuntime: &teamRuntime{store: store}}
			now := time.Now().UTC()
			role := teamID("role", "gateway")
			err = store.Within(ctx, func(tx *workstore.Tx) error {
				if e := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); e != nil {
					return e
				}
				if e := tx.PutRole(work.Role{ID: role, Name: "Role", Description: "Work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
					return e
				}
				if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: role, GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
					return e
				}
				if e := tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopePersonal, PrincipalID: "human", RoleID: role, Revision: 1}, 0); e != nil {
					return e
				}
				scope, e := tx.GetScope("scope")
				if e != nil {
					return e
				}
				if e = tx.PutContext(work.WorkContext{ID: "reaction-context", ScopeID: scope.ID, RoleID: role, Workstream: "main", Revision: 1}, 0); e != nil {
					return e
				}
				if e = tx.PutContent(work.NewContent("reaction-body", scope.ID, "text/plain", []byte("text"), now)); e != nil {
					return e
				}
				msg := work.Message{ID: "reaction-message", ScopeID: scope.ID, ConversationID: "chat", Source: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "chat", MessageID: "original"}, BodyRef: "reaction-body", Observations: []work.Observation{{AppID: "app", IdentityGeneration: 1, ReceivedAt: now}}, Revision: 1, CreatedAt: now}
				inbox := work.Inbox{ID: "reaction-input", ScopeID: scope.ID, RecipientRoleID: role, RecipientContextID: "reaction-context", EventID: "reaction-event", MessageID: msg.ID, State: work.InboxPending, Revision: 1, AvailableAt: now}
				_, _, e = tx.AcceptMessage(msg, []work.Inbox{inbox})
				return e
			})
			if err != nil {
				t.Fatal(err)
			}
			entered := make(chan struct{})
			release := make(chan struct{})
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "tenant_access_token") {
					fmt.Fprint(w, "{\"code\":0,\"tenant_access_token\":\"fixture\",\"expire\":7200}")
					return
				}
				if r.Method != http.MethodPost {
					t.Errorf("unexpected %s", r.Method)
					http.Error(w, "unexpected", 500)
					return
				}
				calls.Add(1)
				close(entered)
				<-release
				if unknown {
					http.Error(w, "uncertain", 503)
					return
				}
				fmt.Fprint(w, "{\"code\":0,\"data\":{\"reaction_id\":\"actual-receipt\"}}")
			}))
			defer server.Close()
			client := lark.NewClient("app", "fixture", lark.WithOpenBaseUrl(server.URL), lark.WithHttpClient(server.Client()))
			sdk := feishu.NewProjectChatAPI(feishu.NewFeishuCallBroker(t.Name(), client))
			service := workreaction.Service{Store: store, Resolver: migrationReactionResolver{sdk}, ActivityGuard: a.teamRuntime.beginPersonalSourceActivity}
			done := make(chan error, 1)
			go func() { done <- service.RunOnce(ctx, 16) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("HTTP not started")
			}
			fence, active, e := a.teamRuntime.beginPersonalSourceFence(ctx, "scope", "migration", now)
			if e != nil || active != 1 {
				t.Fatalf("fence: %+v active %d %v", fence, active, e)
			}
			waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
			if e = a.teamRuntime.waitPersonalSourceActivities(waitCtx, "scope"); e == nil {
				t.Fatal("drain passed active HTTP")
			}
			cancel()
			close(release)
			if e = <-done; unknown && e == nil {
				t.Fatal("unknown HTTP reported success")
			} else if !unknown && e != nil {
				t.Fatal(e)
			}
			if e = a.teamRuntime.waitPersonalSourceActivities(ctx, "scope"); e != nil {
				t.Fatal(e)
			}
			if e = service.RunOnce(ctx, 16); e != nil {
				t.Fatal(e)
			}
			if calls.Load() != 1 {
				t.Fatalf("fenced source sent again: %d", calls.Load())
			}
			err = store.Within(ctx, func(tx *workstore.Tx) error {
				f, e := tx.GetPersonalMigrationFence("scope")
				if e != nil {
					return e
				}
				if _, e = tx.StartPersonalMigrationDrain("scope", "migration", f.Revision, now); e != nil {
					return e
				}
				status, e := tx.PersonalMigrationStatus("scope")
				if e != nil {
					return e
				}
				if unknown && status.UnsettledReactions != 1 {
					t.Fatalf("unknown missing: %+v", status)
				}
				if !unknown && status.UnsettledReactions != 0 {
					t.Fatalf("confirmed held: %+v", status)
				}
				reaction, e := tx.GetPrivateReaction("input-reaction:reaction-input")
				if e != nil {
					return e
				}
				if unknown && reaction.InFlightAction == "" {
					t.Fatal("unknown lost")
				}
				if !unknown && reaction.ReactionIDs["OneSecond"] != "actual-receipt" {
					t.Fatalf("receipt lost: %+v", reaction)
				}
				if unknown {
					proof := work.NewContent("migration-intake-proof:digest", "scope", "application/json", []byte("{\"migration_id\":\"migration\",\"digest\":\"digest\"}"), now)
					if e = tx.PutContent(proof); e != nil {
						return e
					}
					if e = tx.HoldPersonalUncertainReaction("scope", reaction.ID, proof.ID, now); e != nil {
						return e
					}
					status, e = tx.PersonalMigrationStatus("scope")
					if e != nil {
						return e
					}
					if status.UnsettledReactions != 0 {
						t.Fatalf("component not isolated: %+v", status)
					}
					export, e := tx.CapturePersonalExport("scope", workstore.PersonalExportLimits{MaxRows: 10000, MaxBytes: 10 << 20})
					if e != nil {
						return e
					}
					found := false
					for _, row := range export.Records {
						if row.Table == "personal_migration_reaction_holds" && row.Key == reaction.ID {
							found = true
						}
					}
					if !found {
						t.Fatal("uncertain reaction absent from source snapshot")
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = workstore.Open(ctx, dbPath)
			if err != nil {
				t.Fatal(err)
			}
			a.teamRuntime.store = store
			service.Store = store
			if err = service.RunOnce(ctx, 16); err != nil {
				t.Fatal(err)
			}
			if err = store.Within(ctx, func(tx *workstore.Tx) error {
				status, e := tx.PersonalMigrationStatus("scope")
				if e != nil {
					return e
				}
				if status.UnsettledReactions != 0 {
					t.Fatalf("restart lost hold or receipt: %+v", status)
				}
				reaction, e := tx.GetPrivateReaction("input-reaction:reaction-input")
				if e != nil {
					return e
				}
				if unknown && reaction.InFlightAction == "" {
					t.Fatal("restart cleared unknown")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatal("unexpected second external write")
			}
		})
	}
}
