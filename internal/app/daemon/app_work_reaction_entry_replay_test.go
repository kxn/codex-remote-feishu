package daemon

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Exercise the actual authenticated App entry, including its enabled-app lookup
// and replay guard. Calling Accept+Register directly would bypass this boundary.
func TestPrivateReactionEntryReplayCannotAcquireLateSourceOrIdentity(t *testing.T) {
	for _, kind := range []string{"raw-source-late", "identity-late"} {
		t.Run(kind, func(t *testing.T) {
			gatewayID := "gateway"
			appID := isolatedSDKFixtureAppID(t, "entry-replay")
			a, source := teamPickerFixture(t, appID, teamID("role", gatewayID))
			cfg := config.AppConfig{Feishu: config.FeishuSettings{Apps: []config.FeishuAppConfig{{ID: gatewayID, AppID: appID, AppSecret: "fixture-only"}}}}
			a.admin.loadConfig = func() (config.LoadedAppConfig, error) { return config.LoadedAppConfig{Config: cfg}, nil }
			f := seedReactionActionFixture(t, a, source, work.BackendCodex)
			ids := source.SenderIDs
			operator := ids
			if kind == "identity-late" {
				operator = work.SenderIDs{UnionID: "verified-only-by-later-sdk-message"}
				ids.UnionID = operator.UnionID
			}
			event := feishu.PlatformIngressEvent{GatewayRef: gatewayID, AppID: appID, EventID: "early-unusable-reaction", Tenant: source.Tenant, ChatID: source.ChatID, MessageID: "late-sdk-source", Kind: "reaction.created", OperatorType: "user", OperatorIDs: operator, Emoji: "THUMBSUP", OccurredAt: time.Now().UTC(), ReceivedAt: time.Now().UTC()}
			if err := a.receiveTeamPlatformEvent(t.Context(), event); err != nil {
				t.Fatal(err)
			}
			alias := work.IdentityScope{Tenant: source.Tenant, AppIdentityDomain: appID, IDType: "union_id", ExternalID: operator.UnionID, Generation: source.Gateway.BotGeneration}
			if kind == "identity-late" {
				pickerTx(t, a, func(tx *workstore.Tx) error {
					if _, e := tx.GetExternalIdentity(alias); !errors.Is(e, workstore.ErrNotFound) {
						t.Fatal("reaction manufactured an unknown identity alias")
					}
					return nil
				})
			}
			ingress, err := workingress.New(a.teamRuntime.store, workingress.Config{Directory: steerSDKDirectory{source.Gateway}, Expander: steerMediaExpander{}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = ingress.Receive(t.Context(), work.RawIngressEnvelope{GatewayRef: gatewayID, AppID: appID, EventID: "later-sdk-body", Tenant: event.Tenant, ChatID: event.ChatID, ChatType: "p2p", MessageID: event.MessageID, SenderType: "user", SenderIDs: ids, MessageType: "text", RawContent: `{"text":"a later source for an earlier ignored reaction"}`, ReceivedAt: time.Now().UTC()})
			if err != nil {
				t.Fatal(err)
			}
			if kind == "identity-late" {
				pickerTx(t, a, func(tx *workstore.Tx) error {
					identity, e := tx.GetExternalIdentity(alias)
					if e != nil {
						return e
					}
					if identity.PrincipalID != source.PrincipalID {
						t.Fatal("actual SDK observation did not bind the intended existing principal")
					}
					return nil
				})
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				old, e := tx.GetNativeTurn("steer-carrier", "steer-session", "steer-turn")
				if e != nil {
					return e
				}
				old.TerminalStatus = "completed"
				old.TerminalSequence = 100
				old.LastSequence = 100
				if e = tx.PutNativeTurn(old); e != nil {
					return e
				}
				old.TurnID = "later-native-replay-target"
				old.TerminalStatus = ""
				old.TerminalSequence = 0
				old.LastSequence = 101
				return tx.PutNativeTurn(old)
			})
			// The identical SDK EventID is redelivered only after the missing evidence
			// and a different runnable turn exist. It must remain an ignored observation.
			if err = a.receiveTeamPlatformEvent(t.Context(), event); err != nil {
				t.Fatal(err)
			}
			result, err := ingress.ProcessPending(t.Context(), 16)
			if err != nil || len(result.Failures) > 0 {
				t.Fatalf("source processing: %+v %v", result, err)
			}
			sends := 0
			f.relay.send = func(string, agentproto.Command) error { sends++; return nil }
			if err = a.runTeamPrivateReactionActions(t.Context()); err != nil {
				t.Fatal(err)
			}
			if sends != 0 {
				t.Fatal("redelivery upgraded ignored reaction into authority for a later source/turn")
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				facts := event
				facts.ReceivedAt = time.Time{}
				raw, _ := json.Marshal(facts)
				id, replay, e := tx.AcceptPlatformObservation(workstore.PlatformObservation{Gateway: source.Gateway, EventID: event.EventID, Tenant: event.Tenant, ChatID: event.ChatID, MessageID: event.MessageID, Kind: event.Kind, Facts: raw, ReceivedAt: event.ReceivedAt})
				if e != nil {
					return e
				}
				if !replay {
					t.Fatal("SDK event redelivery was not deduplicated")
				}
				if _, e = tx.GetPrivateReactionAction(id); !errors.Is(e, workstore.ErrNotFound) {
					t.Fatal("ignored event acquired a deferred action after redelivery")
				}
				inputs, e := tx.ListPendingInputs(source.ScopeID, source.RoleID)
				if e != nil {
					return e
				}
				found := false
				for _, input := range inputs {
					message, e := tx.GetMessage(input.MessageID)
					if e != nil {
						return e
					}
					if message.PlatformKey != nil && message.PlatformKey.MessageID == event.MessageID {
						found = true
						if input.State != work.InboxPending {
							t.Fatal("ignored reaction consumed late source")
						}
					}
				}
				if !found {
					t.Fatal("late source vanished instead of remaining queued")
				}
				return nil
			})
		})
	}
}
