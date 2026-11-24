package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// runPrivateStageInput executes only a committed source action. An interrupted
// stage is reconciled by hashes and never overwrites a different target file.
func runPrivateStageInput(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	record, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(record.Effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	if cmd == nil || cmd.Kind != control.DaemonCommandPrivateStageInput || cmd.SurfaceSessionID != o.SurfaceID || event.SurfaceSessionID != o.SurfaceID || cmd.InstanceID == "" || !filepath.IsAbs(cmd.ThreadCWD) || filepath.Clean(cmd.ThreadCWD) != cmd.ThreadCWD {
		return nil, privatechat.ErrJournalConflict
	}
	var action control.Action
	if err = json.Unmarshal([]byte(cmd.Text), &action); err != nil {
		return nil, err
	}
	if action.GatewayID != o.GatewayID || action.SurfaceSessionID != o.SurfaceID || action.ChatID != o.ChatID || action.ActorUserID != o.UserID || (action.Kind != control.ActionFileMessage && action.Kind != control.ActionTextMessage) {
		return nil, privatechat.ErrJournalConflict
	}
	sources, err := j.IngressSourcesForEffect(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	owned := map[string]bool{}
	for _, source := range sources {
		owned[source] = true
	}
	if action.Kind == control.ActionFileMessage {
		if !owned[action.MessageID] {
			return nil, privatechat.ErrJournalConflict
		}
	} else {
		for _, file := range action.Files {
			source := file.SourceMessageID
			if source == "" {
				source = action.MessageID
			}
			if !owned[source] {
				return nil, privatechat.ErrJournalConflict
			}
		}
	}
	if cmd.StageObjectID == "" && !action.RecoveryNewThread && action.RecoverySessionID == "" && record.State != "confirmed" {
		if record.State == "prepared" {
			if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
				return nil, err
			}
		}
		receipt, e := json.Marshal(privateStageReceipt{Version: 1, EffectID: id, SurfaceID: o.SurfaceID, LegacyTargetUnknown: true})
		if e != nil {
			return nil, e
		}
		if e = j.ConfirmLocalEffect(ctx, o, generation, id, receipt, privateLocalVerifier{o, generation, record.Effect, receipt}); e != nil {
			return nil, e
		}
		return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateStageReducer{host: host, journal: j})
	}
	if cmd.StageObjectID != "" && record.State != "confirmed" {
		cancelled := false
		err = p.InspectPendingHostIntent(ctx, o, generation, id, func(app *privatechat.App, current privatechat.EffectRecord) error {
			pending, e := app.Service().PrivateStagePending(o.SurfaceID, cmd.StageObjectID, action.MessageID, action.ActorUserID)
			if e != nil || pending {
				return e
			}
			if current.State == "prepared" {
				if e = j.MarkEffectSending(ctx, o, generation, id); e != nil {
					return e
				}
			}
			receipt, e := json.Marshal(privateStageReceipt{Version: 1, EffectID: id, SurfaceID: o.SurfaceID, Cancelled: true})
			if e != nil {
				return e
			}
			if e = j.ConfirmLocalEffect(ctx, o, generation, id, receipt, privateLocalVerifier{o, generation, current.Effect, receipt}); e != nil {
				return e
			}
			cancelled = true
			return nil
		})
		if err != nil {
			return nil, err
		}
		if cancelled {
			if err = preparePrivateStageCleanupIfPresent(ctx, j, o, generation, id, action); err != nil {
				return nil, err
			}
			return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateStageReducer{host: host, journal: j})
		}
	}
	if record.State == "confirmed" {
		var saved privateStageReceipt
		if err = json.Unmarshal(record.Receipt, &saved); err != nil {
			return nil, err
		}
		if cmd.StageObjectID == "" {
			// Legacy source files remain available when their original route is unproven.
		} else if saved.Cancelled || saved.Failed {
			if err = preparePrivateStageCleanupIfPresent(ctx, j, o, generation, id, action); err != nil {
				return nil, err
			}
		} else if err = preparePrivateStageCleanup(ctx, j, o, generation, id, action.MessageID); err != nil {
			return nil, err
		}
		return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateStageReducer{host: host, journal: j})
	}
	if record.State != "prepared" && record.State != "unknown" {
		return nil, fmt.Errorf("attachment stage requires reconciliation")
	}
	canonical, err := filepath.EvalSymlinks(cmd.ThreadCWD)
	if err != nil || canonical != cmd.ThreadCWD {
		return settlePrivateStageFailure(ctx, host, p, j, o, generation, record, cmd, fmt.Errorf("附件工作目录已变化"))
	}
	root, err := os.OpenRoot(cmd.ThreadCWD)
	if err != nil {
		return settlePrivateStageFailure(ctx, host, p, j, o, generation, record, cmd, err)
	}
	defer root.Close()
	workspace, err := root.Open(".")
	if err != nil {
		return settlePrivateStageFailure(ctx, host, p, j, o, generation, record, cmd, err)
	}
	defer workspace.Close()
	identity, err := privateDirectoryIdentity(workspace)
	if err != nil {
		return settlePrivateStageFailure(ctx, host, p, j, o, generation, record, cmd, err)
	}
	stored, err := j.LoadLocalIntent(ctx, o, generation, id)
	var intent privateStageIntent
	if errors.Is(err, sql.ErrNoRows) && record.State == "prepared" {
		intent, err = privateBuildStageIntent(cmd.ThreadCWD, identity, action)
		if err != nil {
			return settlePrivateStageFailure(ctx, host, p, j, o, generation, record, cmd, err)
		}
		stored, err = json.Marshal(intent)
		if err != nil {
			return nil, err
		}
		if err = j.PrepareLocalIntent(ctx, o, generation, id, stored); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else if err = json.Unmarshal(stored, &intent); err != nil {
		return nil, err
	}
	if intent.Version != 1 || intent.Workspace != cmd.ThreadCWD || intent.WorkspaceIdentity != identity || len(intent.Files) == 0 {
		return nil, privatechat.ErrJournalConflict
	}
	if record.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
			return nil, err
		}
	}
	fail := func(cause error) ([]privatechat.EffectRecord, error) {
		if cmd.StageObjectID == "" {
			return nil, cause
		}
		return settlePrivateStageFailure(ctx, host, p, j, o, generation, record, cmd, cause)
	}
	for _, file := range intent.Files {
		if err = privateStageOne(root, cmd.ThreadCWD, file); err != nil {
			return fail(err)
		}
	}
	current, err := os.Open(cmd.ThreadCWD)
	if err != nil {
		return fail(err)
	}
	currentID, err := privateDirectoryIdentity(current)
	current.Close()
	if err != nil || currentID != identity {
		return fail(fmt.Errorf("attachment workspace replaced"))
	}
	if err = ensureWorkspaceContextGitExclude(cmd.ThreadCWD); err != nil {
		return fail(err)
	}
	receipt, err := json.Marshal(privateStageReceipt{Version: 1, EffectID: id, SurfaceID: o.SurfaceID, Files: intent.Files})
	if err != nil {
		return nil, err
	}
	verifier := privateLocalVerifier{o, generation, record.Effect, receipt}
	if err = j.ConfirmLocalEffect(context.WithoutCancel(ctx), o, generation, id, receipt, verifier); err != nil {
		return nil, err
	}
	if cmd.StageObjectID != "" {
		if err = preparePrivateStageCleanup(ctx, j, o, generation, id, action.MessageID); err != nil {
			return nil, err
		}
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateStageReducer{host: host, journal: j})
}

