package workingress

import (
	"context"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
	"time"
)

type failedExpander struct{}

func (failedExpander) Expand(context.Context, work.RawIngressEnvelope) (ExpandedMessage, error) {
	return ExpandedMessage{}, errors.New("attachment download failed")
}
func TestExpansionFailureStillProducesRecoverablePartialInput(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	ingress.config.Expander = failedExpander{}
	env := rawMessage("leader", "file", "group", "group", "open", "user", "")
	env.MessageType = "file"
	env.RawContent = `{"file_key":"actual-file-key","file_name":"design.pdf"}`
	receipt := receive(t, ingress, env)
	result := process(t, ingress)
	if result.Partial != 1 || result.Processed != 1 {
		t.Fatalf("missing partial input %+v", result)
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		record, err := tx.GetIngress(receipt.IngressID)
		if err != nil {
			return err
		}
		if record.Envelope.RawContent != env.RawContent {
			t.Fatal("raw attachment envelope lost")
		}
		message, err := tx.GetMessage(record.MessageID)
		if err != nil {
			return err
		}
		body, err := tx.GetContent(message.ScopeID, message.BodyRef)
		if err != nil {
			return err
		}
		inputs, err := tx.ListInbox(message.ScopeID)
		if err != nil {
			return err
		}
		if len(inputs) != 1 || inputs[0].ContentState != work.InputContentPartial || !strings.Contains(string(body.Data), "actual-file-key") || !strings.Contains(string(body.Data), "部分内容未获取") {
			t.Fatal("attachment failure was dropped or fabricated complete")
		}
		return nil
	})
}

type privateHandler struct {
	calls   int
	unknown bool
}

