package workstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPersonalExportIsolationMediaUnknownAndPreferences(t *testing.T) {
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "team.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	err = s.Within(t.Context(), func(tx *Tx) error {
		if e := tx.PutRole(work.Role{ID: "role", Name: "Role", Description: "work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		binding := work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}
		if e := tx.PutRoleBotBinding(binding, 0); e != nil {
			return e
		}
		for _, name := range []string{"alice", "bob"} {
			raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: name + "-event", Tenant: "tenant", ChatID: name + "-chat", ChatType: "p2p", MessageID: name + "-sdk", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: name}, MessageType: "text", RawContent: `{"text":"private"}`, ReceivedAt: now}
			gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1}
			ingress, _, e := tx.AcceptRawIngress(raw, gateway)
			if e != nil {
				return e
			}
			principal, _, e := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: "tenant", AppID: "app", Generation: 1, IDs: raw.SenderIDs, ObservationRef: ingress.ID})
			if e != nil {
				return e
			}
			scope := work.WorkScope{ID: name + "-scope", Kind: work.ScopePersonal, PrincipalID: principal.ID, RoleID: "role", Revision: 1}
			if e = tx.PutScope(scope, 0); e != nil {
				return e
			}
			navigation, e := tx.EnsurePrivateNavigation(scope.ID, ingress.ID, now)
			if e != nil {
				return e
			}
			if e = tx.FreezePrivateIngressNavigation(ingress.ID, navigation); e != nil {
				return e
			}
			body := work.NewContent(name+"-body", scope.ID, "text/plain", []byte(name+" private input"), now)
			image := work.NewContent(name+"-image", scope.ID, "image/png", []byte(name+" image bytes"), now)
			for _, item := range []work.Content{body, image} {
				if e = tx.PutContent(item); e != nil {
					return e
				}
			}
			msg := work.Message{ID: name + "-message", ScopeID: scope.ID, ConversationID: name + "-conversation", Source: work.Actor{Kind: work.ActorPrincipal, ID: principal.ID}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: raw.ChatID, MessageID: raw.MessageID}, BodyRef: body.ID, Revision: 1, CreatedAt: now, Attachments: []work.MessageAttachment{{ContentRef: image.ID, SHA256: image.SHA256, Size: int64(len(image.Data)), MediaType: image.MediaType, SourceMessageID: raw.MessageID, Kind: "image"}}}
			if _, _, e = tx.AcceptMessage(msg, []work.Inbox{{ID: name + "-inbox", ScopeID: scope.ID, RecipientRoleID: "role", RecipientContextID: navigation.FrontContextID, EventID: "message:" + msg.ID, MessageID: msg.ID, AwaitingText: true, State: work.InboxPending, AvailableAt: now, Revision: 1}}); e != nil {
				return e
			}
			location := work.ExecutionLocation{ID: name + "-location", HostID: "host", Cwd: "/workspace/" + name, Generation: 1, Revision: 1}
			if e = tx.PutLocation(location, 0); e != nil {
				return e
			}
			worker := work.WorkerSession{ID: name + "-worker", ContextID: navigation.FrontContextID, ScopeID: scope.ID, RoleID: "role", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, NativeNamespace: "account:" + name, NativeSessionID: name + "-session", InstanceID: name + "-instance", CarrierIncarnation: name + "-carrier", InstanceGeneration: 1, ProcessGeneration: 1, LocationID: location.ID, LocationGeneration: 1, Revision: 1}
			if e = tx.PutWorker(worker, 0); e != nil {
				return e
			}
			owner := agentproto.ExecutionBinding{Version: 1, ScopeID: scope.ID, ContextID: navigation.FrontContextID, RoleID: "role", WorkerSessionID: worker.ID, SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
			if e = tx.RegisterCarrier(CarrierRegistration{Incarnation: worker.CarrierIncarnation, OriginInstanceID: worker.InstanceID, Owner: owner, Backend: worker.Backend, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}); e != nil {
				return e
			}
			event := agentproto.Event{Seq: 1, Kind: agentproto.EventTurnStarted, ThreadID: worker.NativeSessionID, TurnID: name + "-turn"}
			hash, e := agentproto.RuntimeEventPayloadHash(event)
			if e != nil {
				return e
			}
			event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: worker.CarrierIncarnation, Sequence: 1, PayloadHash: hash}
			if _, _, e = tx.AcceptRuntimeEvent(worker.InstanceID, event, now, nil); e != nil {
				return e
			}
			pref := work.ContextPreferences{ContextID: navigation.FrontContextID, ScopeID: scope.ID, Source: work.SourceRef{Kind: "sdk_private_ingress", ID: ingress.ID, Version: "1", EvidenceRef: ingress.ID}, UpdatedBy: work.Actor{Kind: work.ActorPrincipal, ID: principal.ID}, Revision: 1, UpdatedAt: now}
			mode := "full-access"
			pref.AccessMode = &mode
			encoded, _ := json.Marshal(pref)
			if _, e = tx.tx.ExecContext(tx.ctx, `INSERT INTO context_preferences(context_id,scope_id,revision,payload) VALUES(?,?,1,?)`, navigation.FrontContextID, scope.ID, string(encoded)); e != nil {
				return e
			}
			chosen := ExecutionPreference{ScopeID: scope.ID, SelectedLocationIDs: []string{location.ID}, DefaultLocationID: location.ID, SelectedBy: work.Actor{Kind: work.ActorPrincipal, ID: principal.ID}, SourceRef: body.ID, Revision: 1}
			if e = tx.PutExecutionPreference(chosen, 0); e != nil {
				return e
			}
			memoryBody := work.NewContent(name+"-memory-body", scope.ID, "text/plain", []byte(name+" private memory"), now)
			if e = tx.PutContent(memoryBody); e != nil {
				return e
			}
			memory := work.MemoryEntry{ID: name + "-memory", StorageScopeID: scope.ID, Scope: work.MemoryScope{Kind: work.MemoryPersonal, PrincipalID: principal.ID, RoleID: "role"}, Author: work.Actor{Kind: work.ActorRole, ID: "role"}, Sources: []work.SourceRef{{Kind: "message", ID: msg.ID, Version: "1", EvidenceRef: body.ID}}, ContentRef: memoryBody.ID, Applicability: "personal", State: work.MemoryInvalidated, Revision: 1, UpdatedAt: now}
			if e = tx.PutMemory(memory, 0); e != nil {
				return e
			}
			launch := work.NewContent(name+"-launch-body", scope.ID, "application/json", []byte(fmt.Sprintf(`{"native_history_namespace":"account:%s","profile_id":"profile","snapshot":{"cwd":"/workspace/%s"}}`, name, name)), now)
			if e = tx.PutContent(launch); e != nil {
				return e
			}
			op := work.Operation{ID: name + "-launch", ScopeID: scope.ID, ContextID: navigation.FrontContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "host"}, Kind: "worker.launch", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: name + "-launch", RequestHash: launch.SHA256, RequestRef: launch.ID, State: work.OperationUnknown, Revision: 1}
			if _, _, e = tx.PutOperation(op, 0); e != nil {
				return e
			}
			output := work.NewContent(name+"-output", scope.ID, "application/json", []byte(`{"text":"pending send"}`), now)
			if e = tx.PutContent(output); e != nil {
				return e
			}
			send := work.Operation{ID: name + "-send", ScopeID: scope.ID, Actor: work.Actor{Kind: work.ActorSystem, ID: "display"}, Kind: "feishu.publish", TargetID: raw.ChatID, TargetGeneration: 1, IdempotencyKey: name + "-uuid", RequestHash: output.SHA256, RequestRef: output.ID, State: work.OperationUnknown, Revision: 1}
			if _, _, e = tx.PutOperation(send, 0); e != nil {
				return e
			}
			if e = tx.PutOutbox(work.Outbox{ID: name + "-outbox", ScopeID: scope.ID, OperationID: send.ID, Target: raw.ChatID, EventID: send.ID, PayloadRef: output.ID, State: work.OutboxUnknown, Revision: 1}, 0); e != nil {
				return e
			}
			sentOp := send
			sentOp.ID, sentOp.IdempotencyKey, sentOp.State = name+"-sent-op", name+"-sent-uuid", work.OperationConfirmed
			sentOp.ExternalReceipt = name + "-sent"
			if _, _, e = tx.PutOperation(sentOp, 0); e != nil {
				return e
			}
			if e = tx.PutOutbox(work.Outbox{ID: name + "-sent-outbox", ScopeID: scope.ID, OperationID: sentOp.ID, Target: raw.ChatID, EventID: sentOp.ID, PayloadRef: output.ID, State: work.OutboxSent, ExternalMessageID: name + "-sent", Revision: 1}, 0); e != nil {
				return e
			}
			viewPayload, _ := json.Marshal(UIView{ID: name + "-view", ScopeID: scope.ID, Tenant: "tenant", ChatID: raw.ChatID, ExternalMessageID: name + "-sent", BodyRef: output.ID})
			if _, e = tx.tx.ExecContext(tx.ctx, `INSERT INTO team_ui_views(id,scope_id,surface_key,revision,payload) VALUES(?,?,?,1,?)`, name+"-view", scope.ID, name+"-surface", string(viewPayload)); e != nil {
				return e
			}
			observation := fmt.Sprintf(`{"event_id":%q,"tenant":"tenant","chat_id":%q,"message_id":%q,"kind":"message.recalled"}`, name+"-recall", raw.ChatID, name+"-sent")
			if _, e = tx.tx.ExecContext(tx.ctx, `INSERT INTO team_platform_observations(id,tenant,chat_id,message_id,kind,payload_hash,payload) VALUES(?,?,?,?,?,?,?)`, name+"-recall", "tenant", raw.ChatID, name+"-sent", "message.recalled", "hash", observation); e != nil {
				return e
			}
		}
		if _, e := tx.tx.ExecContext(tx.ctx, `INSERT INTO native_upgrades(id,host_id,payload) VALUES('upgrade','host',?)`, `{"id":"upgrade","host_id":"host","stage":"installing","target_version":"1","revision":1,"workers":[{"id":"alice-worker"},{"id":"bob-worker"}]}`); e != nil {
			return e
		}
		if e := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "role", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "project-scope", Kind: work.ScopeProject, ProjectID: "project", TeamID: "team", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProject(work.Project{ID: "project", ScopeID: "project-scope", TeamID: "team", Name: "Project", LeaderRoleID: "role", State: work.ProjectActive, Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutContent(work.NewContent("project-secret", "project-scope", "text/plain", []byte("project-only-secret"), now))
	})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot PersonalExport
	err = s.Within(t.Context(), func(tx *Tx) error {
		var e error
		snapshot, e = tx.CapturePersonalExport("alice-scope", PersonalExportLimits{MaxRows: 1000, MaxBytes: 4 << 20})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = snapshot.Verify(); err != nil {
		t.Fatal(err)
	}
	if !snapshot.ReadOnly || snapshot.WritersFenced || snapshot.SourceSchemaVersion != SchemaVersion {
		t.Fatalf("invalid snapshot metadata: %+v", snapshot)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("bob")) || bytes.Contains(raw, []byte("project-only-secret")) {
		t.Fatal("other owner data leaked")
	}
	seen := map[string]bool{}
	var contents = map[string][]byte{}
	for _, r := range snapshot.Records {
		seen[r.Table+":"+r.Key] = true
		if r.Table == "content" {
			contents[r.Key] = r.Data
		}
	}
	for _, key := range []string{"private_navigation:alice-scope", "memories:alice-memory", "outbox:alice-outbox", "outbox:alice-sent-outbox", "workers:alice-worker", "operations:alice-launch", "team_platform_observations:alice-recall", "native_upgrades:upgrade", `runtime_receipts:["alice-carrier",1]`} {
		if !seen[key] {
			t.Errorf("missing %s", key)
		}
	}
	if string(contents["alice-image"]) != "alice image bytes" || !bytes.Contains(contents["alice-launch-body"], []byte(`"native_history_namespace":"account:alice"`)) {
		t.Fatal("media or launch snapshot missing")
	}
	if !strings.Contains(string(raw), `"state":"unknown"`) || !strings.Contains(string(raw), `"access_mode":"full-access"`) {
		t.Fatal("unknown send or preference lost")
	}
	if _, _, err = snapshot.Page(0, 1); err != nil {
		t.Fatal(err)
	}
	snapshot.Records[0].Key = "tampered"
	if _, _, err = snapshot.Page(0, 1); err == nil {
		t.Fatal("mutated manifest page accepted")
	}
	err = s.Within(t.Context(), func(tx *Tx) error {
		_, e := tx.CapturePersonalExport("alice-scope", PersonalExportLimits{MaxRows: 2, MaxBytes: 4 << 20})
		return e
	})
	if err == nil {
		t.Fatal("row limit returned partial success")
	}
	err = s.Within(t.Context(), func(tx *Tx) error {
		_, e := tx.CapturePersonalExport("alice-scope", PersonalExportLimits{MaxRows: 1000, MaxBytes: 16})
		return e
	})
	if err == nil {
		t.Fatal("byte limit returned partial success")
	}
}
