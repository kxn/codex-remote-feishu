package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worktransport"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

func TestSDKApplauseShellImageClusterUsesActualStandaloneTurnWithoutLosingModel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("captured Linux app-server shell argv requires /bin/bash")
	}
	binary := buildPipelineTeamCLI(t)
	t.Setenv("FSC_NATIVE_PIPELINE_CLAUDE_STEER", "true")
	t.Setenv("FSC_NATIVE_PIPELINE_REACTION_STEER", "true")
	t.Setenv("FSC_NATIVE_PIPELINE_IMAGES", "false")
	for _, order := range []string{"native-order", "terminal-before-item"} {
		t.Run(order, func(t *testing.T) {
			t.Setenv("FSC_NATIVE_PIPELINE_PRIVATE_SHELL", order)
			runSDKNativePipeline(t, binary, work.BackendCodex)
		})
	}
}
func pipelinePendingReactionEmoji() string {
	if os.Getenv("FSC_NATIVE_PIPELINE_PRIVATE_SHELL") != "" {
		return "APPLAUSE"
	}
	return "THUMBSUP"
}

type pipelineShellAttachment struct {
	Ref      string `json:"ref"`
	Type     string `json:"type"`
	Path     string `json:"path"`
	MIMEType string `json:"mime_type"`
}
type pipelineShellBundle struct {
	Schema      string                    `json:"schema"`
	Text        string                    `json:"text"`
	Attachments []pipelineShellAttachment `json:"attachments"`
}

func pipelineSendPrivateShell(ctx context.Context, store *workstore.Store, driver *worktransport.Driver, worker work.WorkerSession, turn workstore.NativeTurn, source workstore.PrivateActionSource, ids []string, inputs []agentproto.Input) (bool, error) {
	bundle := pipelineShellBundle{Schema: "queued_input_bundle.v1"}
	for _, input := range inputs {
		if input.Type == agentproto.InputText {
			bundle.Text += input.Text + "\n"
		} else if input.Type == agentproto.InputLocalImage {
			bundle.Attachments = append(bundle.Attachments, pipelineShellAttachment{Ref: fmt.Sprintf("att-%d", len(bundle.Attachments)+1), Type: "image", Path: input.Path, MIMEType: input.MIMEType})
		}
	}
	raw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return false, err
	}
	owner := turn.Binding
	owner.AttemptID = ""
	owner.RunRef = ""
	command := agentproto.Command{CommandID: "pipeline-independent-shell-command", Kind: agentproto.CommandThreadShellCommand, Execution: &owner, Target: agentproto.Target{ThreadID: turn.SessionID, TurnID: turn.TurnID}, ShellCommand: agentproto.ShellCommand{OperationID: "pipeline-independent-shell", Payload: "<queued_input_bundle_v1>\n" + string(raw) + "\n</queued_input_bundle_v1>\n"}, IssuedAt: time.Now().UTC()}
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		action, e := tx.GetPrivateReactionAction(source.ReactionID)
		if e != nil {
			return e
		}
		_, _, e = tx.BeginPrivateShellOperation(workstore.BeginPrivateShellOperationRequest{ID: command.ShellCommand.OperationID, Source: source, InputID: action.InputID, InputIDs: ids, Command: command, Now: time.Now().UTC()})
		return e
	})
	if err != nil {
		return false, err
	}
	if err = driver.SendCommand(ctx, worker.InstanceID, command); err != nil {
		return false, err
	}
	return true, nil
}

