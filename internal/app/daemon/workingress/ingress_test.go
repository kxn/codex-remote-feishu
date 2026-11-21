package workingress

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
)

func TestReceiveCommitsRawBeforeExpansionAndRejectsStorageFailure(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	receipt := receive(t, ingress, rawMessage("leader", "message", "group", "group", "open", "user", "raw survives"))
	txRun(t, store, func(tx *workstore.Tx) error {
		record, err := tx.GetIngress(receipt.IngressID)
		if err != nil {
			return err
		}
		if record.State != work.IngressPending || !strings.Contains(record.Envelope.RawContent, "raw survives") {
			t.Fatal("raw was not durable before receive returned")
		}
		messages, err := tx.ListMessages("scope")
		if err != nil {
			return err
		}
		if len(messages) != 0 {
			t.Fatal("receive expanded/dispatched before background processing")
		}
		return nil
	})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ingress.Receive(t.Context(), rawMessage("leader", "failed", "group", "group", "open", "user", "must fail")); err == nil {
		t.Fatal("storage failure was acknowledged as acceptance")
	}
}
func TestMultipleBotObservationsProduceOneCanonicalMessageAndOneExplicitRecipient(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	first := rawMessage("leader", "same-message", "group", "group", "open-a", "user", "@_user_1 inspect")
	first.Mentions = []work.RawMention{{Key: "@_user_1", IDs: work.SenderIDs{OpenID: "bot:expert"}, Type: "bot", Name: "Expert"}}
	a := receive(t, ingress, first)
	second := first
	second.GatewayRef = "gateway:expert"
	second.AppID = "app:expert"
	second.EventID = "expert-observation"
	second.SenderIDs.OpenID = "open-b"
	b := receive(t, ingress, second)
	if a.IngressID != b.IngressID || !b.Replay {
		t.Fatal("platform canonical key did not deduplicate multiple apps")
	}
	result := process(t, ingress)
	if result.Processed != 1 {
		t.Fatalf("processed=%d", result.Processed)
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		messages, err := tx.ListMessages("scope")
		if err != nil {
			return err
		}
		inputs, err := tx.ListInbox("scope")
		if err != nil {
			return err
		}
		if len(messages) != 1 || len(inputs) != 1 || inputs[0].RecipientRoleID != "expert" {
			t.Fatalf("duplicate/wrong recipient messages=%d inputs=%+v", len(messages), inputs)
		}
		if len(messages[0].Observations) != 2 {
			t.Fatal("app observations lost")
		}
		return nil
	})
	// A late redelivery adds evidence, never another business input.
	second.EventID = "late-expert-event"
	receive(t, ingress, second)
	process(t, ingress)
	txRun(t, store, func(tx *workstore.Tx) error {
		inputs, err := tx.ListInbox("scope")
		if len(inputs) != 1 {
			t.Fatal("late observation duplicated dispatch")
		}
		return err
	})
}
func TestReceiveTimeBindingSnapshotPreventsOldReplayEnteringNewProject(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	env := rawMessage("leader", "old", "group", "group", "open", "user", "belongs to original project")
	receipt := receive(t, ingress, env)
	txRun(t, store, func(tx *workstore.Tx) error { return seedProject(tx, "new-scope", "new-project", "group", 1) })
	process(t, ingress)
	receive(t, ingress, env)
	process(t, ingress)
	txRun(t, store, func(tx *workstore.Tx) error {
		record, err := tx.GetIngress(receipt.IngressID)
		if err != nil {
			return err
		}
		message, err := tx.GetMessage(record.MessageID)
		if err != nil {
			return err
		}
		if message.ScopeID != "scope" {
			t.Fatal("old envelope was rerouted by latest channel binding")
		}
		newInputs, err := tx.ListInbox("new-scope")
		if err != nil {
			return err
		}
		if len(newInputs) != 0 {
			t.Fatal("old replay entered replacement project")
		}
		return nil
	})
}
func TestGroupSlashIsUserInputAndBotFlowNeverBecomesHumanAuthority(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	receive(t, ingress, rawMessage("leader", "human", "group", "group", "open", "user", "/attach should stay ordinary group text"))
	bot := rawMessage("leader", "bot-echo", "group", "group", "bot:leader", "", "/attach")
	bot.SenderType = "app"
	receive(t, ingress, bot)
	result := process(t, ingress)
	if result.Processed != 1 || result.Ignored != 1 {
		t.Fatalf("wrong human/bot routing %+v", result)
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		commands, err := tx.ListPendingPrivateCommands()
		if err != nil {
			return err
		}
		inputs, err := tx.ListInbox("scope")
		if err != nil {
			return err
		}
		if len(commands) != 0 || len(inputs) != 1 {
			t.Fatalf("legacy slash/bot loop commands=%d inputs=%d", len(commands), len(inputs))
		}
		return nil
	})
}
func TestPrivateScopeDistinguishesPrincipalAndRole(t *testing.T) {
	for _, stable := range []bool{false, true} {
		t.Run(map[bool]string{false: "no cross-app proof", true: "verified tenant user ID"}[stable], func(t *testing.T) {
			ingress, store, _ := ingressFixture(t, false)
			user := ""
			if stable {
				user = "same-tenant-user"
			}
			receive(t, ingress, rawMessage("leader", "a", "private-a", "p2p", "same-open-id", user, "person one leader"))
			receive(t, ingress, rawMessage("expert", "b", "private-b", "p2p", "same-open-id", user, "person one expert"))
			receive(t, ingress, rawMessage("leader", "c", "private-c", "p2p", "other-open-id", "other-user", "person two leader"))
			process(t, ingress)
			txRun(t, store, func(tx *workstore.Tx) error {
				scopes, err := tx.ListScopes()
				if err != nil {
					return err
				}
				if len(scopes) != 3 {
					t.Fatalf("private scopes=%d", len(scopes))
				}
				principals := map[string]string{}
				for _, scope := range scopes {
					messages, err := tx.ListMessages(scope.ID)
					if err != nil {
						return err
					}
					if len(messages) != 1 {
						t.Fatal("private histories were mixed")
					}
					body, err := tx.GetContent(scope.ID, messages[0].BodyRef)
					if err != nil {
						return err
					}
					principals[string(body.Data)] = scope.PrincipalID
				}
				same := principals["person one leader"] == principals["person one expert"]
				if same != stable {
					t.Fatalf("app aliases auto-merged=%v proof=%v", same, stable)
				}
				if principals["person two leader"] == principals["person one leader"] {
					t.Fatal("last speaker changed personal ownership")
				}
				return nil
			})
		})
	}
}
func TestUnresolvedLeaderIsDurableRecoverableSelection(t *testing.T) {
	ingress, store, directory := ingressFixture(t, false)
	directory.leader = ""
	directory.leaderErr = errors.New("multiple configured leaders")
	receipt := receive(t, ingress, rawMessage("expert", "unselected", "new-group", "group", "open", "user", "Please establish a project"))
	result, err := ingress.ProcessPending(t.Context(), 10)
	if err != nil || len(result.Selections) != 1 {
		t.Fatalf("no explicit selection state %+v %v", result, err)
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		record, err := tx.GetIngress(receipt.IngressID)
		if err != nil {
			return err
		}
		if record.State != work.IngressPending || record.NextAttemptAt == nil {
			t.Fatal("unselected input was lost or endlessly hot retried")
		}
		return nil
	})
	directory.leader = "leader"
	directory.leaderErr = nil
	if err = ingress.RetryIngress(t.Context(), receipt.IngressID); err != nil {
		t.Fatal(err)
	}
	process(t, ingress)
}
