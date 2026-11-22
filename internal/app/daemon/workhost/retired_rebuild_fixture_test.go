package workhost

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A real isolated process owns the deterministic native RPC state used below.
func TestRetiredRebuildNativeHelper(t *testing.T) {
	if os.Getenv("RETIRED_REBUILD_NATIVE_HELPER") != "1" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	turns := 0
	for scan.Scan() {
		var request struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(scan.Bytes(), &request); err != nil {
			os.Exit(2)
		}
		result := map[string]any{}
		switch request.Method {
		case "thread/start", "thread/resume":
			result["thread"] = map[string]any{"id": "native-history"}
		case "turn/start":
			turns++
			result["turn"] = map[string]any{"id": fmt.Sprintf("native-turn-%d", turns), "status": "completed", "text": fmt.Sprintf("native control response %d", turns)}
		case "thread/read":
			if turns == 0 {
				os.Exit(3)
			}
			result["thread"] = map[string]any{"id": "native-history", "status": map[string]any{"type": "idle"}, "turns": []any{map[string]any{"id": fmt.Sprintf("native-turn-%d", turns), "status": "completed", "items": []any{}}}}
		case "thread/goal/get":
			result["goal"] = nil
		default:
			os.Exit(4)
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"id": request.ID, "result": result}); err != nil {
			os.Exit(5)
		}
	}
}

type retiredNativePipe struct {
	cmd    *exec.Cmd
	input  io.WriteCloser
	output *bufio.Scanner
	scope  *hostinventory.Scope
}

func startRetiredNativePipe(t *testing.T) *retiredNativePipe {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRetiredRebuildNativeHelper$")
	cmd.Env = append(os.Environ(), "RETIRED_REBUILD_NATIVE_HELPER=1")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	hostinventory.Prepare(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scope, err := hostinventory.Bind(cmd)
	if err != nil {
		t.Fatal(err)
	}
	pipe := &retiredNativePipe{cmd: cmd, input: input, output: bufio.NewScanner(output), scope: scope}
	t.Cleanup(func() { input.Close(); cmd.Process.Kill(); scope.Close() })
	return pipe
}
func (p *retiredNativePipe) call(frame []byte) (json.RawMessage, error) {
	if _, err := p.input.Write(append(frame, '\n')); err != nil {
		return nil, err
	}
	if !p.output.Scan() {
		return nil, fmt.Errorf("native pipe ended: %v", p.output.Err())
	}
	return append(json.RawMessage(nil), p.output.Bytes()...), nil
}

type retiredRebuildDriver struct {
	store       *workstore.Store
	journal     *runtimejournal.Store
	pipe        *retiredNativePipe
	inspections int
	offline     bool
}

func (d *retiredRebuildDriver) SendCommand(context.Context, string, agentproto.Command) error {
	return nil
}
func (d *retiredRebuildDriver) Inspect(ctx context.Context, request workruntime.InspectionRequest) (workruntime.NativeObservation, error) {
	d.inspections++
	if d.offline {
		return workruntime.NativeObservation{}, errors.New("exact carrier transport disconnected")
	}
	owner := request.Binding
	command := agentproto.Command{CommandID: fmt.Sprintf("retired-stack-inspect-%d", d.inspections), Kind: agentproto.CommandRuntimeInspect, Execution: &owner, Target: agentproto.Target{ThreadID: request.SessionID, TurnID: request.TurnID}, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: agentproto.RuntimeInspectionProtocolVersion, SubjectCommandID: request.SubjectCommandID, ExpectedInstanceGeneration: request.Carrier.InstanceGeneration, ExpectedProcessGeneration: request.Carrier.ProcessGeneration, ExpectedLocationGeneration: request.LocationGeneration}}
	err := d.store.Within(ctx, func(tx *workstore.Tx) error {
		raw, err := json.Marshal(command)
		if err != nil {
			return err
		}
		body := work.NewContent(command.CommandID+":body", owner.ScopeID, "application/json", raw, time.Now().UTC())
		if err := tx.PutContent(body); err != nil {
			return err
		}
		_, _, err = tx.PutOperation(work.Operation{ID: command.CommandID, ScopeID: owner.ScopeID, ContextID: owner.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "runtime-inspector"}, Kind: string(command.Kind), TargetID: owner.WorkerSessionID, TargetGeneration: owner.SessionGeneration, IdempotencyKey: command.CommandID, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationSending, Revision: 1}, 0)
		return err
	})
	if err != nil {
		return workruntime.NativeObservation{}, err
	}
	registrar := workruntime.New(d.store, workruntime.Options{})
	if err := registrar.RegisterCommand(ctx, request.Carrier.Incarnation, command); err != nil {
		return workruntime.NativeObservation{}, err
	}
	if _, _, err := d.journal.PrepareCommand(command); err != nil {
		return workruntime.NativeObservation{}, err
	}
	probe := func(id, method string) (agentproto.RPCProbe, error) {
		frame, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": map[string]any{"threadId": request.SessionID, "includeTurns": true}})
		if _, err := d.journal.PrepareFrame(command.CommandID, frame); err != nil {
			return agentproto.RPCProbe{}, err
		}
		response, err := d.pipe.call(frame)
		return agentproto.RPCProbe{Request: frame, Response: response}, err
	}
	native, err := probe(command.CommandID+":native", "thread/read")
	if err != nil {
		return workruntime.NativeObservation{}, err
	}
	goal, err := probe(command.CommandID+":goal", "thread/goal/get")
	if err != nil {
		return workruntime.NativeObservation{}, err
	}
	inventory := d.pipe.scope.Inspect()
	if !inventory.Complete || len(inventory.ProcessIDs) != 0 {
		return workruntime.NativeObservation{}, fmt.Errorf("native scope inventory is not empty: %+v", inventory)
	}
	observation, err := workruntime.ObserveProtocol(request, workruntime.ProtocolInspection{Native: native, Continuation: goal, Background: agentproto.BackgroundInspection{CarrierIncarnation: request.Carrier.Incarnation, ProcessGeneration: request.Carrier.ProcessGeneration, LocationGeneration: request.LocationGeneration, Complete: inventory.Complete, NativeTaskSetComplete: true, NativeInputsComplete: true, ContinuationInventoryKnown: true, Evidence: inventory.JSON()}, ObservedAt: time.Now().UTC()})
	if err != nil {
		return observation, err
	}
	observation.InspectionCommandID = command.CommandID
	err = d.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		observation.CanonicalCommandWatermark, err = tx.NativeCommandWatermark(command.CommandID)
		return err
	})
	if err == nil {
		observation.NativeWriteWatermark, err = d.journal.NativeWriteWatermark()
	}
	return observation, err
}