func (h *privateHandler) Handle(context.Context, work.TypedCommandEnvelope) (PrivateCommandReceipt, error) {
	h.calls++
	return PrivateCommandReceipt{Receipt: "command-receipt", Unknown: h.unknown}, nil
}
func TestPrivateSlashHasOneTypedExecutionOwnerAndUnknownDoesNotReplay(t *testing.T) {
	ingress, store, _ := ingressFixture(t, false)
	handler := &privateHandler{unknown: true}
	ingress.config.PrivateCommands = handler
	receive(t, ingress, rawMessage("leader", "slash", "private", "p2p", "open", "user", "/status"))
	result := process(t, ingress)
	if result.Commands != 1 || result.Processed != 0 {
		t.Fatalf("slash entered model lane %+v", result)
	}
	for range 2 {
		if _, err := ingress.DispatchPrivateCommands(t.Context(), 10); err != nil {
			t.Fatal(err)
		}
	}
	if handler.calls != 1 {
		t.Fatal("unknown legacy command was automatically repeated")
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		scopes, err := tx.ListScopes()
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			inputs, err := tx.ListInbox(scope.ID)
			if err != nil {
				return err
			}
			if len(inputs) != 0 {
				t.Fatal("private slash had both command and model owners")
			}
		}
		return nil
	})
}
func TestBootstrapFirstPrincipalAndExplicitLeaderSurviveOtherObservers(t *testing.T) {
	ingress, store, directory := ingressFixture(t, false)
	directory.leader = ""
	directory.leaderErr = errors.New("must explicitly choose leader")
	first := rawMessage("expert", "first", "new-group", "group", "first-open", "first-user", "@_leader establish a project")
	first.Mentions = []work.RawMention{{Key: "@_leader", IDs: work.SenderIDs{OpenID: "bot:leader"}, Type: "bot", Name: "Leader"}}
	receive(t, ingress, first)
	second := rawMessage("expert", "second", "new-group", "group", "second-open", "second-user", "Also consider this detail")
	receive(t, ingress, second)
	result := process(t, ingress)
	if result.Processed != 2 {
		t.Fatalf("bootstrap inputs %+v", result)
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		scope, err := tx.FindBootstrapScope("tenant", "new-group", 0)
		if err != nil {
			return err
		}
		if scope.RoleID != "leader" {
			t.Fatal("first observer silently became leader")
		}
		firstIdentity, err := tx.GetExternalIdentity(work.IdentityScope{Tenant: "tenant", AppIdentityDomain: "tenant", IDType: "user_id", ExternalID: "first-user", Generation: 1})
		if err != nil {
			return err
		}
		if scope.PrincipalID != firstIdentity.PrincipalID {
			t.Fatal("last speaker replaced establishing principal")
		}
		inputs, err := tx.ListInbox(scope.ID)
		if err != nil {
			return err
		}
		if len(inputs) != 2 {
			t.Fatal("bootstrap did not reuse established scope")
		}
		return nil
	})
}
func TestBootstrapActualSDKInputIsAdoptedByProjectExactlyOnce(t *testing.T) {
	ingress, store, _ := ingressFixture(t, false)
	receipt := receive(t, ingress, rawMessage("leader", "first", "new-group", "group", "open", "user", "Actual original human requirement"))
	process(t, ingress)
	var source work.Message
	var scope work.WorkScope
	txRun(t, store, func(tx *workstore.Tx) error {
		record, err := tx.GetIngress(receipt.IngressID)
		if err != nil {
			return err
		}
		source, err = tx.GetMessage(record.MessageID)
		if err != nil {
			return err
		}
		scope, err = tx.GetScope(source.ScopeID)
		return err
	})
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return ingressNow })
	if err != nil {
		t.Fatal(err)
	}
	service, err := workservice.New(store, signer, func() time.Time { return ingressNow })
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Issue(workauth.Claims{Purpose: "bootstrap", RoleID: scope.RoleID, BotGeneration: 1, Tenant: scope.Tenant, ChatID: scope.ChatID, PrincipalID: scope.PrincipalID, OperationID: scope.BootstrapOperationID}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.CreateProject(t.Context(), token, workservice.CreateProjectRequest{Key: scope.BootstrapOperationID, Tenant: scope.Tenant, ChatID: scope.ChatID, PrincipalID: scope.PrincipalID, Name: "New project", TeamID: "team", LeaderRoleID: scope.RoleID, SourceMessageID: source.ID, InitialRequest: "Model summary is not the original human input"})
	if err != nil {
		t.Fatal(err)
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		input := workstore.AdoptBootstrapInputRequest{OperationID: project.OperationID, SourceMessageID: source.ID}
		derived, created, err := tx.AdoptBootstrapInput(input)
		if err != nil {
			return err
		}
		if !created || derived.PlatformKey != nil || derived.DerivedFrom == nil || derived.Source != source.Source {
			t.Fatal("project input lost human provenance")
		}
		body, err := tx.GetContent(derived.ScopeID, derived.BodyRef)
		if err != nil {
			return err
		}
		if string(body.Data) != "Actual original human requirement" {
			t.Fatal("model summary replaced source message")
		}
		_, created, err = tx.AdoptBootstrapInput(input)
		if err != nil {
			return err
		}
		if created {
			t.Fatal("bootstrap task duplicated on saga replay")
		}
		return nil
	})
}

func TestLaterLeaderChoiceDoesNotReplaceOriginalEstablishingPrincipal(t *testing.T) {
	ingress, store, directory := ingressFixture(t, false)
	directory.leader = ""
	directory.leaderErr = errors.New("choose leader")
	receive(t, ingress, rawMessage("expert", "original-first", "new-group", "group", "first-open", "first-user", "Original first request"))
	choice := rawMessage("leader", "later-choice", "new-group", "group", "second-open", "second-user", "@_leader use this configured colleague")
	choice.Mentions = []work.RawMention{{Key: "@_leader", IDs: work.SenderIDs{OpenID: "bot:leader"}, Type: "bot", Name: "Leader"}}
	receive(t, ingress, choice)
	result := process(t, ingress)
	if result.Processed != 2 || len(result.Selections) != 0 {
		t.Fatalf("resolved choice left original request stranded: %+v", result)
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		scope, err := tx.FindBootstrapScope("tenant", "new-group", 0)
		if err != nil {
			return err
		}
		original, err := tx.GetExternalIdentity(work.IdentityScope{Tenant: "tenant", AppIdentityDomain: "tenant", IDType: "user_id", ExternalID: "first-user", Generation: 1})
		if err != nil {
			return err
		}
		if scope.PrincipalID != original.PrincipalID {
			t.Fatal("later selector became owner by last-speaker inference")
		}
		inputs, err := tx.ListInbox(scope.ID)
		if err != nil {
			return err
		}
		if len(inputs) != 2 {
			t.Fatal("original source was not released for processing after leader choice")
		}
		return nil
	})
}
