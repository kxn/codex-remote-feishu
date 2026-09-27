package daemon

import (
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (a *App) persistSurfaceResumeEntryLocked(entry surfaceresume.Entry, now time.Time) error {
	if !a.surfaceResumeRuntime.writable() || a.surfaceResumeRuntime.store == nil || strings.TrimSpace(a.surfaceResumeRuntime.store.Path()) == "" {
		return errSurfaceResumeStateNotWritable
	}
	entry.UpdatedAt = now
	if err := a.surfaceResumeRuntime.store.Put(entry); err != nil {
		return err
	}
	a.clearGroupOnDemandTerminalFailureLocked(entry.SurfaceSessionID)
	a.markVSCodeDetachedPromptScanDueLocked()
	return nil
}

func (a *App) persistCodexTopicOverrideLocked(surface *state.SurfaceConsoleRecord) error {
	if !a.surfaceResumeRuntime.writable() || a.surfaceResumeRuntime.store == nil || strings.TrimSpace(a.surfaceResumeRuntime.store.Path()) == "" {
		return errSurfaceResumeStateNotWritable
	}
	entry, _ := a.currentSurfaceResumeEntryLocked(surface, false)
	err := a.persistSurfaceResumeEntryLocked(entry, time.Now().UTC())
	if err != nil {
		log.Printf("persist Codex topic setting failed: surface=%s err=%v", surface.SurfaceSessionID, err)
	}
	return err
}
