package workprovision

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
)

type RecoveryChoice struct{ ID, Label string }
type RecoveryChoices struct{ Primary, Secondary []RecoveryChoice }
type recoveryBlockLister interface {
	ListRecoveryHomeBlocks(context.Context, string) ([]HomeBlock, error)
}

// RecoveryOptions only lists actual objects in the frozen project destination.
// Candidates confer no ownership until a real human selects and re-verifies one.
func (s *Service) RecoveryOptions(ctx context.Context, target RecoveryInteractionTarget) (RecoveryChoices, error) {
	state, err := s.load(ctx, target.OperationID)
	if err != nil {
		return RecoveryChoices{}, err
	}
	platform, err := s.platform(ctx, state.home.Identity)
	if err != nil {
		return RecoveryChoices{}, err
	}
	result := RecoveryChoices{}
	switch target.StepID {
	case "navigation.tab":
		tabs, err := platform.ListTabs(ctx, state.request.ChatID)
		if err != nil {
			return result, err
		}
		for _, tab := range tabs {
			if (tab.Type == "doc" || tab.Type == "url") && tab.URL == state.home.URL {
				result.Primary = append(result.Primary, RecoveryChoice{ID: tab.ID, Label: tab.Name})
			}
		}
	case "materials.index":
		lister, ok := platform.(recoveryBlockLister)
		if !ok {
			return result, fmt.Errorf("actual document block listing unavailable")
		}
		blocks, err := lister.ListRecoveryHomeBlocks(ctx, state.home.DocumentID)
		if err != nil {
			return result, err
		}
		for _, block := range blocks {
			if block.ID == "" || block.Text == "" {
				continue
			}
			label := []rune(block.Text)
			if len(label) > 80 {
				label = label[:80]
			}
			choice := RecoveryChoice{ID: block.ID, Label: string(label)}
			result.Primary = append(result.Primary, choice)
			result.Secondary = append(result.Secondary, choice)
		}
	}
	if len(result.Primary) > 100 || len(result.Secondary) > 100 {
		return RecoveryChoices{}, fmt.Errorf("候选区段较多，请先缩小资料范围；未截断现有候选项")
	}
	return result, nil
}
func (p *FeishuPlatform) ListRecoveryHomeBlocks(ctx context.Context, id string) ([]HomeBlock, error) {
	metadata, err := p.documents.GetDocument(ctx, feishu.ProjectDocumentID(id))
	if err != nil {
		return nil, err
	}
	if metadata.RevisionID == nil {
		return nil, fmt.Errorf("document revision missing")
	}
	result := []HomeBlock{}
	token := ""
	seen := map[string]bool{}
	for {
		page, err := p.documents.ListDocumentBlocksPage(ctx, feishu.ProjectDocumentBlocksRequest{DocumentID: feishu.ProjectDocumentID(id), RevisionID: *metadata.RevisionID, PageSize: 500, PageToken: token})
		if err != nil {
			return nil, err
		}
		for _, block := range page.Blocks {
			if block != nil && block.Text != nil {
				value, err := homeBlock(block)
				if err != nil {
					return nil, err
				}
				result = append(result, value)
			}
		}
		if !page.HasMore {
			break
		}
		if seen[page.NextPageToken] {
			return nil, fmt.Errorf("block pagination cycle")
		}
		seen[page.NextPageToken] = true
		token = page.NextPageToken
		if len(result) > 100 {
			return nil, fmt.Errorf("首页候选段落超过展示上限，未截断")
		}
	}
	return result, nil
}
