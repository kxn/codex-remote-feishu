package privatechat

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRequestInventoryRestoresOnlyProvenSameCarrier(t *testing.T) {
	for _, tc := range []string{"valid", "missing-capability", "incomplete", "incomplete-response-intent", "foreign-carrier", "forged-start", "resolved", "response-intent", "unknown-visibility"} {
		t.Run(tc, func(t *testing.T) {
			ctx := context.Background()
			o := Owner{"g", "s", "c", "u", "tenant", "app"}
			native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
			j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			l, err := j.OpenNativeLease(ctx, testNativeAuthority{}, native, o, 1)
			if err != nil {
				t.Fatal(err)
			}
			seed := New(nil, orchestrator.Config{})
			if err = seed.RegisterOwner(o); err != nil {
				t.Fatal(err)
			}
			cp, err := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
			if err != nil {
				t.Fatal(err)
			}
			cp.AttachedInstanceID = native.InstanceID
			req := &state.RequestPromptRecord{RequestID: "request", RequestType: "approval", InstanceID: native.InstanceID, ThreadID: "thread", TurnID: "turn", OwnerSurfaceSessionID: o.SurfaceID, OwnerGatewayID: o.GatewayID, OwnerChatID: o.ChatID, Title: "确认", VisibilityState: "delivery_degraded", LifecycleState: "awaiting_visibility", NeedsRedelivery: true}
			frozen, _ := json.Marshal(struct {
				Requests map[string]*state.RequestPromptRecord
				Order    []string
			}{map[string]*state.RequestPromptRecord{"request": req}, []string{"request"}})
			cp.Holds = []orchestrator.PrivateRecoveryHold{{Kind: "requests", InstanceID: native.InstanceID, Frozen: frozen}}
			if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: cp}, 0, nil); err != nil {
				t.Fatal(err)
			}
			p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
			if err != nil {
				t.Fatal(err)
			}
			if err = p.RegisterNativeHello(ctx, l, nativeHello(native)); err != nil {
				t.Fatal(err)
			}
			binding := agentproto.NativeTurnBinding{Version: 1, Owner: native}
			start := persistNative(t, ctx, l, binding, 1, agentproto.Event{Kind: agentproto.EventRequestStarted, RequestID: "request", ThreadID: "thread", TurnID: "turn"})
			if _, err = j.db.Exec(`UPDATE native_events SET applied=1,applied_revision=1 WHERE incarnation=? AND sequence=?`, start.Incarnation, start.Sequence); err != nil {
				t.Fatal(err)
			}
			if tc == "response-intent" || tc == "incomplete-response-intent" {
				body, encodeErr := EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindAgentCommand, Command: &agentproto.Command{CommandID: "response", Kind: agentproto.CommandRequestRespond, Request: agentproto.Request{RequestID: "request"}}})
				if encodeErr != nil {
					t.Fatal(encodeErr)
				}
				_, err = j.db.Exec(`INSERT INTO private_effect(id,tenant,app_id,gateway,surface,chat,user_id,generation,revision,ordinal,lane,body,state) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, "response-effect", o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, 1, 100, 0, "interactive", body, "prepared")
				if err != nil {
					t.Fatal(err)
				}
			}
			if tc == "unknown-visibility" {
				body, encodeErr := EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindRequest, RequestView: &control.FeishuRequestView{RequestID: "request"}})
				if encodeErr != nil {
					t.Fatal(encodeErr)
				}
				_, err = j.db.Exec(`INSERT INTO private_effect(id,tenant,app_id,gateway,surface,chat,user_id,generation,revision,ordinal,lane,body,state) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, "old-card", o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, 1, 100, 0, "interactive", body, "unknown")
				if err != nil {
					t.Fatal(err)
				}
			}
			if tc == "resolved" {
				resolved := persistNative(t, ctx, l, binding, 2, agentproto.Event{Kind: agentproto.EventRequestResolved, RequestID: "request", ThreadID: "thread", TurnID: "turn"})
				if _, err = j.db.Exec(`UPDATE native_events SET applied=1,applied_revision=1 WHERE incarnation=? AND sequence=?`, resolved.Incarnation, resolved.Sequence); err != nil {
					t.Fatal(err)
				}
			}
			command := agentproto.Command{CommandID: "inspect", Kind: agentproto.CommandRuntimeInspect, NativeBinding: &binding, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: 1, ExpectedInstanceGeneration: 1, ExpectedProcessGeneration: 1, IncludePendingRequests: true}}
			d := Dispatch{ID: "inspect", Owner: o, Generation: 1, CommandID: "inspect", Lane: "inspect", Mode: "start", Session: NativeSession{Backend: "codex", Namespace: "history", CarrierID: "inc", CarrierGeneration: 1}}
			if err = l.PrepareHostInspection(ctx, d, command); err != nil {
				t.Fatal(err)
			}
			if err = l.MarkSending(ctx, d.ID); err != nil {
				t.Fatal(err)
			}
			seq := uint64(2)
			watermark := uint64(1)
			if tc == "resolved" {
				seq = 3
				watermark = 2
			}
			inv := &agentproto.RuntimePendingRequestInventory{Version: 1, Complete: true, CarrierIncarnation: "inc", InstanceGeneration: 1, ProcessGeneration: 1, EventSequence: watermark, Requests: []agentproto.RuntimePendingRequest{{RequestID: "request", ThreadID: "thread", TurnID: "turn", StartedSequence: 1, StartedHash: start.PayloadHash}}}
			if tc == "missing-capability" {
				inv = nil
			}
			if tc == "incomplete" || tc == "incomplete-response-intent" {
				inv.Complete = false
			}
			if tc == "foreign-carrier" {
				inv.CarrierIncarnation = "foreign"
			}
			if tc == "forged-start" {
				inv.Requests[0].StartedHash = "forged"
			}
			event := agentproto.Event{Kind: agentproto.EventRuntimeInspectionCompleted, CommandID: "inspect", NativeBinding: &binding, RuntimeInspection: &agentproto.RuntimeInspectionResult{Version: 1, RequestCommandID: "inspect", CarrierIncarnation: "inc", InstanceGeneration: 1, ProcessGeneration: 1, PendingRequests: inv}}
			ref := persistNative(t, ctx, l, binding, seq, event)
			effects, err := p.ApplyNative(ctx, l, ref, CoreNativeReducer{})
			if tc == "foreign-carrier" || tc == "forged-start" {
				if err == nil {
					t.Fatal("invalid proof accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, effect := range effects {
				e, err := DecodeEffectEvent(effect.Effect.Body)
				if err != nil {
					t.Fatal(err)
				}
				if e.RequestView != nil {
					count++
				}
			}
			if (count == 1) != (tc == "valid") {
				t.Fatalf("unexpected replay count %d", count)
			}
			if tc == "incomplete" {
				now := time.Now().Add(10 * time.Second)
				due, err := p.PrivateTickDue(ctx, l, now)
				if err != nil || !due {
					t.Fatalf("incomplete inventory did not schedule retry: due=%v err=%v", due, err)
				}
				retry, err := p.ApplyPrivateTick(ctx, l, "inventory-retry", now)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, effect := range retry {
					ev, err := DecodeEffectEvent(effect.Effect.Body)
					if err != nil {
						t.Fatal(err)
					}
					if ev.Command != nil && ev.Command.RuntimeInspection != nil && ev.Command.RuntimeInspection.IncludePendingRequests {
						found = true
					}
				}
				if !found {
					t.Fatal("retry did not produce request inventory inspection")
				}
			}
			if tc == "incomplete-response-intent" {
				due, err := p.requestInventoryRetryDue(ctx, l, time.Now().Add(10*time.Second))
				if err != nil || due {
					t.Fatalf("response intent must prevent reinspection: due=%v err=%v", due, err)
				}
			}
			if tc == "missing-capability" {
				a := privateAction(o, control.ActionStatus)
				seq := admittedAction(t, j, o, "status", "control", a)
				status, err := p.ApplyIngress(ctx, o, 1, seq)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, effect := range status {
					ev, err := DecodeEffectEvent(effect.Effect.Body)
					if err != nil {
						t.Fatal(err)
					}
					if ev.Notice != nil && ev.Notice.Code == "private_request_inventory_unsupported" {
						found = true
					}
				}
				if !found {
					t.Fatal("old wrapper status did not explain recovery exit")
				}
			}
		})
	}
}
