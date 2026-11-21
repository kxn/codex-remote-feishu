package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) runTeamProjectNavigation(ctx context.Context) error {
	r := a.teamRuntime
	if r == nil || r.provision == nil {
		return nil
	}
	var homes []workstore.ProjectHome
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		projects, err := tx.ListProjects()
		if err != nil {
			return err
		}
		for _, project := range projects {
			home, err := tx.GetProjectHome(project.ID)
			if errors.Is(err, workstore.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			homes = append(homes, home)
		}
		return nil
	}); err != nil {
		return err
	}
	var failures []error
	for _, home := range homes {
		if home.NavigationCardWanted {
			failures = append(failures, a.advanceTeamProjectNavigation(ctx, home))
		}
		failures = append(failures, r.provision.ObserveFixedNavigation(ctx, home.ID))
	}
	return errors.Join(failures...)
}

func (a *App) advanceTeamProjectNavigation(ctx context.Context, home workstore.ProjectHome) error {
	r := a.teamRuntime
	var pinView workstore.UIView
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		current, err := tx.GetProjectHome(home.ID)
		if err != nil {
			return err
		}
		if !current.NavigationCardWanted || current.NavigationGeneration != home.NavigationGeneration {
			return nil
		}
		if current.NavigationCardViewID != "" {
			view, err := tx.GetUIView(current.NavigationCardViewID)
			if err != nil {
				return err
			}
			operation, err := tx.GetOperation(view.OperationID)
			if err != nil {
				return err
			}
			if !view.Invalidated && operation.State == work.OperationConfirmed {
				pinView = view
			}
			return nil
		}
		facts, err := teamProjectFacts(tx, home.ID)
		if err != nil {
			return err
		}
		if facts.Channel.BindingGeneration != current.ChannelGeneration || facts.Binding != current.Identity {
			return workstore.ErrConflict
		}
		id := workui.ID("project.navigation", home.ID, workRevision(home.NavigationGeneration))
		panel := workui.Panel{Title: "项目 · " + facts.Project.Name, Paragraphs: []string{
			"项目负责人：" + facts.MemberNames[facts.Project.LeaderRoleID],
			"互动按钮失效时，可在群里请负责人重新打开工作入口。",
		}, Buttons: []workui.Button{
			{Label: "查看工作", Action: "project.navigation.open"},
			{Label: "暂停项目", Action: "project.navigation.pause"},
		}}
		if current.URL != "" {
			panel.Buttons = append(panel.Buttons, workui.Button{Label: "打开项目首页", URL: current.URL})
		}
		card, err := workui.Card(id, panel)
		if err != nil {
			return err
		}
		view := workstore.UIView{ID: id, ScopeID: home.ScopeID, SurfaceKey: "project.navigation:" + home.ID, Kind: "project.navigation", TargetID: home.ID, TargetRevision: facts.Project.Revision, Binding: facts.Binding, Tenant: facts.Channel.Tenant, ChatID: facts.Channel.ChatID, ChannelGeneration: facts.Channel.BindingGeneration}
		if _, err = workui.Queue(tx, view, card); err != nil {
			return err
		}
		current.NavigationCardViewID = id
		current.Revision++
		current.UpdatedAt = time.Now()
		return tx.PutProjectHome(current, current.Revision-1)
	})
	if err != nil || pinView.ExternalMessageID == "" {
		return err
	}
	return r.provision.PinFixedNavigation(ctx, home.ID, pinView.ID, pinView.ExternalMessageID)
}

func (a *App) handleTeamProjectNavigation(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) error {
	if err := a.verifyWorkMembership(ctx, interaction.PrincipalID, view.ScopeID, interaction.Envelope.Tenant, interaction.Envelope.SenderIDs, interaction.Gateway); err != nil {
		return err
	}
	switch action {
	case "project.navigation.open":
		if view.Kind == "project.navigation" {
			return a.openProjectStatusFromNavigation(ctx, interaction, view)
		}
	case "project.navigation.pause":
		if view.Kind == "project.navigation" {
			return a.teamRuntime.service.PauseFromProjectNavigation(ctx, interaction.ID, view.ID)
		}
	case "project.navigation.restore":
		if view.Kind == "project.status" {
			return a.restoreTeamProjectNavigation(ctx, interaction, view)
		}
	}
	return workstore.ErrScopeDenied
}

