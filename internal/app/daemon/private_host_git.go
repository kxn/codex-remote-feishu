package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/gitworkspace"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/workspaceimport"
)

type privateGitReceipt struct {
	Version                                                                         int
	EffectID, SurfaceID, PickerID, Kind, WorkspacePath, ErrorCode, ErrorDestination string
	Succeeded, Cancelled                                                            bool
}

func runPrivateGitCancel(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	record, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(record.Effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	if cmd == nil || (cmd.Kind != control.DaemonCommandGitWorkspaceImportCancel && cmd.Kind != control.DaemonCommandGitWorkspaceWorktreeCancel) || cmd.SurfaceSessionID != o.SurfaceID || event.SurfaceSessionID != o.SurfaceID || cmd.PickerID == "" {
		return nil, privatechat.ErrJournalConflict
	}
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateGitReducer{host})
	}
	if record.State != "prepared" && record.State != "unknown" {
		return nil, fmt.Errorf("git cancellation requires recovery")
	}
	if record.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
			return nil, err
		}
	}
	host.mu.Lock()
	if cmd.Kind == control.DaemonCommandGitWorkspaceImportCancel {
		host.cancelGitWorkspaceImportRuntimeLocked(o.SurfaceID, cmd.PickerID)
	} else {
		host.cancelGitWorkspaceWorktreeRuntimeLocked(o.SurfaceID, cmd.PickerID)
	}
	host.mu.Unlock()
	raw, err := json.Marshal(privateGitReceipt{Version: 1, EffectID: id, SurfaceID: o.SurfaceID, PickerID: cmd.PickerID, Kind: string(cmd.Kind), Cancelled: true})
	if err != nil {
		return nil, err
	}
	if err = j.ConfirmLocalEffect(context.WithoutCancel(ctx), o, generation, id, raw, privateLocalVerifier{o, generation, record.Effect, raw}); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateGitReducer{host})
}

// runPrivateGitWorkspace launches only a committed Git intent. A crash after
// launch remains unknown until independently reconciled; it is never rerun.
func runPrivateGitWorkspace(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	record, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(record.Effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	if cmd == nil || (cmd.Kind != control.DaemonCommandGitWorkspaceImport && cmd.Kind != control.DaemonCommandGitWorkspaceWorktreeCreate) || cmd.SurfaceSessionID != o.SurfaceID || event.SurfaceSessionID != o.SurfaceID || cmd.PickerID == "" {
		return nil, privatechat.ErrJournalConflict
	}
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateGitReducer{host})
	}
	if record.State != "prepared" {
		return nil, fmt.Errorf("git operation requires exact recovery evidence")
	}
	if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
		return nil, err
	}
	result := privateGitReceipt{Version: 1, EffectID: id, SurfaceID: o.SurfaceID, PickerID: cmd.PickerID, Kind: string(cmd.Kind)}
	var runErr error
	if cmd.Kind == control.DaemonCommandGitWorkspaceImport {
		runCtx, cancel := context.WithTimeout(ctx, gitWorkspaceImportCommandTimeout)
		host.mu.Lock()
		key := host.beginGitWorkspaceImportRuntimeLocked(o.SurfaceID, cmd.PickerID, cancel)
		host.mu.Unlock()
		var output gitworkspace.ImportResult
		output, runErr = runGitWorkspaceImport(runCtx, gitworkspace.ImportRequest{RepoURL: strings.TrimSpace(cmd.RepoURL), RefName: strings.TrimSpace(cmd.RefName), ParentDir: strings.TrimSpace(cmd.LocalPath), DirectoryName: strings.TrimSpace(cmd.DirectoryName)})
		host.mu.Lock()
		result.Cancelled = host.finishGitWorkspaceImportRuntimeLocked(key)
		host.mu.Unlock()
		cancel()
		if runErr == nil {
			result.Succeeded = true
			result.WorkspacePath = output.WorkspacePath
		} else {
			var detail *workspaceimport.ImportError
			if errors.As(runErr, &detail) {
				result.ErrorCode = string(detail.Code)
				result.ErrorDestination = detail.DestinationPath
			} else {
				result.ErrorCode = "git_import_clone_failed"
			}
		}
	} else {
		runCtx, cancel := context.WithTimeout(ctx, gitWorkspaceWorktreeCommandTimeout)
		host.mu.Lock()
		key := host.beginGitWorkspaceWorktreeRuntimeLocked(o.SurfaceID, cmd.PickerID, cancel)
		host.mu.Unlock()
		var output gitworkspace.WorktreeResult
		output, runErr = runGitWorkspaceWorktreeCreate(runCtx, gitworkspace.WorktreeRequest{BaseWorkspacePath: strings.TrimSpace(cmd.WorkspaceKey), BranchName: strings.TrimSpace(cmd.BranchName), DirectoryName: strings.TrimSpace(cmd.DirectoryName)})
		host.mu.Lock()
		result.Cancelled = host.finishGitWorkspaceWorktreeRuntimeLocked(key)
		host.mu.Unlock()
		cancel()
		if runErr == nil {
			result.Succeeded = true
			result.WorkspacePath = output.WorkspacePath
		} else {
			var detail *gitmeta.WorktreeCreateError
			if errors.As(runErr, &detail) {
				result.ErrorCode = string(detail.Code)
				result.ErrorDestination = detail.DestinationPath
			} else {
				result.ErrorCode = "worktree_create_failed"
			}
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	verifier := privateLocalVerifier{o, generation, record.Effect, raw}
	if err = j.ConfirmLocalEffect(context.WithoutCancel(ctx), o, generation, id, raw, verifier); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateGitReducer{host})
}

type privateGitReducer struct{ host *App }

func (r privateGitReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	var result privateGitReceipt
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if cmd == nil || result.Version != 1 || result.EffectID != effect.ID || result.SurfaceID != event.SurfaceSessionID || result.PickerID != cmd.PickerID || result.Kind != string(cmd.Kind) {
		return nil, privatechat.ErrJournalConflict
	}
	if result.Cancelled {
		return nil, nil
	}
	switch cmd.Kind {
	case control.DaemonCommandGitWorkspaceImport:
		if result.Succeeded {
			return app.Service().CompleteTargetPickerGitImport(result.SurfaceID, result.PickerID, result.WorkspacePath), nil
		}
		return app.Service().FailTargetPickerGitImport(result.SurfaceID, result.PickerID, &workspaceimport.ImportError{Code: workspaceimport.ImportErrorCode(result.ErrorCode), DestinationPath: result.ErrorDestination}), nil
	case control.DaemonCommandGitWorkspaceWorktreeCreate:
		if result.Succeeded {
			return app.Service().CompleteTargetPickerWorktreeCreate(result.SurfaceID, result.PickerID, result.WorkspacePath), nil
		}
		return app.Service().FailTargetPickerWorktreeCreate(result.SurfaceID, result.PickerID, &gitmeta.WorktreeCreateError{Code: gitmeta.WorktreeCreateErrorCode(result.ErrorCode), DestinationPath: result.ErrorDestination}), nil
	default:
		return nil, privatechat.ErrJournalConflict
	}
}
