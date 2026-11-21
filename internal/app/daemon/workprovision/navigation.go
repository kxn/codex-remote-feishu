package workprovision

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func uuidFor(parts ...string) string {
	data, _ := json.Marshal(parts)
	sum := sha256.Sum256(data)
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
func (s *Service) navigation(ctx context.Context, state *snapshot) error {
	platform, err := s.platform(ctx, state.home.Identity)
	if err != nil {
		return err
	}
	var issues []error
	if state.home.URL != "" {
		tab, tabErr := s.step(ctx, *state, "navigation.tab", state.home.Identity, map[string]string{"chat_id": state.request.ChatID, "url": state.home.URL}, true, func(ctx context.Context, intent json.RawMessage) (effect, error) {
			var req map[string]string
			if err := json.Unmarshal(intent, &req); err != nil {
				return effect{}, err
			}
			tab, err := platform.CreateDocTab(ctx, req["chat_id"], req["url"])
			return effect{ID: tab.ID, Data: tab}, err
		})
		if tabErr != nil && tab.ExternalID != "" {
			tabs, readErr := platform.ListTabs(ctx, state.request.ChatID)
			if readErr == nil {
				for _, actual := range tabs {
					if actual.ID == tab.ExternalID && (actual.Type == "doc" || actual.Type == "url") && actual.URL == state.home.URL {
						tab, tabErr = s.confirmKnown(ctx, *state, tab, effect{ID: actual.ID, Data: actual})
						break
					}
				}
			}
		}
		if tab.ExternalID != "" {
			if err = s.homeUpdate(ctx, state, func(home *workstore.ProjectHome) { home.TabID = tab.ExternalID }); err != nil {
				issues = append(issues, err)
			}
		}
		if tabErr != nil {
			issues = append(issues, fmt.Errorf("项目 Tab 结果待按实际 tab_id 核对：%w", tabErr))
		}
	}
	if _, ok := platform.(interface{ UsesFixedNavigationCard() }); ok && state.home.NavigationMessageID == "" {
		legacy := false
		if err = s.store.Within(ctx, func(tx *workstore.Tx) error {
			_, err := tx.GetProvisionStep(state.operation.ID, "navigation.message")
			legacy = err == nil
			if errors.Is(err, workstore.ErrNotFound) {
				return nil
			}
			return err
		}); err != nil {
			return errors.Join(append(issues, err)...)
		}
		if !legacy {
			if !state.home.NavigationCardWanted && state.home.NavigationCardViewID == "" {
				if err = s.homeUpdate(ctx, state, func(home *workstore.ProjectHome) { home.NavigationCardWanted = true }); err != nil {
					issues = append(issues, err)
				}
			}
			return errors.Join(issues...)
		}
	}
	sync := "资料索引待同步"
	if state.home.SyncState == "synced" {
		sync = "已同步"
	}
	navigation := Navigation{ProjectName: state.request.Name, HomeURL: state.home.URL, SyncState: sync}
	message, messageErr := s.step(ctx, *state, "navigation.message", state.home.Identity, navigation, true, func(ctx context.Context, intent json.RawMessage) (effect, error) {
		var req Navigation
		if err := json.Unmarshal(intent, &req); err != nil {
			return effect{}, err
		}
		id, err := platform.PublishNavigation(ctx, state.request.ChatID, req, uuidFor("navigation", state.operation.ID))
		return effect{ID: id, Data: map[string]string{"message_id": id, "chat_id": state.request.ChatID}}, err
	})
	if messageErr != nil && message.ExternalID != "" {
		actual, readErr := platform.ReadNavigation(ctx, message.ExternalID)
		var desired Navigation
		decodeErr := json.Unmarshal(message.Intent, &desired)
		if readErr == nil && decodeErr == nil && actual.ChatID == state.request.ChatID && actual.Text == renderNavigation(desired) {
			message, messageErr = s.confirmKnown(ctx, *state, message, effect{ID: actual.MessageID, Data: actual})
		}
	}
	if message.ExternalID != "" {
		if err = s.homeUpdate(ctx, state, func(home *workstore.ProjectHome) { home.NavigationMessageID = message.ExternalID }); err != nil {
			issues = append(issues, err)
		}
	}
	if messageErr != nil {
		issues = append(issues, fmt.Errorf("导航消息结果未知，未重新发送：%w", messageErr))
		return errors.Join(issues...)
	}
	if message.ExternalID == "" {
		return errors.Join(append(issues, fmt.Errorf("navigation message has no actual ID"))...)
	}
	pin, pinErr := s.step(ctx, *state, "navigation.pin", state.home.Identity, map[string]string{"message_id": message.ExternalID}, true, func(ctx context.Context, intent json.RawMessage) (effect, error) {
		var req map[string]string
		if err := json.Unmarshal(intent, &req); err != nil {
			return effect{}, err
		}
		id, err := platform.PinNavigation(ctx, req["message_id"])
		return effect{ID: id, Data: map[string]string{"message_id": id}}, err
	})
	if pinErr != nil {
		exists, readErr := platform.HasNavigationPin(ctx, state.request.ChatID, message.ExternalID)
		if readErr == nil && exists {
			pin, pinErr = s.confirmKnown(ctx, *state, pin, effect{ID: message.ExternalID, Data: map[string]any{"message_id": message.ExternalID, "read_back": true}})
		}
	}
	if pinErr != nil {
		issues = append(issues, pinErr)
	} else if err = s.homeUpdate(ctx, state, func(home *workstore.ProjectHome) { home.PinMessageID = pin.ExternalID }); err != nil {
		issues = append(issues, err)
	}
	return errors.Join(issues...)
}
