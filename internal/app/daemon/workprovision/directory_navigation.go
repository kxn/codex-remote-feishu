package workprovision

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"strconv"
)

type directoryNavigationUpdater interface {
	UpdateDirectoryTab(context.Context, string, string, string) error
	UpdateDirectoryMessage(context.Context, string, string) error
}

func (p *FeishuPlatform) UpdateDirectoryTab(ctx context.Context, chat, tabID, link string) error {
	tabs, err := p.chat.ListChatTabs(ctx, chat)
	if err != nil {
		return err
	}
	for _, tab := range tabs {
		if tab.ID == tabID {
			if tab.Type != "doc" && tab.Type != "url" {
				return fmt.Errorf("recorded project Tab is no longer a document/link tab")
			}
			tab.URL = link
			_, err = p.chat.UpdateChatTab(ctx, chat, tab)
			return err
		}
	}
	return fmt.Errorf("recorded project Tab no longer exists")
}
func (p *FeishuPlatform) UpdateDirectoryMessage(ctx context.Context, id, text string) error {
	return p.chat.UpdateNavigationText(ctx, id, text)
}
func (s *Service) updateDirectoryNavigation(ctx context.Context, state *snapshot, platform Platform, newURL string) error {
	if newURL == state.home.URL || state.home.URL == "" {
		return nil
	}
	updater, ok := platform.(directoryNavigationUpdater)
	if !ok && (state.home.TabID != "" || state.home.NavigationMessageID != "") {
		return fmt.Errorf("current platform cannot update the existing fixed navigation")
	}
	generation := strconv.FormatInt(state.home.DirectoryGeneration, 10)
	if state.home.TabID != "" {
		matches := func() (bool, error) {
			tabs, err := platform.ListTabs(ctx, state.request.ChatID)
			if err != nil {
				return false, err
			}
			for _, tab := range tabs {
				if tab.ID == state.home.TabID {
					return (tab.Type == "doc" || tab.Type == "url") && tab.URL == newURL, nil
				}
			}
			return false, fmt.Errorf("项目原有 Tab 已被删除，保留其他可用入口，不另抢建")
		}
		done, err := matches()
		if err != nil {
			return err
		}
		if !done {
			step, callErr := s.step(ctx, *state, "directory."+generation+".tab", state.home.Identity, map[string]string{"tab_id": state.home.TabID, "url": newURL}, true, func(ctx context.Context, _ json.RawMessage) (effect, error) {
				err := updater.UpdateDirectoryTab(ctx, state.request.ChatID, state.home.TabID, newURL)
				return effect{ID: state.home.TabID, Data: map[string]string{"url": newURL}}, err
			})
			done, err = matches()
			if err != nil {
				return err
			}
			if !done {
				return fmt.Errorf("项目 Tab 更新待核查：%v", callErr)
			}
			if callErr != nil {
				if _, err = s.confirmKnown(ctx, *state, step, effect{ID: state.home.TabID, Data: feishu.ProjectChatTab{ID: state.home.TabID, URL: newURL}}); err != nil {
					return err
				}
			}
		}
	}
	if state.home.NavigationMessageID != "" {
		text := renderNavigation(Navigation{ProjectName: state.request.Name, HomeURL: newURL, SyncState: "资料目录已更新，索引待同步"})
		matches := func() (bool, error) {
			actual, err := platform.ReadNavigation(ctx, state.home.NavigationMessageID)
			if err != nil {
				return false, err
			}
			return actual.ChatID == state.request.ChatID && actual.Text == text, nil
		}
		done, err := matches()
		if err != nil {
			return err
		}
		if !done {
			step, callErr := s.step(ctx, *state, "directory."+generation+".navigation", state.home.Identity, map[string]string{"message_id": state.home.NavigationMessageID, "text": text}, true, func(ctx context.Context, _ json.RawMessage) (effect, error) {
				err := updater.UpdateDirectoryMessage(ctx, state.home.NavigationMessageID, text)
				return effect{ID: state.home.NavigationMessageID, Data: map[string]string{"text": text}}, err
			})
			done, err = matches()
			if err != nil {
				return err
			}
			if !done {
				return fmt.Errorf("固定导航文字更新待核查：%v", callErr)
			}
			if callErr != nil {
				if _, err = s.confirmKnown(ctx, *state, step, effect{ID: state.home.NavigationMessageID, Data: map[string]string{"text": text}}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
