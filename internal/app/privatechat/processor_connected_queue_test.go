package privatechat

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestAuthenticatedHelloRestoresProvenQueuedFIFO(t *testing.T) {
	for _, mode := range []string{"proven", "unproven", "accepted", "claimed"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			o := Owner{"g", "s", "c", "u", "tenant", "app"}
			n := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
			path := filepath.Join(t.TempDir(), "private.db")
			j, err := OpenJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			var seqs []int64
			for _, id := range []string{"m1", "m2"} {
				a := privateAction(o, control.ActionTextMessage)
				a.MessageID = id
				a.Text = id
				seq := admittedAction(t, j, o, id, "input", a)
				seqs = append(seqs, seq)
				if mode != "unproven" || id != "m1" {
					if _, err = j.db.Exec(`INSERT INTO private_applied(seq,revision) VALUES(?,1)`, seq); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "accepted" {
				_, err = j.db.Exec(`INSERT INTO dispatch(id,gateway,tenant,app_id,surface,chat,user_id,generation,lane,mode,backend,namespace,session,carrier,carrier_generation,command_id,state) VALUES('d',?,?,?,?,?,?,1,'input','start','codex','history','thread','inc',1,'command','accepted')`, o.GatewayID, o.TenantID, o.AppID, o.SurfaceID, o.ChatID, o.UserID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = j.db.Exec(`INSERT INTO dispatch_input(dispatch_id,seq) VALUES('d',?)`, seqs[0]); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "claimed" {
				_, err = j.db.Exec(`INSERT INTO private_migration_imports(import_id,source_scope,source_hash,proof_hash,tenant,app_id,gateway,surface,chat,user_id,generation,receipt) VALUES('i','source','hash','proof',?,?,?,?,?,?,1,'{}')`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = j.db.Exec(`INSERT INTO private_migration_input_holds(import_id,inbox_id,message_id,reason,payload_hash) VALUES('i','old','m1','claimed','hash')`); err != nil {
					t.Fatal(err)
				}
			}
			seed := orchestrator.NewService(nil, orchestrator.Config{PrivateApplication: true}, nil)
			seed.MaterializeSurface(o.SurfaceID, o.GatewayID, o.ChatID, o.UserID)
			core, err := seed.ExportPrivateCheckpoint(o.SurfaceID)
			if err != nil {
				t.Fatal(err)
			}
			core.Version = 2
			core.Backend = agentproto.BackendCodex
			core.RouteMode = state.RouteModePinned
			core.AttachedInstanceID = n.InstanceID
			core.SelectedThreadID = "thread"
			core.ClaimedWorkspaceKey = "/tmp/workspace"
			core.Navigation = &orchestrator.PrivateNavigationSnapshot{Owner: n, ThreadID: "thread", CWD: "/tmp/workspace", WorkspaceKey: "/tmp/workspace"}
			core.NextQueueID = 2
			core.QueuedQueueItemIDs = []string{"queue-1", "queue-2"}
			core.QueueItems = map[string]*state.QueueItemRecord{}
			for i, id := range []string{"m1", "m2"} {
				q := core.QueuedQueueItemIDs[i]
				core.QueueItems[q] = &state.QueueItemRecord{ID: q, SurfaceSessionID: o.SurfaceID, ActorUserID: o.UserID, SourceKind: state.QueueItemSourceUser, SourceMessageID: id, SourceMessageIDs: []string{id}, Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: id}}, Status: state.QueueItemQueued}
			}
			if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Cursor: seqs[1], Core: core}, 0, nil); err != nil {
				t.Fatal(err)
			}
			if err = j.Close(); err != nil {
				t.Fatal(err)
			}
			j, err = OpenJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, n, o, 1)
			if err != nil {
				t.Fatal(err)
			}
			p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
			if err != nil {
				t.Fatal(err)
			}
			effects, err := p.ApplyAuthenticatedNativeHello(ctx, lease, nativeHello(n), "authenticated", orchestrator.PrivateNativeProfiles{})
			if err != nil {
				t.Fatal(err)
			}
			var commands []agentproto.Command
			for _, effect := range effects {
				event, _, e := DecodeStoredEffect(effect.Effect.Body)
				if e != nil {
					t.Fatal(e)
				}
				if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
					commands = append(commands, *event.Command)
				}
			}
			if mode != "proven" {
				if len(commands) != 0 {
					t.Fatalf("unproven input dispatched: %+v", commands)
				}
				return
			}
			if len(commands) != 1 || len(commands[0].Prompt.Inputs) != 1 || commands[0].Prompt.Inputs[0].Text != "m1" {
				t.Fatalf("FIFO commands=%+v", commands)
			}
		})
	}
}
