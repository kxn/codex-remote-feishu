package daemon

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

type privateSettingsCarrierLease struct {
	host   *privateNativeHost
	record privateCarrierRecord
}

func (l privateSettingsCarrierLease) WithPrivateOwner(ctx context.Context, o privatechat.Owner, generation int64, commit func() error) error {
	if l.host == nil || l.host.registry == nil || l.record.Surface != o || l.record.SurfaceGeneration != generation || l.record.Snapshot.Owner.Epoch != generation {
		return privatechat.ErrJournalConflict
	}
	return l.host.registry.WithLease(ctx, privateRegistration(l.record.Snapshot.Owner), commit)
}
func (h *privateNativeHost) settingsLease(ctx context.Context, o privatechat.Owner, generation int64) (privateSettingsOwnerLease, error) {
	if h == nil || h.store == nil {
		return nil, fmt.Errorf("native carrier unavailable")
	}
	var chosen *privateCarrierRecord
	for _, state := range []string{"running", "draining", "sealed"} {
		records, err := h.store.byState(ctx, state)
		if err != nil {
			return nil, err
		}
		for i := range records {
			rec := records[i]
			if rec.Surface == o && rec.SurfaceGeneration == generation {
				if chosen != nil {
					return nil, privatechat.ErrJournalConflict
				}
				copy := rec
				chosen = &copy
			}
		}
	}
	if chosen == nil {
		return nil, privatechat.ErrDeferred
	}
	return privateSettingsCarrierLease{host: h, record: *chosen}, nil
}
