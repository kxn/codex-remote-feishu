package workprovision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strconv"
	"strings"
)

type DirectoryTarget struct {
	Kind string     `json:"kind"`
	ID   string     `json:"id"`
	Wiki WikiParent `json:"wiki"`
}
type HomeDirectoryPlatform interface {
	ResolveDirectory(context.Context, string) (DirectoryTarget, error)
	IsDocumentInDirectory(context.Context, string, string, string, DirectoryTarget) (bool, error)
	MoveDocument(context.Context, string, string, DirectoryTarget) (feishu.ProjectDocumentMoveReceipt, error)
}

func (s *Service) loadDirectorySelection(tx *workstore.Tx, state *snapshot) error {
	if state.home.DirectoryGeneration == 0 {
		return nil
	}
	body, err := tx.GetContent(state.home.ScopeID, state.home.DirectorySelectionRef)
	if err != nil {
		return err
	}
	var selected workservice.ProjectDirectorySelection
	if err = json.Unmarshal(body.Data, &selected); err != nil {
		return err
	}
	if selected.Generation != state.home.DirectoryGeneration || selected.PrincipalID == "" || selected.Container == "" {
		return workstore.ErrScopeDenied
	}
	source, err := tx.GetMessage(selected.Source.ID)
	if err != nil {
		return err
	}
	if source.ScopeID != state.home.ScopeID || source.Source != (work.Actor{Kind: work.ActorPrincipal, ID: selected.PrincipalID}) || source.BodyRef != selected.Source.EvidenceRef || strconv.FormatInt(source.Revision, 10) != selected.Source.Version {
		return workstore.ErrScopeDenied
	}
	state.request.DocumentContainer = selected.Container
	return nil
}
func directoryParentStep(state snapshot, key string) string {
	if state.home.DirectoryGeneration > 0 {
		return key + ":directory:" + strconv.FormatInt(state.home.DirectoryGeneration, 10)
	}
	return key
}
func (s *Service) applyDirectorySelection(ctx context.Context, state *snapshot) error {
	if state.home.DirectoryGeneration == 0 || state.home.DirectoryAppliedGeneration == state.home.DirectoryGeneration {
		return nil
	}
	platform, err := s.platform(ctx, state.home.Identity)
	if err != nil {
		return err
	}
	mover, ok := platform.(HomeDirectoryPlatform)
	if !ok {
		return fmt.Errorf("selected platform has no actual document relocation API")
	}
	generation := strconv.FormatInt(state.home.DirectoryGeneration, 10)
	resolved, err := s.step(ctx, *state, "directory."+generation+".parent", state.home.Identity, map[string]string{"container": state.request.DocumentContainer}, false, func(ctx context.Context, _ json.RawMessage) (effect, error) {
		target, e := mover.ResolveDirectory(ctx, state.request.DocumentContainer)
		return effect{ID: target.ID, Data: target}, e
	})
	if err != nil {
		return err
	}
	target, err := decodeStep[DirectoryTarget](resolved)
	if err != nil {
		return err
	}
	docID := state.home.DocumentID
	var original workstore.ProvisionStep
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		original, e = tx.GetProvisionStep(state.operation.ID, "materials.document")
		if errors.Is(e, workstore.ErrNotFound) {
			return nil
		}
		return e
	})
	if err != nil {
		return err
	}
	if docID == "" {
		docID = original.ExternalID
	}
	if docID == "" && (original.State == work.OperationUnknown || original.State == work.OperationSending) {
		return fmt.Errorf("已保存新目录；旧首页创建结果仍未知，须先从恢复入口核对实际文档 ID，不能再次创建")
	}
	if docID != "" {
		if err = s.settlePreviousDirectoryMoves(ctx, *state, mover, docID); err != nil {
			return err
		}
		present, e := mover.IsDocumentInDirectory(ctx, docID, state.home.ContainerKind, state.home.ContainerID, target)
		if e != nil {
			return e
		}
		if !present {
			moved, moveErr := s.step(ctx, *state, "directory."+generation+".move", state.home.Identity, map[string]any{"document_id": docID, "target": target}, true, func(ctx context.Context, _ json.RawMessage) (effect, error) {
				r, e := mover.MoveDocument(ctx, docID, state.home.ContainerKind, target)
				return effect{ID: r.DocumentID, Data: r}, e
			})
			// A missing response never authorizes a second move. Only actual destination
			// membership can settle this exact document, regardless of an async task ID.
			present, e = mover.IsDocumentInDirectory(ctx, docID, state.home.ContainerKind, state.home.ContainerID, target)
			if e != nil {
				return e
			}
			if !present {
				if moveErr != nil {
					return fmt.Errorf("移动首页结果待核查，未重复请求：%w", moveErr)
				}
				return fmt.Errorf("首页移动请求已登记，尚未在实际目标目录中读到同一文档")
			}
			if moveErr != nil {
				if _, err = s.confirmKnown(ctx, *state, moved, effect{ID: docID, Data: map[string]any{"document_id": docID, "observed_target": target}}); err != nil {
					return err
				}
			}
		}
		if err = s.recordDirectoryMoveObserved(ctx, *state, state.home.DirectoryGeneration, docID, target); err != nil {
			return err
		}
	}
	newURL := state.home.URL
	if docID != "" {
		newURL, err = platform.ReadDocumentURL(ctx, docID)
		if err != nil {
			return err
		}
		if newURL == "" {
			return fmt.Errorf("moved document has no actual canonical URL")
		}
		if err = s.updateDirectoryNavigation(ctx, state, platform, newURL); err != nil {
			return err
		}
	}
	return s.homeUpdate(ctx, state, func(home *workstore.ProjectHome) {
		home.URL = newURL
		home.ContainerKind = target.Kind
		home.ContainerID = target.ID
		home.DirectoryAppliedGeneration = home.DirectoryGeneration
		if docID != "" {
			home.DocumentID = docID
		}
		home.SyncState = "pending"
	})
}
func (p *FeishuPlatform) ResolveDirectory(ctx context.Context, value string) (DirectoryTarget, error) {
	if strings.Contains(value, "/wiki/") {
		parent, err := p.ResolveWikiParent(ctx, value)
		if err != nil {
			return DirectoryTarget{}, err
		}
		return DirectoryTarget{Kind: "wiki", ID: parent.SpaceID + ":" + parent.NodeToken, Wiki: parent}, nil
	}
	folder, err := selectedFolderToken(value)
	if err != nil || folder == "" {
		return DirectoryTarget{}, fmt.Errorf("explicit existing Drive folder required")
	}
	if err = p.CheckFolder(ctx, folder); err != nil {
		return DirectoryTarget{}, err
	}
	return DirectoryTarget{Kind: "drive", ID: folder}, nil
}
func (p *FeishuPlatform) IsDocumentInDirectory(ctx context.Context, id, sourceKind, sourceID string, target DirectoryTarget) (bool, error) {
	if target.Kind == "drive" {
		return p.documents.DocumentInFolder(ctx, id, target.ID)
	}
	if sourceKind == "drive" && sourceID != "" {
		present, err := p.documents.DocumentInFolder(ctx, id, sourceID)
		if err != nil {
			return false, err
		}
		if present {
			return false, nil
		}
	}
	node, err := p.documents.WikiNodeForDocument(ctx, id)
	if err != nil {
		return false, err
	}
	return node.SpaceID == target.Wiki.SpaceID && string(node.ParentNodeToken) == target.Wiki.NodeToken, nil
}
func (p *FeishuPlatform) MoveDocument(ctx context.Context, id, sourceKind string, target DirectoryTarget) (feishu.ProjectDocumentMoveReceipt, error) {
	if target.Kind == "drive" {
		return p.documents.MoveDocumentToFolder(ctx, id, target.ID)
	}
	sourceSpace, sourceNode := "", ""
	if sourceKind == "wiki" {
		node, err := p.documents.WikiNodeForDocument(ctx, id)
		if err != nil {
			return feishu.ProjectDocumentMoveReceipt{}, err
		}
		sourceSpace = node.SpaceID
		sourceNode = string(node.NodeToken)
	}
	return p.documents.MoveDocumentToWiki(ctx, id, sourceSpace, sourceNode, target.Wiki.SpaceID, target.Wiki.NodeToken)
}
