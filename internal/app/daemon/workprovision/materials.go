package workprovision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func (s *Service) homeUpdate(ctx context.Context, state *snapshot, update func(*workstore.ProjectHome)) error {
	return s.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := s.current(tx, *state); err != nil {
			return err
		}
		home, err := tx.GetProjectHome(state.project.ID)
		if err != nil {
			return err
		}
		identity, e := tx.GetRoleBotBinding(home.Identity.RoleID)
		if e != nil {
			return e
		}
		if identity != home.Identity {
			return ErrStale
		}
		previous := home.Revision
		update(&home)
		home.Revision++
		home.UpdatedAt = s.now()
		if err = tx.PutProjectHome(home, previous); err != nil {
			return err
		}
		state.home = home
		return nil
	})
}
func (s *Service) materials(ctx context.Context, state *snapshot) error {
	platform, err := s.platform(ctx, state.home.Identity)
	if err != nil {
		return err
	}
	container := state.request.DocumentContainer
	isWiki := strings.HasPrefix(container, "wiki:") || strings.HasPrefix(container, "wiki-space:") || strings.Contains(container, "/wiki/")
	var parent WikiParent
	if isWiki {
		step, err := s.step(ctx, *state, directoryParentStep(*state, "materials.wiki_parent"), state.home.Identity, map[string]string{"container": container}, false, func(ctx context.Context, intent json.RawMessage) (effect, error) {
			var req map[string]string
			if err := json.Unmarshal(intent, &req); err != nil {
				return effect{}, err
			}
			parent, err := platform.ResolveWikiParent(ctx, req["container"])
			return effect{ID: parent.NodeToken, Data: parent}, err
		})
		if err != nil {
			return err
		}
		parent, err = decodeStep[WikiParent](step)
		if err != nil {
			return err
		}
		if err = s.homeUpdate(ctx, state, func(home *workstore.ProjectHome) {
			home.ContainerKind = "wiki"
			home.ContainerID = parent.SpaceID + ":" + parent.NodeToken
		}); err != nil {
			return err
		}
	} else {
		folderID, err := selectedFolderToken(container)
		if err != nil {
			return err
		}
		if folderID == "" {
			step, createErr := s.step(ctx, *state, "materials.folder", state.home.Identity, map[string]string{"name": state.request.Name}, true, func(ctx context.Context, intent json.RawMessage) (effect, error) {
				var req map[string]string
				if err := json.Unmarshal(intent, &req); err != nil {
					return effect{}, err
				}
				folder, err := platform.CreateFolder(ctx, req["name"], "")
				return effect{ID: folder.ID, Data: folder}, err
			})
			if createErr != nil && step.ExternalID != "" {
				if err = platform.CheckFolder(ctx, step.ExternalID); err == nil {
					step, createErr = s.confirmKnown(ctx, *state, step, effect{ID: step.ExternalID, Data: Folder{ID: step.ExternalID}})
				}
			}
			if step.ExternalID != "" {
				folderID = step.ExternalID
				if err = s.homeUpdate(ctx, state, func(home *workstore.ProjectHome) { home.ContainerKind = "drive"; home.ContainerID = folderID }); err != nil {
					return err
				}
			}
			if createErr != nil {
				return fmt.Errorf("文件夹创建结果未确认；请核对实际 folder token：%w", createErr)
			}
		} else {
			if err = platform.CheckFolder(ctx, folderID); err != nil {
				return err
			}
			if err = s.homeUpdate(ctx, state, func(home *workstore.ProjectHome) { home.ContainerKind = "drive"; home.ContainerID = folderID }); err != nil {
				return err
			}
		}
	}
	document, createErr := s.step(ctx, *state, "materials.document", state.home.Identity, map[string]any{"title": state.request.Name, "container": state.home.ContainerID, "wiki": isWiki, "parent": parent}, true, func(ctx context.Context, intent json.RawMessage) (effect, error) {
		var req struct {
			Title     string     `json:"title"`
			Container string     `json:"container"`
			Wiki      bool       `json:"wiki"`
			Parent    WikiParent `json:"parent"`
		}
		if err := json.Unmarshal(intent, &req); err != nil {
			return effect{}, err
		}
		var doc HomeDocument
		var err error
		if req.Wiki {
			doc, err = platform.CreateWikiDocument(ctx, req.Parent, req.Title)
		} else {
			doc, err = platform.CreateDocument(ctx, req.Title, req.Container)
		}
		return effect{ID: doc.ID, Version: strconv.Itoa(doc.Revision), Data: doc}, err
	})
	if document.ExternalID != "" {
		// Save the returned ID before any URL lookup, sharing, or block write.
		if err = s.homeUpdate(ctx, state, func(home *workstore.ProjectHome) { home.DocumentID = document.ExternalID }); err != nil {
			return err
		}
		if createErr != nil {
			doc, readErr := platform.ReadDocument(ctx, document.ExternalID)
			if readErr == nil {
				document, createErr = s.confirmKnown(ctx, *state, document, effect{ID: doc.ID, Version: strconv.Itoa(doc.Revision), Data: doc})
			}
		}
	}
	if createErr != nil {
		return fmt.Errorf("首页创建结果未知；已有 ID 会保留，无 ID 时须明确恢复：%w", createErr)
	}
	doc, err := decodeStep[HomeDocument](document)
	if err != nil {
		return err
	}
	if doc.ID == "" {
		return fmt.Errorf("首页回执缺少 document ID")
	}
	if err = s.homeUpdate(ctx, state, func(home *workstore.ProjectHome) {
		home.DocumentID = doc.ID
		home.DocumentRevision = doc.Revision
		if home.URL == "" && doc.URL != "" {
			home.URL = doc.URL
		}
	}); err != nil {
		return err
	}
	if state.home.URL == "" {
		urlStep, err := s.step(ctx, *state, "materials.url", state.home.Identity, map[string]string{"document_id": doc.ID}, false, func(ctx context.Context, _ json.RawMessage) (effect, error) {
			link, err := platform.ReadDocumentURL(ctx, doc.ID)
			return effect{ID: doc.ID, Data: map[string]string{"url": link}}, err
		})
		if err != nil {
			return err
		}
		value, err := decodeStep[map[string]string](urlStep)
		if err != nil {
			return err
		}
		if value["url"] == "" {
			return fmt.Errorf("平台未返回可用首页链接")
		}
		if err = s.homeUpdate(ctx, state, func(home *workstore.ProjectHome) { home.URL = value["url"] }); err != nil {
			return err
		}
	}
	var issues []error
	if err = s.shareHome(ctx, *state, platform); err != nil {
		issues = append(issues, err)
	}
	if err = s.initializeIndex(ctx, state, platform); err != nil {
		issues = append(issues, err)
	}
	return errors.Join(issues...)
}
func (s *Service) shareHome(ctx context.Context, state snapshot, platform Platform) error {
	existing, err := platform.HasGroupView(ctx, state.home.DocumentID, state.request.ChatID)
	if err != nil {
		return err
	}
	if existing {
		return nil
	}
	step, err := s.step(ctx, state, "materials.share", state.home.Identity, map[string]string{"document_id": state.home.DocumentID, "chat_id": state.request.ChatID, "permission": "view"}, true, func(ctx context.Context, intent json.RawMessage) (effect, error) {
		var req map[string]string
		if err := json.Unmarshal(intent, &req); err != nil {
			return effect{}, err
		}
		err := platform.EnsureGroupView(ctx, req["document_id"], req["chat_id"])
		return effect{ID: req["document_id"], Data: map[string]any{"chat_id": req["chat_id"], "permission": "view", "acknowledged": err == nil}}, err
	})
	if err != nil {
		visible, checkErr := platform.HasGroupView(ctx, state.home.DocumentID, state.request.ChatID)
		if checkErr == nil && visible {
			_, err = s.confirmKnown(ctx, state, step, effect{ID: state.home.DocumentID, Data: map[string]any{"chat_id": state.request.ChatID, "permission": "view", "read_back": true}})
		}
	}
	return err
}
