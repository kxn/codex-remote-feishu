package daemon

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// The owner writes immutable snapshots under a.mu. The private processor reads
// them without a.mu, so no lock order crosses the host/processor boundary.
type privateProfileCatalog struct {
	Codex                            []state.CodexProfileSummary
	Claude                           []state.ClaudeProfileRecord
	OpenCode                         []state.OpenCodeProfileSummary
	HasCodex, HasClaude, HasOpenCode bool
}

func (a *App) nextPrivateProfilesLocked() privateProfileCatalog {
	if old := a.privateProfilesSnapshot.Load(); old != nil {
		return *old
	}
	return privateProfileCatalog{}
}
func (a *App) updatePrivateCodexCatalogLocked(records []state.CodexProfileSummary) {
	next := a.nextPrivateProfilesLocked()
	next.Codex = append([]state.CodexProfileSummary(nil), records...)
	next.HasCodex = true
	a.privateProfilesSnapshot.Store(&next)
}
func (a *App) updatePrivateClaudeCatalogLocked(records []state.ClaudeProfileRecord) {
	next := a.nextPrivateProfilesLocked()
	next.Claude = append([]state.ClaudeProfileRecord(nil), records...)
	next.HasClaude = true
	a.privateProfilesSnapshot.Store(&next)
}
func (a *App) updatePrivateOpenCodeCatalogLocked(records []state.OpenCodeProfileSummary) {
	next := a.nextPrivateProfilesLocked()
	next.OpenCode = append([]state.OpenCodeProfileSummary(nil), records...)
	next.HasOpenCode = true
	a.privateProfilesSnapshot.Store(&next)
}
func (a *App) refreshPrivateProfileCatalog(ctx context.Context, p *privatechat.Processor) error {
	snapshot := a.privateProfilesSnapshot.Load()
	if snapshot == nil {
		return nil
	}
	return p.ApplyProfileCatalog(ctx, privatechat.ProfileCatalog{Codex: snapshot.Codex, Claude: snapshot.Claude, OpenCode: snapshot.OpenCode, HasCodex: snapshot.HasCodex, HasClaude: snapshot.HasClaude, HasOpenCode: snapshot.HasOpenCode}, func() bool { return a.privateProfilesSnapshot.Load() == snapshot })
}
