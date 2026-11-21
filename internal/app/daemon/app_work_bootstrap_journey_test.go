package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhttp"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/teamcli"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type bootstrapJourneyDirectory struct{ gateway work.IngressGateway }

func (d bootstrapJourneyDirectory) ResolveGateway(context.Context, string, string) (work.IngressGateway, error) {
	return d.gateway, nil
}
func (d bootstrapJourneyDirectory) ResolveLeader(context.Context, string, string, string) (string, error) {
	return d.gateway.RoleID, nil
}
func (d bootstrapJourneyDirectory) ResolveMention(context.Context, work.IngressGateway, work.RawMention) (string, error) {
	return "", nil
}

func TestBootstrapJourneyFromGroupThroughDirectoryChoiceAndSecondProject(t *testing.T) {
	a, human := teamPickerFixture(t, isolatedSDKFixtureAppID(t, "bootstrap-leader"))
	expertAppID := isolatedSDKFixtureAppID(t, "bootstrap-expert")
	r := a.teamRuntime
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	r.service.WithBootstrapPlatform(workservice.BootstrapPlatform{
		ConfiguredApps: func() (map[string]string, error) {
			return map[string]string{human.Gateway.GatewayRef: human.Gateway.AppID, "expert-gateway": expertAppID}, nil
		},
		RegisterLocations:   a.registerTeamLocationCandidates,
		QueueLocationPicker: a.queueBootstrapLocationPicker,
	})
	r.ingress, err = workingress.New(r.store, workingress.Config{Directory: bootstrapJourneyDirectory{human.Gateway}})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(workhttp.NewHandler(r.service))
	defer endpoint.Close()
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	location := work.ExecutionLocation{ID: "selected-repo", HostID: teamID("host", host+":"+a.headlessRuntime.Paths.StateDir), Cwd: t.TempDir(), Generation: 1, Revision: 1}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		if err := tx.PutRole(work.Role{ID: "expert", Name: "测试同事", Description: "检查客户流程", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "expert", GatewayRef: "expert-gateway", AppID: expertAppID, IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRole(work.Role{ID: "disabled", Name: "已停用同事", Description: "旧账号", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "disabled", GatewayRef: "disabled-gateway", AppID: "old-app", IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		return tx.PutLocation(location, 0)
	})
	firstChat, firstRequest := "oc_portal", "这个群做客户门户，用工程师和测试同事。先做订单查询，需要现有仓库。"
	raw := func(chat, message, body string) work.RawIngressEnvelope {
		content, _ := json.Marshal(map[string]string{"text": body})
		return work.RawIngressEnvelope{GatewayRef: human.Gateway.GatewayRef, AppID: human.Gateway.AppID, Tenant: human.Tenant,
			ChatType: "group", ChatID: chat, EventID: "event:" + message, MessageID: message, SenderType: "user", SenderIDs: human.SenderIDs,
			MessageType: "text", RawContent: string(content), ReceivedAt: time.Now().UTC()}
	}
	first := bootstrapJourneyReceive(t, a, raw(firstChat, "om_portal", firstRequest))
	firstCall, attempt := bootstrapJourneyCLI(t, a, signer, endpoint.URL, first.ScopeID, "first-run")
	var view workservice.BootstrapView
	firstCall("bootstrap.context", nil, &view)
	if len(view.Teams) != 0 || len(view.Roles) != 2 || len(view.Inputs) != 1 || view.Inputs[0].Text != firstRequest {
		t.Fatalf("fresh team candidates and request: %+v", view)
	}
	var team, choice, project workservice.Result
	if err = firstCall("team.create", workservice.TeamCreateRequest{Key: "disabled", Name: "旧配置", LeaderRoleID: human.RoleID, MemberRoleIDs: []string{"disabled"}, SourceMessageID: first.ID}, nil); err == nil {
		t.Fatal("a disabled historical Bot was admitted into a new team")
	}
	firstCall("team.create", workservice.TeamCreateRequest{Key: "team", Name: "产品团队", LeaderRoleID: human.RoleID, MemberRoleIDs: []string{"expert"}, SourceMessageID: first.ID}, &team)
	firstCall("bootstrap.location.pick", workservice.BootstrapPickLocationRequest{Key: "repo", SourceMessageID: first.ID}, &choice)
	scope := view.Scope
	create := workservice.CreateProjectRequest{Key: scope.BootstrapOperationID, Tenant: scope.Tenant, ChatID: scope.ChatID,
		PrincipalID: scope.PrincipalID, Name: "客户门户", TeamID: team.ObjectID, LeaderRoleID: scope.RoleID,
		MemberRoleIDs: []string{"expert"}, InitialRequest: "订单查询", SourceMessageID: first.ID}
	if err = firstCall("project.create", create, nil); err == nil {
		t.Fatal("project allowed to start ahead of the human directory choice")
	}
	bootstrapJourneyFinish(t, a, attempt)
	human.ChatID = firstChat
	pickerView, interaction := pickerAction(t, a, human, choice.ObjectID, "picker.location.select")
	if err = a.processTeamInteraction(t.Context(), interaction); err != nil {
		t.Fatal(err)
	}
	if _, err = a.handleTeamPickerAction(t.Context(), interaction, pickerView, "picker.location.select"); err == nil {
		t.Fatal("used chooser allowed another selection")
	}
	continueCall, _ := bootstrapJourneyCLI(t, a, signer, endpoint.URL, first.ScopeID, "continue-run")
	continueCall("bootstrap.context", nil, &view)
	if view.DirectoryChoice == nil || view.DirectoryChoice.DefaultLocationID != location.ID || len(view.Inputs) != 1 || view.Inputs[0].Message.ID != first.ID {
		t.Fatalf("directory choice failed to wake the original input: %+v", view)
	}
	continueCall("project.create", create, &project)
	bootstrapJourneyExternalProvision(t, a, project, first, location.ID)
	firstWorkScope := bootstrapJourneyRecordFirstTask(t, a, signer, endpoint.URL, project, "实现订单查询", "portal-work")

	// The actual old group observation grants the initiating human access to the
	// first project. The second scope gets the reusable team, never old content.
	bootstrapJourneyReceive(t, a, raw(firstChat, "om_portal_followup", "客户门户项目专用要求"))
	second := bootstrapJourneyReceive(t, a, raw("oc_internal", "om_internal", "沿用客户门户的团队，建立内部后台。先梳理审批流程。"))
	secondCall, _ := bootstrapJourneyCLI(t, a, signer, endpoint.URL, second.ScopeID, "second-run")
	var matches []workservice.BootstrapProjectTeam
	secondCall("bootstrap.team.resolve", workservice.BootstrapResolveTeamRequest{ProjectName: "客户门户", SourceMessageID: second.ID}, &matches)
	if len(matches) != 1 || matches[0].TeamID != team.ObjectID {
		t.Fatalf("reuse lookup returned %+v", matches)
	}
	var secondView workservice.BootstrapView
	secondCall("bootstrap.context", nil, &secondView)
	scope = secondView.Scope
	create.Key, create.ChatID, create.SourceMessageID, create.Name = scope.BootstrapOperationID, scope.ChatID, second.ID, "内部后台"
	create.InitialRequest = "梳理审批流程"
	var secondProject workservice.Result
	secondCall("project.create", create, &secondProject)
	bootstrapJourneyExternalProvision(t, a, secondProject, second, "")
	if secondProject.ObjectID == project.ObjectID {
		t.Fatal("second project reused the first project state")
	}
	secondWorkScope := bootstrapJourneyRecordFirstTask(t, a, signer, endpoint.URL, secondProject, "梳理审批流程", "internal-work")
	if firstWorkScope == secondWorkScope {
		t.Fatal("second project reused the first execution scope")
	}
}

func bootstrapJourneyRecordFirstTask(t *testing.T, a *App, signer *workauth.Signer, endpoint string, receipt workservice.Result, title, run string) string {
	t.Helper()
	var project work.Project
	pickerTx(t, a, func(tx *workstore.Tx) error {
		var err error
		project, err = tx.GetProject(receipt.ObjectID)
		return err
	})
	call, attempt := bootstrapJourneyCLI(t, a, signer, endpoint, project.ScopeID, run)
	var current json.RawMessage
	call("context", nil, &current)
	if strings.Contains(string(current), "客户门户项目专用要求") {
		t.Fatalf("another project history entered the new project run: %s", current)
	}
	var created, replayed workservice.Result
	assign := workservice.AssignTaskRequest{Key: "first-work", Title: title, OwnerRoleID: "expert"}
	call("task.assign", assign, &created)
	call("task.assign", assign, &replayed)
	if !replayed.Replay || replayed.ObjectID != created.ObjectID {
		t.Fatal("same command registered duplicate first work")
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		inbox, err := tx.ListPendingInputs(project.ScopeID, "expert")
		if err != nil {
			return err
		}
		if len(inbox) != 1 || inbox[0].TaskID != created.ObjectID {
			return fmt.Errorf("expert did not receive one first task: %+v", inbox)
		}
		return nil
	})
	bootstrapJourneyFinish(t, a, attempt)
	return project.ScopeID
}

func bootstrapJourneyReceive(t *testing.T, a *App, envelope work.RawIngressEnvelope) work.Message {
	t.Helper()
	ctx, r := t.Context(), a.teamRuntime
	received, err := r.ingress.Receive(ctx, envelope)
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.ingress.ProcessPending(ctx, 16)
	if err != nil || result.Processed != 1 || len(result.Failures) != 0 {
		t.Fatalf("SDK ingress: %+v %v", result, err)
	}
	var message work.Message
	pickerTx(t, a, func(tx *workstore.Tx) error {
		record, err := tx.GetIngress(received.IngressID)
		if err != nil {
			return err
		}
		message, err = tx.GetMessage(record.MessageID)
		return err
	})
	return message
}

type bootstrapJourneyCall func(name string, input, output any) error

func bootstrapJourneyCLI(t *testing.T, a *App, signer *workauth.Signer, endpoint, scopeID, run string) (bootstrapJourneyCall, work.ExecutionAttempt) {
	t.Helper()
	var attempt work.ExecutionAttempt
	pickerTx(t, a, func(tx *workstore.Tx) error {
		const role = "leader"
		wc := work.WorkContext{ID: scopeID + ":work", RoleID: role, ScopeID: scopeID, Workstream: "establish", Revision: 1}
		worker, err := tx.GetWorker(scopeID + ":worker")
		if err == workstore.ErrNotFound {
			if err = tx.PutContext(wc, 0); err != nil {
				return err
			}
			worker = work.WorkerSession{ID: scopeID + ":worker", RoleID: role, ScopeID: scopeID, ContextID: wc.ID, Backend: work.BackendCodex,
				State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1,
				InstanceID: scopeID + ":carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
			err = tx.PutWorker(worker, 0)
		}
		if err != nil {
			return err
		}
		inbox, err := tx.ListPendingInputs(scopeID, role)
		if err != nil {
			return err
		}
		if len(inbox) != 1 {
			return fmt.Errorf("expected one input, saw %d", len(inbox))
		}
		attempt = work.ExecutionAttempt{ID: run + ":attempt", ScopeID: scopeID, ContextID: wc.ID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: run},
			State: work.AttemptDispatchPending, CommandID: run + ":start", InputIDs: []string{inbox[0].ID}, Revision: 1}
		op := work.Operation{ID: attempt.CommandID, ScopeID: scopeID, ContextID: wc.ID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"},
			Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: attempt.CommandID, RequestHash: "start", State: work.OperationPrepared, Revision: 1}
		if err = tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1,
			InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, StartOperation: op, Now: time.Now().UTC()}); err != nil {
			return err
		}
		attempt.State, attempt.Revision = work.AttemptRunning, 2
		return tx.PutAttempt(attempt, 1)
	})
	token, err := signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "leader", BotGeneration: 1, ScopeID: scopeID, ContextID: attempt.ContextID,
		WorkerID: attempt.RunRef.WorkerID, SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return func(name string, input, output any) error {
		args := []string{name, "--run-ref", run}
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		if input != nil {
			args = append(args, "--stdin")
		}
		var stdout bytes.Buffer
		err = teamcli.Run(t.Context(), teamcli.Options{Args: args, Stdin: bytes.NewReader(body), Stdout: &stdout, Getenv: func(key string) string {
			return map[string]string{config.WorkToolEndpointEnv: endpoint, config.WorkSessionTokenEnv: token}[key]
		}})
		if output != nil {
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if err = json.Unmarshal(stdout.Bytes(), output); err != nil {
				t.Fatalf("%s result %s: %v", name, stdout.String(), err)
			}
		}
		return err
	}, attempt
}

func bootstrapJourneyFinish(t *testing.T, a *App, attempt work.ExecutionAttempt) {
	t.Helper()
	pickerTx(t, a, func(tx *workstore.Tx) error {
		now := time.Now().UTC()
		checkpoint := work.NewContent(attempt.ID+":checkpoint", attempt.ScopeID, "text/plain", []byte("Waiting for the human's directory choice"), now)
		if err := tx.PutContent(checkpoint); err != nil {
			return err
		}
		return tx.CommitOutcome(workstore.OutcomeRequest{AttemptID: attempt.ID, RunRef: attempt.RunRef, ExpectedAttemptRevision: attempt.Revision,
			ProcessedInputIDs: attempt.InputIDs, OutcomeRef: checkpoint.ID, Checkpoint: string(checkpoint.Data), TerminalState: work.AttemptCompleted,
			TerminalEvidence: []work.SourceRef{{Kind: "controlled-runtime", ID: attempt.ID, Version: "1", EvidenceRef: checkpoint.ID}}, Quiescent: true, Now: now})
	})
}
