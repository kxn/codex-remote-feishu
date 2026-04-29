package daemon

import (
	"log"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/claudeworkspaceprofile"
)

func (a *App) configureClaudeWorkspaceProfileStateLocked(stateDir string) {
	path := claudeworkspaceprofile.StatePath(stateDir)
	store, err := claudeworkspaceprofile.LoadStore(path)
	if err != nil {
		log.Printf("load claude workspace profile state failed: path=%s err=%v", path, err)
		store = claudeworkspaceprofile.NewStore(path)
	}
	if store != nil && store.Dirty() {
		if err := store.Save(); err != nil {
			log.Printf("persist sanitized claude workspace profile state failed: path=%s err=%v", path, err)
		}
	}
	a.claudeWorkspaceProfileState.store = store
	if store != nil {
		a.service.MaterializeClaudeWorkspaceProfileSnapshots(store.Entries())
	}
}

func (a *App) syncClaudeWorkspaceProfileStateLocked() {
	if a.claudeWorkspaceProfileState.store == nil {
		return
	}
	existing := a.claudeWorkspaceProfileState.store.Entries()
	desired := a.service.ClaudeWorkspaceProfileSnapshots()
	for key, entry := range desired {
		if current, ok := a.claudeWorkspaceProfileState.store.Get(key); ok && current == entry {
			continue
		}
		if err := a.claudeWorkspaceProfileState.store.Put(key, entry); err != nil {
			log.Printf("persist claude workspace profile state failed: key=%s err=%v", key, err)
		}
	}
	for key := range existing {
		if _, ok := desired[key]; ok {
			continue
		}
		if err := a.claudeWorkspaceProfileState.store.Delete(key); err != nil {
			log.Printf("clear claude workspace profile state failed: key=%s err=%v", key, err)
		}
	}
}
