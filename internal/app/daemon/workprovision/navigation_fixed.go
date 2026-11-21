package workprovision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (s *Service) navigationSnapshot(ctx context.Context, projectID string) (snapshot, error) {
	var id string
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		home, err := tx.GetProjectHome(projectID)
		id = home.OperationID
		return err
	}); err != nil {
		return snapshot{}, err
	}
	return s.load(ctx, id)
}

// ObserveFixedNavigation checks only known IDs. API or permission errors do
// not establish removal, and observing removal never triggers a write.
func (s *Service) ObserveFixedNavigation(ctx context.Context, projectID string) error {
	state, err := s.navigationSnapshot(ctx, projectID)
	if err != nil {
		return err
	}
	home := state.home
	if home.TabID == "" && home.PinMessageID == "" {
		return nil
	}
	platform, err := s.platform(ctx, home.Identity)
	if err != nil {
		return err
	}
	tabMissing, pinMissing := home.NavigationTabMissing, home.NavigationPinMissing
	observeTab := home.TabID != ""
	if observeTab {
		if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
			step, err := tx.GetProvisionStep(home.OperationID, "navigation.tab")
			if errors.Is(err, workstore.ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			// A tentative ID from an uncertain create is not evidence of an
			// established entry that a human removed.
			if step.ExternalID == home.TabID && step.State != work.OperationConfirmed {
				observeTab = false
			}
			return nil
		}); err != nil {
			return err
		}
	}
	var tabErr, pinErr error
	if observeTab {
		actual, err := platform.ListTabs(ctx, state.request.ChatID)
		tabErr = err
		if err == nil {
			tabMissing = true
			for _, tab := range actual {
				if tab.ID == home.TabID {
					tabMissing = false
					break
				}
			}
		}
	}
	if home.PinMessageID != "" && !home.NavigationCardWanted {
		var present bool
		present, pinErr = platform.HasNavigationPin(ctx, state.request.ChatID, home.PinMessageID)
		if pinErr == nil {
			pinMissing = !present
		}
	}
	if tabMissing == home.NavigationTabMissing && pinMissing == home.NavigationPinMissing {
		return errors.Join(tabErr, pinErr)
	}
	saveErr := s.homeUpdate(ctx, &state, func(current *workstore.ProjectHome) {
		if current.Revision != home.Revision {
			return
		}
		if tabMissing && !home.NavigationTabMissing || pinMissing && !home.NavigationPinMissing {
			current.NavigationGeneration++
		}
		current.NavigationTabMissing = tabMissing
		current.NavigationPinMissing = pinMissing
	})
	return errors.Join(tabErr, pinErr, saveErr)
}

func IsNavigationTabRestoreStep(step string) bool {
	return strings.HasPrefix(step, "navigation.fixed.tab.")
}

func navigationTabRestoreStep(tabID string) string {
	return "navigation.fixed.tab." + stableID("tab", tabID)
}

func (s *Service) RestoreFixedTab(ctx context.Context, projectID string, generation int64) error {
	state, err := s.navigationSnapshot(ctx, projectID)
	if err != nil {
		return err
	}
	home := state.home
	if !home.NavigationTabMissing || home.NavigationGeneration != generation || home.TabID == "" || home.URL == "" {
		return workstore.ErrConflict
	}
	platform, err := s.platform(ctx, home.Identity)
	if err != nil {
		return err
	}
	tabs, err := platform.ListTabs(ctx, state.request.ChatID)
	if err != nil {
		return err
	}
	for _, tab := range tabs {
		if tab.ID == home.TabID {
			return s.homeUpdate(ctx, &state, func(home *workstore.ProjectHome) { home.NavigationTabMissing = false })
		}
	}
	key := navigationTabRestoreStep(home.TabID)
	step, err := s.step(ctx, state, key, home.Identity, map[string]string{"old_tab_id": home.TabID, "url": home.URL}, true, func(ctx context.Context, intent json.RawMessage) (effect, error) {
		var request map[string]string
		if err := json.Unmarshal(intent, &request); err != nil {
			return effect{}, err
		}
		tab, err := platform.CreateDocTab(ctx, state.request.ChatID, request["url"])
		if tab.ID == "" && err == nil {
			err = fmt.Errorf("未收到新项目标签的实际 ID")
		}
		return effect{ID: tab.ID, Data: tab}, err
	})
	if err != nil && step.ExternalID != "" {
		for _, tab := range tabs {
			if tab.ID == step.ExternalID && tab.URL == home.URL && (tab.Type == "doc" || tab.Type == "url") {
				step, err = s.confirmKnown(ctx, state, step, effect{ID: tab.ID, Data: tab})
				break
			}
		}
	}
	if err != nil {
		return err
	}
	return s.homeUpdate(ctx, &state, func(home *workstore.ProjectHome) {
		home.TabID = step.ExternalID
		home.NavigationTabMissing = false
	})
}

// PinFixedNavigation pins only a confirmed UIView message. An uncertain Pin is
// always checked by exact message ID and never replayed.
func (s *Service) PinFixedNavigation(ctx context.Context, projectID, viewID, messageID string) error {
	state, err := s.navigationSnapshot(ctx, projectID)
	if err != nil {
		return err
	}
	home := state.home
	if !home.NavigationCardWanted || home.NavigationCardViewID != viewID || messageID == "" {
		return workstore.ErrConflict
	}
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		view, err := tx.GetUIView(viewID)
		if err != nil {
			return err
		}
		if view.Invalidated || view.Kind != "project.navigation" || view.ScopeID != home.ScopeID ||
			view.TargetID != projectID || view.Binding != home.Identity || view.ChannelGeneration != home.ChannelGeneration ||
			view.Tenant != state.request.Tenant || view.ChatID != state.request.ChatID || view.ExternalMessageID != messageID {
			return workstore.ErrConflict
		}
		operation, err := tx.GetOperation(view.OperationID)
		if err != nil {
			return err
		}
		if operation.State != work.OperationConfirmed {
			return workstore.ErrConflict
		}
		return nil
	}); err != nil {
		return err
	}
	platform, err := s.platform(ctx, home.Identity)
	if err != nil {
		return err
	}
	key := "navigation.fixed.pin." + viewID
	step, err := s.step(ctx, state, key, home.Identity, map[string]string{"message_id": messageID}, true, func(ctx context.Context, _ json.RawMessage) (effect, error) {
		id, err := platform.PinNavigation(ctx, messageID)
		if err == nil && id != messageID {
			err = fmt.Errorf("项目 Pin 回执未确认同一消息")
		}
		return effect{ID: id, Data: id}, err
	})
	if err != nil {
		exists, readErr := platform.HasNavigationPin(ctx, state.request.ChatID, messageID)
		if readErr == nil && exists {
			step, err = s.confirmKnown(ctx, state, step, effect{ID: messageID, Data: messageID})
		}
	}
	if err != nil {
		return err
	}
	if step.State != work.OperationConfirmed {
		return ErrUnknown
	}
	return s.homeUpdate(ctx, &state, func(home *workstore.ProjectHome) {
		if home.NavigationCardViewID == viewID {
			home.PinMessageID = messageID
			home.NavigationCardWanted = false
			home.NavigationPinMissing = false
		}
	})
}
