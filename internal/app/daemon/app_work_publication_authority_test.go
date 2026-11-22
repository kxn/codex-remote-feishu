package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhttp"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/teamcli"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type publicationTestDirectory struct {
	deliveryJourneyDirectory
}

func (d publicationTestDirectory) ResolveMention(_ context.Context, _ work.IngressGateway, mention work.RawMention) (string, error) {
	if mention.IDs.OpenID == "ou_expert" {
		return "expert", nil
	}
	return "", nil
}

func TestPublicationAuthorityThroughCLIIngressAndSDK(t *testing.T) {
	ctx := t.Context()
	leaderApp := isolatedSDKFixtureAppID(t, "publication-leader")
	expertApp := isolatedSDKFixtureAppID(t, "publication-expert")
	f := newResourceRecoveryFixture(t, leaderApp)
	a, r := f.a, f.a.teamRuntime
	identity := work.IngressGateway{GatewayRef: "gateway", AppID: leaderApp, RoleID: "leader", BotGeneration: 1}
	task := work.Task{ID: "publication-task", ScopeID: "scope", Title: "Prepare a report", OwnerRoleID: "expert", State: work.TaskActive, RequirementsRevision: 1, AssignmentGeneration: 1, Revision: 1}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		if err := tx.PutRole(work.Role{ID: "expert", Name: "Expert", Description: "Investigate", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "expert", GatewayRef: "expert-gateway", AppID: expertApp, IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: "expert", Active: true, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutProjectMember(work.ProjectMember{ProjectID: "project", RoleID: "expert", State: work.ProjectMemberReady, BotGeneration: 1, Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutTask(task, 0)
	})
	type sentMessage struct {
		Path, Kind, Content, ChatID, UUID string
		Thread                            bool
	}
	messages := make(chan sentMessage, 16)
	uploads := make(chan string, 4)
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"publication-test-token","expire":7200}`)
		case req.URL.Path == "/open-apis/im/v1/files":
			file, _, err := req.FormFile("file")
			if err != nil {
				t.Error(err)
				http.Error(w, "expected a file", http.StatusBadRequest)
				return
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil {
				t.Error(err)
			}
			uploads <- string(data)
			fmt.Fprint(w, `{"code":0,"data":{"file_key":"private_asset_key"}}`)
		case strings.HasPrefix(req.URL.Path, "/open-apis/im/v1/messages"):
			var body struct {
				Kind    string `json:"msg_type"`
				Content string `json:"content"`
				Receive string `json:"receive_id"`
				Thread  bool   `json:"reply_in_thread"`
				UUID    string `json:"uuid"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.UUID == "" {
				t.Errorf("invalid platform write: %+v %v", body, err)
			}
			chat := body.Receive
			if strings.Contains(req.URL.Path, "/private-input/") {
				chat = "private-chat"
			} else if chat == "" {
				chat = "chat"
			}
			messages <- sentMessage{req.URL.Path, body.Kind, body.Content, chat, body.UUID, body.Thread}
			if strings.Contains(body.Content, "Lose this publication receipt") {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"code":99991600,"msg":"result unknown"}`)
				return
			}
			fmt.Fprintf(w, `{"code":0,"data":{"message_id":%q,"chat_id":%q}}`, "sent-"+body.UUID, chat)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(platform.Close)
	useDeliveryJourneyGateway(t, a, platform.URL, identity)
	controller := a.gateway.(*feishu.MultiGatewayController)
	if err := controller.UpsertApp(ctx, feishu.GatewayAppConfig{GatewayID: "expert-gateway", AppID: expertApp, AppSecret: "fixture-only", BotOpenID: "ou_expert", Domain: platform.URL, Enabled: true, ImageTempDir: t.TempDir(), PreviewCacheDir: t.TempDir(), PreviewStatePath: filepath.Join(t.TempDir(), "preview.json")}); err != nil {
		t.Fatal(err)
	}
	ingress, err := workingress.New(r.store, workingress.Config{Directory: publicationTestDirectory{deliveryJourneyDirectory{identity}}})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(workhttp.NewHandler(r.service))
	t.Cleanup(endpoint.Close)
	r.service.WithArtifactHost("local-delivery-host")
	leader := publicationJourneyCLI(t, f, endpoint.URL, "leader", startDeliveryJourneyRun(t, r.store, t.TempDir(), "publication-leader", ""))
	dir := t.TempDir()
	expert := publicationJourneyCLI(t, f, endpoint.URL, "expert", startPublicationJourneyRun(t, r.store, dir, "publication-expert", "", "scope", "expert", task))
	assertPublicationCommands(t, expert, []string{"resource.file.save", "message.send"}, []string{"message.publish", "delivery.publish", "media.publish", "publication.grant"})
	assertPublicationCommands(t, leader, []string{"message.publish", "delivery.publish", "publication.grant"}, []string{"media.publish"})
	assertForbiddenPublication(t, expert("message.publish", workservice.PublishMessageRequest{Key: "bypass", Body: "Publish me", TaskID: task.ID}, nil))
	assertForbiddenPublication(t, expert("publication.grant", workservice.GrantPublicationRequest{Key: "self-grant", GranteeRoleID: "expert", TaskID: task.ID, ContentKind: "message"}, nil))
	decision := workservice.RequestDecisionRequest{
		Key: "internal-decision", Title: "Confirm report", Prompt: "Which result should the team use?",
		Choices: []work.DecisionChoice{{Label: "First", Value: "first"}, {Label: "Second", Value: "second"}},
	}
	if err = expert("decision.request", decision, nil); err != nil {
		t.Fatalf("expert's internal decision request: %v", err)
	}
	if err = expert("message.send", workservice.SendMessageRequest{Key: "internal", RecipientRoleID: "leader", ConversationID: "collaboration", Body: "Internal handoff"}, nil); err != nil {
		t.Fatalf("internal handoff: %v", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "report.txt"), []byte("Saved expert result"), 0600); err != nil {
		t.Fatal(err)
	}
	var saved workservice.SavedResourceFile
	if err = expert("resource.file.save", workservice.SaveResourceFileRequest{Key: "save", Path: "report.txt", CollectionID: "deliverables", Purpose: "Report"}, &saved); err != nil {
		t.Fatal(err)
	}
	refs := []work.DeliveryRef{{ResourceID: saved.ResourceID, VersionID: saved.VersionID}}
	delivery := workservice.PublishDeliveryRequest{Key: "publication", TaskID: task.ID, Purpose: "Report", Summary: "Verified", References: refs, Audience: []work.Actor{{Kind: work.ActorRole, ID: "leader"}}, Retention: work.RetentionPermanent}
	assertForbiddenPublication(t, expert("delivery.publish", delivery, nil))
	var grant workservice.Result
	if err = leader("publication.grant", workservice.GrantPublicationRequest{Key: "grant", GranteeRoleID: "expert", TaskID: task.ID, ContentKind: "delivery", References: refs}, &grant); err != nil {
		t.Fatal(err)
	}
	assertPublicationCommands(t, expert, []string{"delivery.publish"}, []string{"message.publish", "publication.grant"})
	delivery.GrantID = grant.ObjectID
	wrong := delivery
	wrong.Key, wrong.References = "wrong-version", []work.DeliveryRef{{ResourceID: saved.ResourceID, VersionID: "wrong"}}
	assertForbiddenPublication(t, expert("delivery.publish", wrong, nil))
	var queued workservice.Result
	if err = expert("delivery.publish", delivery, &queued); err != nil || queued.Status != "accepted" {
		t.Fatalf("valid delegation did not queue: %+v %v", queued, err)
	}
	duplicate := delivery
	duplicate.Key = "duplicate-pending-delivery"
	assertForbiddenPublication(t, expert("delivery.publish", duplicate, nil))
	if err = leader("publication.revoke", workservice.RevokePublicationRequest{Key: "revoke", GrantID: grant.ObjectID}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = r.publisher.RunOnce(ctx, 16); err != nil {
		t.Fatalf("process revoked outbox: %v", err)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		d, err := tx.GetDelivery("scope", queued.ObjectID)
		if err != nil {
			return err
		}
		if _, err = tx.GetSentDelivery("scope", queued.ObjectID, "tenant", "chat", 1); !errors.Is(err, workstore.ErrScopeDenied) {
			return fmt.Errorf("revoked delivery visible on demand: %w", err)
		}
		published, err := tx.ListSentDeliveries("scope", "tenant", "chat", 1)
		if err != nil || len(published) != 0 {
			return fmt.Errorf("revoked delivery visible in published index: %v %w", published, err)
		}
		outbox, err := tx.GetOutbox(d.NotificationOutboxID)
		if err == nil && outbox.State != work.OutboxTerminal {
			return fmt.Errorf("revoked publication still sendable: %+v", outbox)
		}
		return err
	})
	if len(messages) != 0 {
		t.Fatal("revoked delegation reached the SDK")
	}
	var textGrant workservice.Result
	if err = leader("publication.grant", workservice.GrantPublicationRequest{Key: "text-grant", GranteeRoleID: "expert", TaskID: task.ID, ContentKind: "message"}, &textGrant); err != nil {
		t.Fatal(err)
	}
	text := workservice.PublishMessageRequest{Key: "unknown-message", GrantID: textGrant.ObjectID, TaskID: task.ID, Body: "Lose this publication receipt"}
	var uncertain workservice.Result
	if err = expert("message.publish", text, &uncertain); err != nil {
		t.Fatal(err)
	}
	if _, err = r.publisher.RunOnce(ctx, 16); err != nil || len(messages) != 1 {
		t.Fatalf("expected one SDK attempt with lost receipt: calls=%d err=%v", len(messages), err)
	}
	<-messages
	pickerTx(t, a, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(uncertain.ObjectID)
		if err == nil && op.State != work.OperationUnknown {
			return fmt.Errorf("SDK lost reply did not leave an unknown operation: %+v", op)
		}
		return err
	})
	if err = expert("message.publish", text, &uncertain); err != nil || !uncertain.Replay {
		t.Fatalf("same key must return accepted receipt after unknown transport: %+v %v", uncertain, err)
	}
	text.Key = "retry-with-fresh-key"
	assertForbiddenPublication(t, expert("message.publish", text, nil))
	assertPublicationCommands(t, expert, []string{"message.send"}, []string{"message.publish", "delivery.publish"})
	if _, err = r.publisher.RunOnce(ctx, 16); err != nil || len(messages) != 0 {
		t.Fatalf("unknown delegation sent another publication: calls=%d err=%v", len(messages), err)
	}

	receive := func(id, chat, chatType, parent string, mentions []work.RawMention, role string) (string, string) {
		t.Helper()
		return publicationIngressInput(t, ingress, r.store, identity, id, chat, chatType, parent, mentions, role)
	}
	mention := []work.RawMention{{Key: "@_user_1", IDs: work.SenderIDs{OpenID: "ou_expert"}}}
	oldID, _ := receive("old-directed", "chat", "group", "", mention, "expert")
	liveID, _ := receive("live-directed", "chat", "group", "", mention, "expert")
	directed := publicationJourneyCLI(t, f, endpoint.URL, "expert", startPublicationJourneyRun(t, r.store, t.TempDir(), "directed-pub", liveID, "scope", "expert"))
	assertPublicationCommands(t, directed, []string{"message.publish"}, []string{"delivery.publish", "media.publish", "publication.grant"})
	pickerTx(t, a, func(tx *workstore.Tx) error {
		input, err := tx.GetInbox(liveID)
		decision.SourceMessageID = input.MessageID
		return err
	})
	decision.Key = "unapproved-human-decision"
	assertForbiddenPublication(t, directed("decision.request", decision, nil))
	humanDecisionID, _ := receive("leader-question", "chat", "group", "", nil, "leader")
	pickerTx(t, a, func(tx *workstore.Tx) error {
		input, err := tx.GetInbox(humanDecisionID)
		decision.SourceMessageID = input.MessageID
		return err
	})
	leaderDirected := publicationJourneyCLI(t, f, endpoint.URL, "leader", startPublicationJourneyRun(t, r.store, t.TempDir(), "leader-question", humanDecisionID, "scope", "leader"))
	decision.Key = "leader-human-decision"
	if err = leaderDirected("decision.request", decision, nil); err != nil {
		t.Fatalf("leader's decision request for human: %v", err)
	}
	reply := workservice.PublishMessageRequest{Key: "reply", Body: "See https://example.com/reference for details.", ReplyToInputID: oldID}
	assertForbiddenPublication(t, directed("message.publish", reply, nil))
	reply.ReplyToInputID = liveID
	var first, replay workservice.Result
	if err = directed("message.publish", reply, &first); err != nil {
		t.Fatalf("authentic expert reply: %v", err)
	}
	if err = directed("message.publish", reply, &replay); err != nil || !replay.Replay || replay.ObjectID != first.ObjectID {
		t.Fatalf("reply was not idempotent: %+v %v", replay, err)
	}
	reply.Key = "second-reply-to-same-input"
	assertForbiddenPublication(t, directed("message.publish", reply, nil))
	if _, err = r.publisher.RunOnce(ctx, 16); err != nil || len(messages) != 1 {
		t.Fatalf("expected single expert SDK reply: sends=%d %v", len(messages), err)
	}
	sent := <-messages
	if sent.Path != "/open-apis/im/v1/messages/live-directed/reply" || sent.Thread || sent.Kind != "text" || !strings.Contains(sent.Content, "https://example.com/reference") {
		t.Fatalf("expert reply escaped original input: %+v", sent)
	}
	quotedID, _ := receive("quoted-expert", "chat", "group", "sent-"+sent.UUID, nil, "expert")
	quoted := publicationJourneyCLI(t, f, endpoint.URL, "expert", startPublicationJourneyRun(t, r.store, t.TempDir(), "quoted-pub", quotedID, "scope", "expert"))
	if err = quoted("message.publish", workservice.PublishMessageRequest{Key: "quote-reply", Body: "Exact reply", ReplyToInputID: quotedID}, nil); err != nil {
		t.Fatalf("human quote of the expert's published reply: %v", err)
	}
	if err = leader("message.publish", workservice.PublishMessageRequest{Key: "leader", Body: "Official result", Purpose: workservice.PublicationFinal}, nil); err != nil {
		t.Fatalf("leader publication: %v", err)
	}
	assertForbiddenPublication(t, leader("message.publish", workservice.PublishMessageRequest{Key: "late-progress", Body: "Work still underway", Purpose: workservice.PublicationProgress}, nil))
	topic := task
	topic.ID = "real-topic-task"
	pickerTx(t, a, func(tx *workstore.Tx) error { return tx.PutTask(topic, 0) })
	if err = leader("message.publish", workservice.PublishMessageRequest{Key: "topic-root", TaskID: topic.ID, Body: "Topic result", Purpose: workservice.PublicationFinal}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = r.publisher.RunOnce(ctx, 16); err != nil || len(messages) != 3 {
		t.Fatalf("leader and quoted replies should be sent: sends=%d %v", len(messages), err)
	}
	var topicRoot string
	for range 3 {
		sent = <-messages
		if strings.Contains(sent.Content, "Topic result") {
			topicRoot = "sent-" + sent.UUID
		}
	}
	if topicRoot == "" {
		t.Fatal("task topic lacked a real platform receipt")
	}
	topicInput, _ := receive("existing-topic-input", "chat", "group", topicRoot, mention, "expert")
	topicExpert := publicationJourneyCLI(t, f, endpoint.URL, "expert", startPublicationJourneyRun(t, r.store, t.TempDir(), "topic-expert", topicInput, "scope", "expert", topic))
	if err = topicExpert("message.publish", workservice.PublishMessageRequest{Key: "topic-reply", ReplyToInputID: topicInput, Body: "Existing topic answer"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = r.publisher.RunOnce(ctx, 16); err != nil || len(messages) != 1 {
		t.Fatalf("actual task topic reply: sends=%d %v", len(messages), err)
	}
	sent = <-messages
	if !sent.Thread || sent.Path != "/open-apis/im/v1/messages/existing-topic-input/reply" {
		t.Fatalf("task topic reply did not retain its authorized lane: %+v", sent)
	}

	privateID, privateScope := receive("private-input", "private-chat", "p2p", "", nil, "leader")
	privateDir := t.TempDir()
	private := publicationJourneyCLI(t, f, endpoint.URL, "leader", startPublicationJourneyRun(t, r.store, privateDir, "private-pub", privateID, privateScope, "leader"))
	assertPublicationCommands(t, private, []string{"message.publish", "delivery.publish", "media.publish"}, []string{"publication.grant"})
	if err = private("message.publish", workservice.PublishMessageRequest{Key: "personal", Body: "Private answer"}, nil); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(privateDir, "attachment.txt"), []byte("Private bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	share := workservice.PublishPrivateMediaRequest{Key: "media", Path: "attachment.txt", Kind: "file"}
	if err = private("media.publish", share, nil); err != nil {
		t.Fatal(err)
	}
	if err = private("media.publish", share, &replay); err != nil || !replay.Replay {
		t.Fatalf("media replay: %+v %v", replay, err)
	}
	if _, err = r.publisher.RunOnce(ctx, 16); err != nil || len(messages) != 2 || len(uploads) != 1 {
		t.Fatalf("private answer and file: sends=%d uploads=%d %v", len(messages), len(uploads), err)
	}
	if got := <-uploads; got != "Private bytes" {
		t.Fatalf("wrong file uploaded: %q", got)
	}
	for range 2 {
		sent := <-messages
		if sent.ChatID != "private-chat" || sent.Thread || sent.Path != "/open-apis/im/v1/messages" {
			t.Fatalf("private output escaped normal chat: %+v", sent)
		}
	}
}

type publicationCLI func(string, any, any) error

func publicationJourneyCLI(t *testing.T, f *resourceRecoveryFixture, url, role string, attempt work.ExecutionAttempt) publicationCLI {
	t.Helper()
	token, err := f.signer.Issue(workauth.Claims{Purpose: "worker", RoleID: role, BotGeneration: 1, ScopeID: attempt.ScopeID,
		ContextID: attempt.ContextID, WorkerID: attempt.RunRef.WorkerID, SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return func(command string, payload any, result any) error {
		args := []string{command, "--run-ref", attempt.RunRef.RunID}
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if payload != nil {
			args = append(args, "--stdin")
		}
		var out bytes.Buffer
		err = teamcli.Run(t.Context(), teamcli.Options{Args: args, Stdin: bytes.NewReader(data), Stdout: &out, Getenv: func(key string) string {
			return map[string]string{config.WorkToolEndpointEnv: url, config.WorkSessionTokenEnv: token}[key]
		}})
		if err != nil || result == nil {
			return err
		}
		return json.Unmarshal(out.Bytes(), result)
	}
}

func assertForbiddenPublication(t *testing.T, err error) {
	t.Helper()
	var denied *workcmd.Error
	if !errors.As(err, &denied) || denied.Code != "forbidden" {
		t.Fatalf("wanted a forbidden publication result, got %v", err)
	}
}

func assertPublicationCommands(t *testing.T, cli publicationCLI, include, exclude []string) {
	t.Helper()
	var commands []workcmd.Command
	if err := cli("commands", nil, &commands); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		names = append(names, command.Name)
	}
	for _, name := range include {
		if !slices.Contains(names, name) {
			t.Fatalf("missing command %s", name)
		}
	}
	for _, name := range exclude {
		if slices.Contains(names, name) {
			t.Fatalf("unavailable command advertised: %s", name)
		}
	}
}

func publicationIngressInput(t *testing.T, ingress *workingress.Ingress, store *workstore.Store, gateway work.IngressGateway, id, chat, chatType, parent string, mentions []work.RawMention, role string) (inputID, scopeID string) {
	t.Helper()
	ctx := t.Context()
	raw := work.RawIngressEnvelope{
		GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: id, Tenant: "tenant", ChatID: chat, ChatType: chatType,
		MessageID: id, ParentID: parent, SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "owner"},
		MessageType: "text", RawContent: `{"text":"Please explain the verified result."}`, Mentions: mentions, ReceivedAt: time.Now(),
	}
	receipt, err := ingress.Receive(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := ingress.ProcessPending(ctx, 16); err != nil || outcome.Processed != 1 {
		t.Fatalf("SDK input not processed: %+v %v", outcome, err)
	}
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		record, err := tx.GetIngress(receipt.IngressID)
		if err != nil {
			return err
		}
		msg, err := tx.GetMessage(record.MessageID)
		if err != nil {
			return err
		}
		scopeID = msg.ScopeID
		pending, err := tx.ListPendingInputs(scopeID, role)
		if err != nil {
			return err
		}
		for _, input := range pending {
			if input.MessageID == msg.ID && input.ControlOfInputID == "" {
				inputID = input.ID
				return nil
			}
		}
		return fmt.Errorf("input %s did not reach role %s", id, role)
	})
	if err != nil {
		t.Fatal(err)
	}
	return
}
