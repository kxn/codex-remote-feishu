package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worktransport"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestSDKClaudeRunningTurnSteerReceivesImagesAndConfirmsExactUUID(t *testing.T) {
	binary := buildPipelineTeamCLI(t)
	t.Setenv("FSC_NATIVE_PIPELINE_CLAUDE_STEER", "true")
	t.Setenv("FSC_NATIVE_PIPELINE_IMAGES", "false")
	runSDKNativePipeline(t, binary, work.BackendClaude)
}

func TestSDKReactionCaptionSteerCarriesStagedImagesToNative(t *testing.T) {
	binary := buildPipelineTeamCLI(t)
	t.Setenv("FSC_NATIVE_PIPELINE_CLAUDE_STEER", "true")
	t.Setenv("FSC_NATIVE_PIPELINE_IMAGES", "false")
	t.Setenv("FSC_NATIVE_PIPELINE_REACTION_STEER", "true")
	for _, backend := range []work.Backend{work.BackendCodex, work.BackendClaude} {
		t.Run(string(backend), func(t *testing.T) { runSDKNativePipeline(t, binary, backend) })
	}
}

func pipelineInjectClaudeSteer(t *testing.T, ctx context.Context, store *workstore.Store, service *workservice.Service, driver *worktransport.Driver, ingress *workingress.Ingress, send func(work.RawIngressEnvelope), raw work.RawIngressEnvelope, scope work.WorkScope, original work.Inbox, root string, handler *pipelineSteerCommandHandler) (bool, error) {
	t.Helper()
	var attempt work.ExecutionAttempt
	var worker work.WorkerSession
	var turn workstore.NativeTurn
	var canonical agentproto.Command
	err := store.Within(ctx, func(tx *workstore.Tx) error {
		attempts, e := tx.ListAttempts(scope.ID)
		if e != nil {
			return e
		}
		for _, candidate := range attempts {
			if candidate.State != work.AttemptRunning {
				continue
			}
			for _, id := range candidate.InputIDs {
				if id == original.ID {
					attempt = candidate
				}
			}
		}
		if attempt.ID == "" {
			return nil
		}
		worker, e = tx.GetWorker(attempt.RunRef.WorkerID)
		if e != nil {
			return e
		}
		turns, e := tx.ListNativeTurns(attempt.ID)
		if e != nil {
			return e
		}
		for _, candidate := range turns {
			if candidate.Started && candidate.TerminalStatus == "" && candidate.CarrierIncarnation == worker.CarrierIncarnation {
				turn = candidate
			}
		}
		if turn.TurnID == "" {
			attempt = work.ExecutionAttempt{}
			return nil
		}
		canonical, e = tx.NativeCommandPayload(attempt.CommandID, worker.CarrierIncarnation)
		return e
	})
	if err != nil || attempt.ID == "" {
		return false, err
	}
	for _, name := range []string{"image-one", "image-two"} {
		image := raw
		image.EventID = "steer-" + name
		image.MessageID = image.EventID
		image.MessageType = "image"
		image.RawContent = fmt.Sprintf(`{"image_key":%q}`, name)
		send(image)
	}
	caption := raw
	caption.EventID = "steer-caption"
	caption.MessageID = caption.EventID
	caption.RawContent = `{"text":"Use these two queued pictures in this running request."}`
	send(caption)
	request := raw
	request.EventID = "explicit-steerall"
	request.MessageID = request.EventID
	request.RawContent = `{"text":"/steerall"}`
	if os.Getenv("FSC_NATIVE_PIPELINE_REACTION_STEER") != "true" {
		send(request)
	}
	processed, err := ingress.ProcessPending(ctx, 16)
	if err != nil || len(processed.Failures) > 0 {
		return false, fmt.Errorf("steer SDK processing: %+v %v", processed, err)
	}
	var ids []string
	var source workstore.PrivateActionSource
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		pending, e := tx.ListPendingInputs(scope.ID, scope.RoleID)
		if e != nil {
			return e
		}
		sort.SliceStable(pending, func(i, j int) bool {
			if pending[i].AvailableAt.Equal(pending[j].AvailableAt) {
				return pending[i].ID < pending[j].ID
			}
			return pending[i].AvailableAt.Before(pending[j].AvailableAt)
		})
		for _, input := range pending {
			if input.RecipientContextID == attempt.ContextID {
				ids = append(ids, input.ID)
			}
		}
		if os.Getenv("FSC_NATIVE_PIPELINE_REACTION_STEER") == "true" {
			for _, input := range pending {
				message, e := tx.GetMessage(input.MessageID)
				if e != nil {
					return e
				}
				if message.PlatformKey == nil || message.PlatformKey.MessageID != caption.MessageID {
					continue
				}
				cluster, e := tx.PendingInputMediaCluster(input)
				if e != nil {
					return e
				}
				ids = nil
				for _, member := range cluster {
					ids = append(ids, member.ID)
				}
				gateway, _ := (pipelineDirectory{}).ResolveGateway(ctx, raw.GatewayRef, raw.AppID)
				facts, _ := json.Marshal(feishu.PlatformIngressEvent{GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: "caption-thumbsup", Tenant: raw.Tenant, ChatID: raw.ChatID, MessageID: caption.MessageID, Kind: "reaction.created", OperatorType: "user", OperatorIDs: raw.SenderIDs, Emoji: pipelinePendingReactionEmoji(), ReceivedAt: time.Now().UTC()})
				id, _, e := tx.AcceptPlatformObservation(workstore.PlatformObservation{Gateway: gateway, EventID: "caption-thumbsup", Tenant: raw.Tenant, ChatID: raw.ChatID, MessageID: caption.MessageID, Kind: "reaction.created", Facts: facts, ReceivedAt: time.Now().UTC()})
				if e != nil {
					return e
				}
				if e = tx.RegisterPrivateReactionAction(id); e != nil {
					return e
				}
				if os.Getenv("FSC_NATIVE_PIPELINE_PRIVATE_SHELL") == "" {
					if e = tx.FinishPrivateReactionAction(id, ""); e != nil {
						return e
					}
				}
				source = workstore.PrivateActionSource{PersonalScopeID: scope.ID, ReactionID: id}
			}
			return nil
		}
		commands, e := tx.ListPendingPrivateCommands()
		if e != nil {
			return e
		}
		for _, command := range commands {
			if command.Text == "/steerall" && command.ScopeID == scope.ID {
				source = workstore.PrivateActionSource{PersonalScopeID: scope.ID, CommandID: command.ID}
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if len(ids) != 3 || (source.CommandID == "" && source.ReactionID == "") {
		return false, fmt.Errorf("exact user steer source/three queued inputs unavailable")
	}
	inputs, err := service.BuildSteerWorkPrompt(ctx, attempt.ID, ids, filepath.Join(root, "team-attachments"))
	if err != nil {
		return false, fmt.Errorf("build exact steer prompt: %w", err)
	}
	if os.Getenv("FSC_NATIVE_PIPELINE_PRIVATE_SHELL") != "" {
		return pipelineSendPrivateShell(ctx, store, driver, worker, turn, source, ids, inputs)
	}
	binding := turn.Binding
	command := agentproto.Command{CommandID: "explicit-claude-steer-command", Kind: agentproto.CommandTurnSteer, Execution: &binding, InstructionRevision: canonical.InstructionRevision, Target: agentproto.Target{ThreadID: turn.SessionID, TurnID: turn.TurnID}, Prompt: agentproto.Prompt{Inputs: inputs}, NativeInputAdmission: &agentproto.NativeInputAdmissionCommand{Version: 1, ID: "explicit-claude-steer", InputIDs: ids}, IssuedAt: time.Now().UTC()}
	var handlerError error
	handler.handle = func(callCtx context.Context, typed work.TypedCommandEnvelope) (workingress.PrivateCommandReceipt, error) {
		actualSource := source
		actualSource.CommandID = typed.ID
		err := store.Within(callCtx, func(tx *workstore.Tx) error {
			_, _, e := tx.BeginAttemptInputAdmission(workstore.BeginAttemptInputAdmissionRequest{ID: "explicit-claude-steer", AttemptID: attempt.ID, RunRef: attempt.RunRef, ExpectedWorkerRevision: worker.Revision, InputIDs: ids, Command: command, Source: actualSource, Now: time.Now()})
			return e
		})
		if err == nil {
			err = driver.SendCommand(callCtx, worker.InstanceID, command)
		}
		handlerError = err
		return workingress.PrivateCommandReceipt{Receipt: "native steer request sent; awaiting exact UUID"}, err
	}
	if source.ReactionID != "" {
		_, err := handler.handle(ctx, work.TypedCommandEnvelope{})
		return err == nil, err
	}
	count, err := ingress.DispatchPrivateCommands(ctx, 1)
	if err != nil {
		return false, err
	}
	if handlerError != nil {
		return false, handlerError
	}
	if count != 1 {
		return false, fmt.Errorf("literal /steerall was not dispatched")
	}
	return true, nil
}

func verifyPipelineClaudeSteerSettled(t *testing.T, store *workstore.Store, scope work.WorkScope) {
	t.Helper()
	if e := store.Within(t.Context(), func(tx *workstore.Tx) error {
		admission, e := tx.GetAttemptInputAdmission("explicit-claude-steer")
		if e != nil {
			return e
		}
		if admission.State != work.InputAdmissionConfirmed || admission.NativeSource == nil {
			return fmt.Errorf("native Claude UUID echo did not confirm admission: %s", admission.State)
		}
		receipt, e := tx.GetContent(scope.ID, admission.NativeSource.EvidenceRef)
		if e != nil {
			return e
		}
		var event agentproto.Event
		if e = json.Unmarshal(receipt.Data, &event); e != nil {
			return e
		}
		if event.Kind != agentproto.EventNativeTurnInputAccepted || event.RequestID == "" || event.Execution == nil || event.ThreadID != admission.NativeSessionID || event.TurnID != admission.NativeTurnID {
			return fmt.Errorf("Claude accepted without exact native UUID/thread/turn evidence")
		}
		for _, id := range admission.InputIDs {
			input, e := tx.GetInbox(id)
			if e != nil {
				return e
			}
			if input.State != work.InboxProcessed {
				return fmt.Errorf("accepted Claude queued input not settled: %s", input.State)
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

type pipelineSteerCommandHandler struct {
	handle func(context.Context, work.TypedCommandEnvelope) (workingress.PrivateCommandReceipt, error)
}

func (h *pipelineSteerCommandHandler) Handle(ctx context.Context, command work.TypedCommandEnvelope) (workingress.PrivateCommandReceipt, error) {
	if h.handle == nil {
		return workingress.PrivateCommandReceipt{}, fmt.Errorf("steer callback not ready")
	}
	return h.handle(ctx, command)
}