func (a *App) openProjectStatusFromNavigation(ctx context.Context, interaction workstore.HumanInteraction, source workstore.UIView) error {
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.ValidateViewInteraction(source, interaction); err != nil {
			return err
		}
		facts, err := teamProjectFacts(tx, source.TargetID)
		if err != nil {
			return err
		}
		id := workui.ID("project.navigation.status", interaction.ID)
		view := workstore.UIView{ID: id, ScopeID: source.ScopeID, SurfaceKey: id, Kind: "project.status", TargetID: source.TargetID, TargetRevision: facts.Project.Revision, Binding: source.Binding, Tenant: source.Tenant, ChatID: source.ChatID, ChannelGeneration: source.ChannelGeneration, ReplyToMessageID: source.ExternalMessageID}
		if err = queueTeamProjectStatus(tx, view, facts); err != nil {
			return err
		}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, id)
	})
}

func queueTeamProjectStatus(tx *workstore.Tx, view workstore.UIView, facts teamProjectProjection) error {
	card, err := workui.Card(view.ID, teamProjectPanel(facts))
	if err != nil {
		return err
	}
	if facts.NavigationTabMissing || facts.NavigationPinMissing || facts.NavigationLegacy {
		content := work.NewContent(view.ID+":navigation-generation", view.ScopeID, "text/plain", []byte(workRevision(facts.NavigationGeneration)), time.Now())
		if err = tx.PutContent(content); err != nil {
			return err
		}
	}
	_, err = workui.Queue(tx, view, card)
	return err
}

func (a *App) restoreTeamProjectNavigation(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView) error {
	r := a.teamRuntime
	if r.provision == nil {
		return fmt.Errorf("项目固定入口恢复暂不可用")
	}
	// The status view freezes the observation generation, so an old action is
	// never permission to repair a subsequent human removal.
	var restoreTab bool
	var generation int64
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.ValidateViewInteraction(view, interaction); err != nil {
			return err
		}
		home, err := tx.GetProjectHome(view.TargetID)
		if err != nil {
			return err
		}
		proof, err := tx.GetContent(view.ScopeID, view.ID+":navigation-generation")
		if err != nil {
			return err
		}
		generation = home.NavigationGeneration
		if string(proof.Data) != workRevision(generation) {
			return workstore.ErrConflict
		}
		restoreTab = home.NavigationTabMissing
		if home.NavigationCardWanted || (!home.NavigationPinMissing && (home.NavigationCardViewID != "" || home.PinMessageID == "")) {
			return nil
		}
		home.NavigationCardWanted = true
		home.NavigationCardViewID = ""
		home.Revision++
		home.UpdatedAt = time.Now()
		return tx.PutProjectHome(home, home.Revision-1)
	})
	if err != nil {
		return err
	}
	if restoreTab {
		if err = r.provision.RestoreFixedTab(ctx, view.TargetID, generation); err != nil {
			return fmt.Errorf("项目 Tab 恢复未确认。已保留原首页和操作记录，请核对飞书中是否出现新的项目标签：%w", err)
		}
	}
	return r.store.Within(ctx, func(tx *workstore.Tx) error {
		id := workui.ID("project.navigation.restore.result", interaction.ID)
		card, err := workui.Card(id, workui.Panel{Title: "固定入口恢复请求已保存", Paragraphs: []string{"系统会核对已登记的导航卡和标签。消息 Pin 和群顶部置顶是两个独立入口。"}})
		if err != nil {
			return err
		}
		result := workstore.UIView{ID: id, ScopeID: view.ScopeID, SurfaceKey: id, Kind: "project.navigation.result", TargetID: view.TargetID, TargetRevision: view.TargetRevision, Binding: view.Binding, Tenant: view.Tenant, ChatID: view.ChatID, ChannelGeneration: view.ChannelGeneration}
		if _, err = workui.Queue(tx, result, card); err != nil {
			return err
		}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, id)
	})
}
