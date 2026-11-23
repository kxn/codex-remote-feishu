package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	if record.State == "confirmed" {
		if err = preparePrivateStageCleanup(ctx, j, o, generation, id, action.MessageID); err != nil {
			return nil, err
		}
		return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateStageReducer{host})
	}
	if record.State != "prepared" && record.State != "unknown" {
		return nil, fmt.Errorf("attachment stage requires reconciliation")
	}
	canonical, err := filepath.EvalSymlinks(cmd.ThreadCWD)
	if err != nil || canonical != cmd.ThreadCWD {
		return nil, fmt.Errorf("attachment workspace changed")
	}
	root, err := os.OpenRoot(cmd.ThreadCWD)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	workspace, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer workspace.Close()
	identity, err := privateDirectoryIdentity(workspace)
	if err != nil {
		return nil, err
	}
	stored, err := j.LoadLocalIntent(ctx, o, generation, id)
	var intent privateStageIntent
	if errors.Is(err, sql.ErrNoRows) && record.State == "prepared" {
		intent, err = privateBuildStageIntent(cmd.ThreadCWD, identity, action)
		if err != nil {
			return nil, err
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
		if record.State == "prepared" {
			raw, _ := json.Marshal(map[string]string{"error": cause.Error()})
			_ = j.ResolveEffect(context.WithoutCancel(ctx), o, generation, id, "unknown", raw, nil)
		}
		return nil, cause
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
	if err = preparePrivateStageCleanup(ctx, j, o, generation, id, action.MessageID); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateStageReducer{host})
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

type privateStageReducer struct{ host *App }

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
	if cmd == nil || cmd.Kind != control.DaemonCommandPrivateStageInput || receipt.Version != 1 || receipt.EffectID != effect.ID || receipt.SurfaceID != event.SurfaceSessionID || len(receipt.Files) == 0 {
		return nil, privatechat.ErrJournalConflict
	}
	var action control.Action
	if err = json.Unmarshal([]byte(cmd.Text), &action); err != nil {
		return nil, err
	}
	if err = app.ValidateAction(action); err != nil {
		return nil, err
	}
	instanceAction := action
	instanceAction.InstanceID = cmd.InstanceID
	if err = app.ValidateAction(instanceAction); err != nil {
		return nil, privatechat.ErrDeferred
	}
	surface := app.Service().Surface(action.SurfaceSessionID)
	instance := app.Service().Instance(cmd.InstanceID)
	if surface == nil || surface.AttachedInstanceID != cmd.InstanceID || instance == nil || !instance.Online || filepath.Clean(instance.WorkspaceRoot) != cmd.ThreadCWD || (action.RecoveryNewThread && (surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "")) || (action.RecoverySessionID != "" && surface.SelectedThreadID != action.RecoverySessionID) {
		return nil, privatechat.ErrDeferred
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
