package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

func TestTeamBotMenuStatusAndRetiredStopPublishDistinctFeedback(t *testing.T) {
	a, source := teamPickerFixture(t)
	for _, tc := range []struct{ key, prompt string }{
		{string(feishuapp.TeamMenuStatus), "当前工作状态"},
		{"stop", "请在私聊发送 /stop 停止当前工作"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			var click workstore.MenuInteraction
			pickerTx(t, a, func(tx *workstore.Tx) error {
				raw := work.MenuInteractionEnvelope{
					GatewayRef: source.Gateway.GatewayRef,
					AppID:      source.Gateway.AppID,
					EventID:    "menu-" + tc.key,
					Tenant:     source.Tenant,
					SenderIDs:  source.SenderIDs,
					EventKey:   tc.key,
					ReceivedAt: time.Now().UTC(),
				}
				var err error
				click, _, err = tx.AcceptMenuInteraction(raw, source.Gateway)
				if err != nil {
					return err
				}
				sending, _, err := tx.BeginMenuDestination(click.ID, click.Revision, "已收到菜单请求。")
				if err != nil {
					return err
				}
				if err = tx.FinishMenuDestination(click.ID, sending.Revision, source.ChatID, "entry-"+tc.key, "", true); err != nil {
					return err
				}
				click, err = tx.GetMenuInteraction(click.ID)
				return err
			})

			if err := a.advanceTeamMenu(t.Context(), click); err != nil {
				t.Fatal(err)
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				latest, err := tx.GetMenuInteraction(click.ID)
				if err != nil {
					return err
				}
				view, err := tx.GetUIView(latest.ResultRef)
				if err != nil {
					return err
				}
				card, err := tx.GetContent(view.ScopeID, view.BodyRef)
				if err == nil && !strings.Contains(string(card.Data), tc.prompt) {
					t.Errorf("menu feedback should include %q", tc.prompt)
				}
				return err
			})
		})
	}
}
