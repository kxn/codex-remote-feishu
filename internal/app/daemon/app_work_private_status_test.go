package daemon

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateStatusRestoresConversationDirectoryConfigurationAndGit(t *testing.T) {
	base, source := teamPickerFixture(t)
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, AppSecret: "isolated"}}
	a, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), feishu.NewMultiGatewayController(), false, "")
	a.teamRuntime = base.teamRuntime
	a.headlessRuntime.Paths.StateDir = base.headlessRuntime.Paths.StateDir
	setTestWorkBotSettings(t, a, state.BotCapabilitySettingsRecord{GatewayID: source.Gateway.GatewayRef, Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID, PromptOverride: state.ModelConfigRecord{Model: "status-selected-model", ReasoningEffort: "high"}})
	a.feishuRuntime.permissionGaps = map[string]map[string]*feishuPermissionGapRecord{
		source.Gateway.GatewayRef: {"actual-gap": {Scope: "im:message:send_as_bot", LastSourceAPI: "message.create"}},
		"unrelated-bot":           {"other-gap": {Scope: "do-not-expose-other-bot"}},
	}
	dir := t.TempDir()
	if output, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", output, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("visible change"), 0600); err != nil {
		t.Fatal(err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	var current string
	pickerTx(t, a, func(tx *workstore.Tx) error {
		nav, err := tx.GetPrivateNavigation(source.ScopeID)
		if err != nil {
			return err
		}
		current = nav.FrontContextID
		if err = tx.PutContext(work.WorkContext{ID: "unrelated-context", ScopeID: source.ScopeID, RoleID: source.RoleID, Workstream: "other", Revision: 1}, 0); err != nil {
			return err
		}
		evidence := work.NewContent("status-settings", source.ScopeID, "text/plain", []byte("human selections"), time.Now().UTC())
		if err = tx.PutContent(evidence); err != nil {
			return err
		}
		if err = tx.PutLocation(work.ExecutionLocation{ID: "status-dir", HostID: teamID("host", hostname+":"+a.headlessRuntime.Paths.StateDir), Cwd: dir, Generation: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err = tx.PutExecutionPreference(workstore.ExecutionPreference{ScopeID: source.ScopeID, DefaultLocationID: "status-dir", SelectedLocationIDs: []string{"status-dir"}, SelectedBy: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, SourceRef: evidence.ID, Revision: 1}, 0); err != nil {
			return err
		}
		access, plan, verbose := "confirm", "on", "quiet"
		_, ref, err := tx.ValidatePrivateAction(source.Authority)
		if err != nil {
			return err
		}
		return tx.PutContextPreferences(work.ContextPreferences{ContextID: current, ScopeID: source.ScopeID, AccessMode: &access, PlanMode: &plan, Verbose: &verbose, Source: ref, UpdatedBy: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, Revision: 1, UpdatedAt: time.Now().UTC()}, 0, source.Authority)
	})
	for i, input := range []struct{ id, context, text string }{{"first", current, "first selected conversation message"}, {"other", "unrelated-context", "do-not-expose-other-conversation"}, {"last", current, "last selected conversation message"}} {
		pickerTx(t, a, func(tx *workstore.Tx) error {
			at := time.Now().UTC().Add(time.Duration(i) * time.Second)
			body := work.NewContent(input.id+":body", source.ScopeID, "text/plain", []byte(input.text), at)
			if err := tx.PutContent(body); err != nil {
				return err
			}
			message := work.Message{ID: input.id, ScopeID: source.ScopeID, ConversationID: source.ChatID, Source: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, PlatformKey: &work.MessageKey{Tenant: source.Tenant, ChatID: source.ChatID, MessageID: input.id}, Observations: []work.Observation{{AppID: source.Gateway.AppID, IdentityGeneration: 1, ReceivedAt: at}}, BodyRef: body.ID, Revision: 1, CreatedAt: at}
			_, _, err := tx.AcceptMessage(message, []work.Inbox{{ID: input.id + ":input", ScopeID: source.ScopeID, RecipientRoleID: source.RoleID, RecipientContextID: input.context, MessageID: input.id, EventID: input.id, State: work.InboxPending, Revision: 1}})
			return err
		})
	}
	id, err := a.queuePrivatePanel(t.Context(), source, "status", "")
	if err != nil {
		t.Fatal(err)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		view, err := tx.GetUIView(id)
		if err != nil {
			return err
		}
		body, err := tx.GetContent(source.ScopeID, view.BodyRef)
		if err != nil {
			return err
		}
		raw := string(body.Data)
		for _, want := range []string{"/status", "first selected conversation message", "last selected conversation message", "2 条待处理", dir, "Git 分支", "1 个未跟踪", "status-selected-model", "high", "confirm", "quiet", "/autowhip", "/autocontinue", "/plan", "im:message:send_as_bot", "message.create"} {
			if !strings.Contains(raw, want) {
				t.Errorf("status missing %q", want)
			}
		}
		if strings.Contains(raw, "do-not-expose-other-conversation") {
			t.Error("status leaked another context")
		}
		if strings.Contains(raw, "do-not-expose-other-bot") {
			t.Error("status leaked another bot")
		}
		if _, err := tx.ReadPrivateContextStatus(source.ScopeID, current, "different-principal"); !errors.Is(err, workstore.ErrScopeDenied) {
			t.Fatalf("cross-principal status=%v", err)
		}
		inputs, err := tx.ListInbox(source.ScopeID)
		if err != nil {
			return err
		}
		if len(inputs) != 3 {
			t.Fatalf("status created or consumed input: %d", len(inputs))
		}
		for _, input := range inputs {
			if input.State != work.InboxPending {
				t.Error("status changed pending work")
			}
		}
		return nil
	})
}

func TestPrivateStatusKeepsExecutionFactsWhenSettingsCannotBeRead(t *testing.T) {
	a, source := teamPickerFixture(t)
	id, err := a.queuePrivatePanel(t.Context(), source, "status", "")
	if err != nil {
		t.Fatal(err)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		view, err := tx.GetUIView(id)
		if err != nil {
			return err
		}
		body, err := tx.GetContent(source.ScopeID, view.BodyRef)
		if err != nil {
			return err
		}
		for _, want := range []string{"/status", "新会话", "尚未选择", "0 条待处理", "参数配置读取失败"} {
			if !strings.Contains(string(body.Data), want) {
				t.Errorf("status missing %s", want)
			}
		}
		return nil
	})
}
