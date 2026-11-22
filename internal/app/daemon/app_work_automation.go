package daemon

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"time"
)

func (a *App) handleTeamAutomation(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	kind := work.AutomationWhip
	name := "自动检查"
	if action.Kind == control.ActionAutoContinueCommand {
		kind = work.AutomationContinue
		name = "断线后自动继续"
	}
	args := strings.Fields(action.Text)
	verb := "status"
	if len(args) > 1 {
		verb = strings.ToLower(args[1])
	}
	if len(args) > 2 || (verb != "status" && verb != "on" && verb != "off") {
		return "", fmt.Errorf("用法：/%s on、off 或 status", kind)
	}
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		wc, _, e := worksettings.FrozenContext(tx, source.Authority)
		if e != nil {
			return e
		}
		policy, e := tx.GetAutomationPolicy(wc.ID, kind)
		if e != nil && e != workstore.ErrNotFound {
			return e
		}
		if e == workstore.ErrNotFound {
			policy = work.AutomationPolicy{ContextID: wc.ID, ScopeID: wc.ScopeID, Kind: kind}
		}
		if verb != "status" {
			_, proof, e := tx.ValidatePrivateAction(source.Authority)
			if e != nil {
				return e
			}
			before := policy.Revision
			policy.Revision++
			policy.Enabled = verb == "on"
			policy.Source = proof
			policy.UpdatedAt = time.Now().UTC()
			if e = tx.PutAutomationPolicy(policy, before, source.Authority); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	note := ""
	if verb != "status" {
		note = "已保存「" + name + "」设置。"
	}
	return a.queuePrivatePanel(ctx, source, string(kind), note)
}
