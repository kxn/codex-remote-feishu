package daemon

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateMenuRetainsReviewCompactAndExplicitMCPAcrossBackends(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendClaude, agentproto.BackendOpenCode} {
		t.Run(string(backend), func(t *testing.T) {
			a, source := teamPickerFixture(t)
			pickerTx(t, a, func(tx *workstore.Tx) error {
				nav, err := tx.GetPrivateNavigation(source.ScopeID)
				if err != nil {
					return err
				}
				if err = tx.PutWorker(work.WorkerSession{ID: "ready-worker", ContextID: nav.FrontContextID, ScopeID: source.ScopeID, RoleID: source.RoleID, Backend: work.Backend(backend), State: work.WorkerReady, Controller: work.ControllerAutomatic, Generation: 1, ControllerGeneration: 1, NativeSessionID: "existing-session", NativeNamespace: "fixture", Revision: 1}, 0); err != nil {
					return err
				}
				for _, kind := range []string{"tools", "basic", "mcp", "admin"} {
					panel, err := teamPrivateMenuPanel(tx, source, nav, backend, kind, "")
					if err != nil {
						return err
					}
					actions := map[string]bool{}
					for _, button := range panel.Buttons {
						actions[button.Action] = true
					}
					if kind == "admin" && !actions["private.admin-autostart"] {
						t.Fatal("autostart entry hidden before actual host capability detection")
					}
					if kind == "tools" && (!actions["private.review"] || actions["private.mcp"]) {
						t.Fatal("retained native tools vanished for backend", backend)
					}
					if kind == "tools" && actions["private.goal-help"] != (backend == agentproto.BackendCodex) {
						t.Fatal("Goal shown for unsupported backend")
					}
					if kind == "basic" && !actions["private.compact"] {
						t.Fatal("safe context rebuild unavailable from menu")
					}
					if kind == "mcp" {
						login := false
						for _, form := range panel.Forms {
							login = login || form.Submit.Action == "private.mcp-login"
						}
						if login != (backend == agentproto.BackendCodex) {
							t.Fatal("MCP OAuth menu mismatches native capability")
						}
					}
				}
				return nil
			})
		})
	}
}

func TestPrivateReadOnlyReviewDebugAndHistoryEntrypoints(t *testing.T) {
	for _, tt := range []struct{ text, want string }{
		{"/review", "private.review-commit"}, {"/review commit", "private.review-commit"}, {"/REVIEW COMMIT", "private.review-commit"}, {"/review cancel", "已退出审查选择"}, {"/debug", "系统管理"},
	} {
		t.Run(tt.text, func(t *testing.T) {
			a, source := teamPickerFixture(t)
			action, known := parseTeamPrivateCommand(tt.text)
			if !known {
				t.Fatal("missing slash parse")
			}
			id, err := a.handleTeamPrivateOperation(t.Context(), source, action)
			if err != nil {
				t.Fatal(err)
			}
			if card := goalUICardBody(t, a, source.ScopeID, id); !strings.Contains(card, tt.want) {
				t.Fatalf("missing expected entry %q: %s", tt.want, card)
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				inputs, err := tx.ListInbox(source.ScopeID)
				if len(inputs) != 0 {
					t.Fatal("read-only launcher started business execution")
				}
				return err
			})
		})
	}
	a, source := teamPickerFixture(t)
	id, err := a.queueTeamCurrentHistory(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if card := goalUICardBody(t, a, source.ScopeID, id); !strings.Contains(card, "会话历史") {
		t.Fatal("history opened context list instead of current discussion")
	}
	if _, err = a.handleTeamPrivateOperation(t.Context(), source, control.Action{Kind: control.ActionDebugCommand, Text: "/debug admin"}); err == nil {
		t.Fatal("deprecated debug args silently discarded")
	}
	if _, err = a.handleTeamPrivateOperation(t.Context(), source, control.Action{Kind: control.ActionReviewCommand, Text: "/review cancel extra"}); err == nil {
		t.Fatal("invalid review cancellation became a new custom review")
	}
}
