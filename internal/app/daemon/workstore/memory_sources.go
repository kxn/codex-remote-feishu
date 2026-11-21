package workstore

import (
	"fmt"
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Platform observations may advance a message's record revision without
// changing its text. A source still refers to the same evidence body; recall,
// changed content or ownership makes it unusable.
func (t *Tx) validateMemoryMessageSources(entry work.MemoryEntry) error {
	for _, source := range entry.Sources {
		if source.Kind != "message" {
			continue
		}
		message, err := t.GetMessage(source.ID)
		if err != nil {
			return err
		}
		revision, err := strconv.ParseInt(source.Version, 10, 64)
		if err != nil || revision < 1 || revision > message.Revision || message.ScopeID != entry.StorageScopeID || source.EvidenceRef != message.BodyRef || message.RecalledEvidenceRef != "" {
			return fmt.Errorf("memory message evidence is withdrawn, changed or outside its source scope")
		}
	}
	return nil
}