func preparePrivateStageCleanupIfPresent(ctx context.Context, j *privatechat.Journal, o privatechat.Owner, generation int64, id string, action control.Action) error {
	if _, err := j.LoadLocalIntent(ctx, o, generation, id); err == nil {
		return preparePrivateStageCleanup(ctx, j, o, generation, id, action.MessageID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	files := action.Files
	if action.Kind == control.ActionFileMessage {
		files = []control.ActionFileAttachment{{SourceMessageID: action.MessageID, LocalPath: action.LocalPath}}
	}
	for _, file := range files {
		if file.LocalPath == "" {
			continue
		}
		message := file.SourceMessageID
		if message == "" {
			message = action.MessageID
		}
		// Empty hash asks the existing cleanup worker to verify the trusted SDK
		// staging manifest; no source filesystem I/O precedes cancellation.
		if err := j.PrepareStagingCleanup(ctx, o, generation, id, message, map[string]string{file.LocalPath: ""}); err != nil {
			return err
		}
	}
	return nil
}

func preparePrivateStageCleanup(ctx context.Context, j *privatechat.Journal, o privatechat.Owner, generation int64, id, messageID string) error {
	raw, err := j.LoadLocalIntent(ctx, o, generation, id)
	if err != nil {
		return err
	}
	var intent privateStageIntent
	if err = json.Unmarshal(raw, &intent); err != nil {
		return err
	}
	files := make(map[string]string, len(intent.Files))
	for _, file := range intent.Files {
		files[file.Source] = file.SHA256
	}
	return j.PrepareStagingCleanup(ctx, o, generation, id, messageID, files)
}

type privateStageReducer struct {
	host    *App
	journal *privatechat.Journal
}

func (r privateStageReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	var receipt privateStageReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		return nil, err
	}
	if cmd == nil || cmd.Kind != control.DaemonCommandPrivateStageInput || receipt.Version != 1 || receipt.EffectID != effect.ID || receipt.SurfaceID != event.SurfaceSessionID || (receipt.Cancelled && receipt.Failed) || (receipt.LegacyTargetUnknown && (cmd.StageObjectID != "" || receipt.Cancelled || receipt.Failed || len(receipt.Files) != 0)) || ((receipt.Cancelled || receipt.Failed) && len(receipt.Files) != 0) || (!receipt.Cancelled && !receipt.Failed && !receipt.LegacyTargetUnknown && len(receipt.Files) == 0) {
		return nil, privatechat.ErrJournalConflict
	}
	var action control.Action
	if err = json.Unmarshal([]byte(cmd.Text), &action); err != nil {
		return nil, err
	}
	if err = app.ValidateAction(action); err != nil {
		return nil, err
	}
	if cmd.StageObjectID != "" && receipt.Failed {
		return app.Service().FailPrivateStage(action.SurfaceSessionID, cmd.StageObjectID, action.MessageID, action.ActorUserID, receipt.FailureReason)
	}
	if cmd.StageObjectID != "" && receipt.Cancelled {
		pending, e := app.Service().PrivateStagePending(action.SurfaceSessionID, cmd.StageObjectID, action.MessageID, action.ActorUserID)
		if e != nil {
			return nil, e
		}
		if pending {
			return nil, privatechat.ErrJournalConflict
		}
		return nil, nil
	}
	if cmd.StageObjectID != "" {
		var files []control.ActionFileAttachment
		for _, file := range receipt.Files {
			if file.Target == "" || !filepath.IsLocal(file.Target) || filepath.Clean(file.Target) != file.Target || file.SHA256 == "" {
				return nil, privatechat.ErrJournalConflict
			}
			files = append(files, control.ActionFileAttachment{SourceMessageID: file.SourceMessageID, LocalPath: filepath.Join(cmd.ThreadCWD, file.Target), FileName: file.Name})
		}
		var path, name string
		if action.Kind == control.ActionFileMessage {
			if len(files) != 1 {
				return nil, privatechat.ErrJournalConflict
			}
			path, name = files[0].LocalPath, files[0].FileName
		}
		return app.Service().CompletePrivateStage(action.SurfaceSessionID, cmd.StageObjectID, action.MessageID, action.ActorUserID, path, name, inboundFileAttachmentPrompt(files))
	}
	if r.journal == nil {
		return nil, privatechat.ErrJournalState
	}
	discarded, e := r.journal.LegacyStageRouteDiscarded(context.Background(), effect.ID)
	if e != nil {
		return nil, e
	}
	if discarded || receipt.LegacyTargetUnknown || (!action.RecoveryNewThread && action.RecoverySessionID == "") {
		return legacyStageTargetUnknown(action), nil
	}
	surface := app.Service().Surface(action.SurfaceSessionID)
	instance := app.Service().Instance(cmd.InstanceID)
	if surface == nil {
		return nil, privatechat.ErrDeferred
	}
	if surface.AttachedInstanceID != cmd.InstanceID || (action.RecoveryNewThread && (surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "")) || (action.RecoverySessionID != "" && surface.SelectedThreadID != action.RecoverySessionID) {
		return legacyStageTargetUnknown(action), nil
	}
	if instance == nil || !instance.Online {
		return nil, privatechat.ErrDeferred
	}
	if filepath.Clean(instance.WorkspaceRoot) != cmd.ThreadCWD {
		return legacyStageTargetUnknown(action), nil
	}
	var files []control.ActionFileAttachment
	for _, file := range receipt.Files {
		if file.Target == "" || !filepath.IsLocal(file.Target) || filepath.Clean(file.Target) != file.Target || file.SHA256 == "" {
			return nil, privatechat.ErrJournalConflict
		}
		files = append(files, control.ActionFileAttachment{SourceMessageID: file.SourceMessageID, LocalPath: filepath.Join(cmd.ThreadCWD, file.Target), FileName: file.Name})
	}
	if action.Kind == control.ActionFileMessage {
		if len(files) != 1 {
			return nil, privatechat.ErrJournalConflict
		}
		action.LocalPath = files[0].LocalPath
		action.FileName = files[0].FileName
	} else if action.Kind == control.ActionTextMessage {
		action.Files = files
		if prompt := inboundFileAttachmentPrompt(files); prompt != "" {
			action.Inputs = append([]agentproto.Input{{Type: agentproto.InputText, Text: prompt}}, action.Inputs...)
		}
	} else {
		return nil, privatechat.ErrJournalConflict
	}
	return app.Action(action)
}

func settlePrivateStageFailure(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, record privatechat.EffectRecord, cmd *control.DaemonCommand, cause error) ([]privatechat.EffectRecord, error) {
	if cmd.StageObjectID == "" {
		return nil, fmt.Errorf("attachment staging failed")
	}
	current, err := j.LoadEffect(ctx, o, generation, record.Effect.ID)
	if err != nil {
		return nil, err
	}
	if current.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, record.Effect.ID); err != nil {
			return nil, err
		}
	}
	reason := "附件暂存失败"
	if errors.Is(cause, os.ErrNotExist) {
		reason = "附件文件或目录不存在"
	} else if errors.Is(cause, os.ErrPermission) {
		reason = "没有权限访问附件或工作目录"
	} else if cause != nil && strings.Contains(cause.Error(), "附件工作目录") {
		reason = "附件工作目录已变化"
	}
	detail := ""
	if cause != nil {
		detail = cause.Error()
	}
	receipt, err := json.Marshal(privateStageReceipt{Version: 1, EffectID: record.Effect.ID, SurfaceID: o.SurfaceID, Failed: true, FailureReason: reason, InternalError: detail})
	if err != nil {
		return nil, err
	}
	if err = j.ConfirmLocalEffect(ctx, o, generation, record.Effect.ID, receipt, privateLocalVerifier{o, generation, record.Effect, receipt}); err != nil {
		return nil, err
	}
	var action control.Action
	if err = json.Unmarshal([]byte(cmd.Text), &action); err != nil {
		return nil, err
	}
	if err = preparePrivateStageCleanupIfPresent(ctx, j, o, generation, record.Effect.ID, action); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, record.Effect.ID, privateStageReducer{host: host, journal: j})
}

func legacyStageTargetUnknown(action control.Action) []eventcontract.Event {
	return []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID, Notice: &control.Notice{Code: "legacy_attachment_target_unavailable", Text: "无法确认旧版附件的原目标，未自动发送。请重新发送附件。"}}}
}
