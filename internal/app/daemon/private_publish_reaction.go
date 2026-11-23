package daemon

import (
	"context"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

// Resolve from the exact message's durable status chain, never from the
// gateway's process-local reaction map or a neighboring message's receipt.
func (p *privatePublisher) resolveReaction(ctx context.Context, o privatechat.Owner, generation int64, id string, operation *feishu.Operation) error {
	var revision int64
	ordinal := 0
	for {
		page, err := p.journal.ListDisplayHistory(ctx, o, generation, id, revision, ordinal, 64)
		if err != nil {
			return err
		}
		for _, item := range page {
			for i := len(item.Receipt.Results) - 1; i >= 0; i-- {
				var receipt feishu.PrivateOperationReceipt
				if err = json.Unmarshal(item.Receipt.Results[i].Receipt, &receipt); err != nil {
					return err
				}
				if receipt.MessageID != operation.MessageID || receipt.EmojiType != operation.EmojiType {
					continue
				}
				if receipt.GatewayID != o.GatewayID || receipt.SurfaceSessionID != o.SurfaceID || receipt.ChatID != o.ChatID {
					return privatechat.ErrJournalConflict
				}
				switch receipt.Kind {
				case feishu.OperationRemoveReaction:
					operation.ReactionAbsent = true
					return nil
				case feishu.OperationAddReaction:
					operation.ReactionID = receipt.ReactionID
					operation.ReactionAbsent = receipt.ReactionAbsent
					if receipt.ReactionID == "" && !receipt.ReactionAbsent {
						return privatechat.ErrJournalConflict
					}
					return nil
				}
			}
		}
		if len(page) < 64 {
			operation.ReactionAbsent = true
			return nil
		}
		last := page[len(page)-1]
		revision, ordinal = last.Revision, last.Ordinal
	}
}
