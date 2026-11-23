package daemon

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

// SetPrivateCatalogSnapshot atomically replaces a copy of the processor-owned
// catalog. Callers obtain it under Processor.CatalogSnapshot outside a.mu.
func (a *App) SetPrivateCatalogSnapshot(paths []string) {
	copy := append([]string(nil), paths...)
	a.privateCatalogSnapshot.Store(&copy)
}

// BindPrivateServiceOwnership is called only after processor construction.
func (a *App) BindPrivateServiceOwnership(app *privatechat.App) error {
	a.profileReferenceGate.Lock()
	defer a.profileReferenceGate.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if app == nil || a.service != app.Service() {
		return fmt.Errorf("private service mismatch")
	}
	a.privateServiceOwned.Store(true)
	return nil
}