// Inside the real native child process, execute the actual wrapper-produced
// fixed cat command. This protocol is normalized from the isolated 0.169.1
// app-server capture, including its empty RPC result, displayed shell argv,
// all turn boundaries, and both UserShell item notifications.
func pipelineNativePrivateShell(frame map[string]any, session string) error {
	params, _ := frame["params"].(map[string]any)
	script, _ := params["command"].(string)
	if params["turnId"] != nil || params["expectedTurnId"] != nil || !strings.HasPrefix(script, "cat ") {
		return fmt.Errorf("shell fixture did not receive actual session-level fixed payload command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := execlaunch.CommandContext(ctx, "/bin/bash", "-lc", script).Output()
	if err != nil {
		return fmt.Errorf("native fixed payload read failed: %w", err)
	}
	content := strings.TrimSuffix(strings.TrimPrefix(string(output), "<queued_input_bundle_v1>\n"), "\n</queued_input_bundle_v1>\n")
	var bundle pipelineShellBundle
	if json.Unmarshal([]byte(content), &bundle) != nil || bundle.Schema != "queued_input_bundle.v1" || len(bundle.Attachments) != 2 {
		return fmt.Errorf("actual shell payload lost text/image reference contract")
	}
	envelope, ok := pipelineFindEnvelope(bundle.Text)
	if !ok || len(envelope.Inputs) != 3 {
		return fmt.Errorf("shell context lost exact three-source provenance")
	}
	for i, id := range []string{"steer-image-one", "steer-image-two", "steer-caption"} {
		input := envelope.Inputs[i]
		if input.ID == "" || input.Message == nil || input.Message.ID == "" || input.Message.PlatformKey == nil || input.Message.PlatformKey.MessageID != id {
			return fmt.Errorf("shell source %d lost exact SDK identity", i)
		}
	}
	if !strings.Contains(bundle.Text, "Use these two queued pictures in this running request.") {
		return fmt.Errorf("shell context lost readable human instruction")
	}
	for index, asset := range bundle.Attachments {
		data, e := os.ReadFile(asset.Path)
		if e != nil {
			return e
		}
		if asset.Type != "image" || asset.MIMEType != "image/png" || !bytes.Equal(data, pipelineMediaBytes(index+1)) {
			return fmt.Errorf("shell referenced image bytes/order did not match SDK input")
		}
	}
	pipelineEmit(map[string]any{"id": frame["id"], "result": map[string]any{}})
	turn := "pipeline-native-shell-turn"
	start := map[string]any{"method": "turn/started", "params": map[string]any{"threadId": session, "turn": map[string]any{"id": turn, "rootTurnId": turn, "items": []any{}, "itemsView": "notLoaded", "status": "inProgress", "error": nil}}}
	end := map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": session, "turn": map[string]any{"id": turn, "rootTurnId": turn, "items": []any{}, "itemsView": "notLoaded", "status": "completed", "error": nil}}}
	item := map[string]any{"id": "pipeline-actual-shell-item", "type": "commandExecution", "source": "userShell", "command": fmt.Sprintf("/bin/bash -lc %q", script), "status": "inProgress", "aggregatedOutput": nil, "exitCode": nil}
	pipelineEmit(start)
	if os.Getenv("FSC_NATIVE_PIPELINE_PRIVATE_SHELL") == "terminal-before-item" {
		pipelineEmit(end)
	}
	pipelineEmit(map[string]any{"method": "item/started", "params": map[string]any{"threadId": session, "turnId": turn, "item": item}})
	item["status"] = "completed"
	item["aggregatedOutput"] = string(output)
	item["exitCode"] = 0
	pipelineEmit(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": session, "turnId": turn, "item": item}})
	if os.Getenv("FSC_NATIVE_PIPELINE_PRIVATE_SHELL") != "terminal-before-item" {
		pipelineEmit(end)
	}
	return nil
}

func verifyPipelinePrivateShellSettled(t *testing.T, store *workstore.Store, scope work.WorkScope, original work.Inbox) {
	t.Helper()
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		shell, e := tx.GetPrivateShellOperation("pipeline-independent-shell")
		if e != nil {
			return e
		}
		if shell.State != "completed" || !shell.Acknowledged || shell.ActualTurnID != "pipeline-native-shell-turn" || shell.TurnStartedSource == nil || shell.TurnTerminalSource == nil {
			return fmt.Errorf("full native lifecycle did not settle independent shell: %s", shell.State)
		}
		if _, e = tx.GetNativeTurn(shell.Incarnation, shell.SessionID, shell.ActualTurnID); !errors.Is(e, workstore.ErrNotFound) {
			return fmt.Errorf("shell standalone turn became model business turn")
		}
		if _, e = tx.GetRuntimeGate(shell.Execution.WorkerSessionID); !errors.Is(e, workstore.ErrNotFound) {
			return fmt.Errorf("standalone shell left model runtime gated")
		}
		for _, id := range shell.InputIDs {
			input, e := tx.GetInbox(id)
			if e != nil {
				return e
			}
			if input.State != work.InboxProcessed || input.ClaimedAttemptID != "" || input.ShellOperationID != shell.ID {
				return fmt.Errorf("shell source was lost or consumed by model")
			}
		}
		attempts, e := tx.ListAttempts(scope.ID)
		if e != nil {
			return e
		}
		for _, attempt := range attempts {
			for _, id := range attempt.InputIDs {
				if id != original.ID {
					return fmt.Errorf("independent shell input polluted original model attempt")
				}
			}
		}
		receipt, e := tx.GetPrivateReaction("input-reaction:" + shell.InputID)
		if e != nil {
			return e
		}
		desired, _, e := tx.PrivateInputReactionState(receipt)
		if e != nil {
			return e
		}
		if len(desired) != 1 || desired[0] != "APPLAUSE" {
			return fmt.Errorf("native accepted shell did not retain original applause feedback")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
