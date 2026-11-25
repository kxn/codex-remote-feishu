package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/pathcompare"
)

type privateNativeState struct {
	snapshot       agentproto.PrivateNativeLaunchSnapshot
	journal        *runtimejournal.Store
	inspection     *workInspectionState
	mu             sync.Mutex
	frames         map[*byte]workFrameReference
	requests       map[string]string
	liveBindings   map[string]agentproto.NativeTurnBinding
	publishWritten func([]agentproto.Event) error
}

func readPrivateNativeLaunch(path string) (*agentproto.PrivateNativeLaunchSnapshot, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("private launch must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Size() > maxWorkLaunchSnapshotBytes || (runtime.GOOS != "windows" && opened.Mode().Perm()&077 != 0) {
		return nil, fmt.Errorf("private launch requires unchanged owner-only file")
	}
	dec := json.NewDecoder(io.LimitReader(f, maxWorkLaunchSnapshotBytes+1))
	dec.DisallowUnknownFields()
	var snapshot agentproto.PrivateNativeLaunchSnapshot
	if err = dec.Decode(&snapshot); err != nil {
		return nil, err
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("private launch must contain one JSON object")
	}
	if err = snapshot.Validate(); err != nil {
		return nil, err
	}
	return &snapshot, nil
}
func newPrivateNativeState(cfg Config) (*privateNativeState, error) {
	if cfg.PrivateNativeLaunch == nil {
		return nil, nil
	}
	if cfg.WorkLaunchSnapshot != nil {
		return nil, fmt.Errorf("team and private launch are mutually exclusive")
	}
	snapshot := *cfg.PrivateNativeLaunch
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Managed || cfg.Source != "headless" || cfg.InstanceID != snapshot.Owner.InstanceID || cfg.Backend != snapshot.Owner.Backend || !pathcompare.SameCleanPlatformPath(cfg.WorkspaceRoot, snapshot.CWD) {
		return nil, fmt.Errorf("private launch carrier, backend or cwd mismatch")
	}
	mode, err := wrapperBackendFromArgs(cfg.Args)
	if err != nil || mode != snapshot.Owner.Backend {
		return nil, fmt.Errorf("private launch mode does not match backend")
	}
	j, err := runtimejournal.Open(context.Background(), snapshot.JournalPath, cfg.RuntimePaths.StateDir, snapshot.CWD, snapshot.Owner.CarrierIncarnation)
	if err != nil {
		return nil, err
	}
	if err = j.BindPrivateLaunchSnapshot(snapshot); err != nil {
		j.Close()
		return nil, err
	}
	inspection := newWorkInspectionState()
	if err = inspection.restorePrivate(j, snapshot.Owner); err != nil {
		j.Close()
		return nil, err
	}
	return &privateNativeState{snapshot: snapshot, journal: j, inspection: inspection, frames: map[*byte]workFrameReference{}, requests: map[string]string{}}, nil
}
func (a *App) privateNativeProtocolVersion() int {
	if a.privateNative != nil && a.workErr == nil {
		return agentproto.PrivateNativeProtocolVersion
	}
	return 0
}

// Existing private command catalog, grouped by whether an active execution is required.
func privateCommandRequiresExecution(kind agentproto.CommandKind) (bool, bool) {
	switch kind {
	case agentproto.CommandPromptSend, agentproto.CommandTurnSteer, agentproto.CommandTurnInterrupt, agentproto.CommandRequestRespond, agentproto.CommandThreadShellCommand, agentproto.CommandReviewStart:
		return true, true
	case agentproto.CommandRuntimeInspect, agentproto.CommandThreadsRefresh, agentproto.CommandThreadHistoryRead, agentproto.CommandModelList, agentproto.CommandThreadRead, agentproto.CommandThreadCompactStart, agentproto.CommandProcessChildRestart, agentproto.CommandProcessExit, agentproto.CommandMCPOAuthLogin, agentproto.CommandThreadGoalSet, agentproto.CommandThreadGoalGet, agentproto.CommandThreadGoalClear:
		return false, true
	default:
		return false, false
	}
}
func (s *privateNativeState) prepare(command agentproto.Command) (agentproto.Command, error) {
	required, allowed := privateCommandRequiresExecution(command.Kind)
	if !allowed || command.Execution != nil || command.NativeBinding == nil || command.NativeBinding.Validate(required) != nil || command.NativeBinding.Owner != s.snapshot.Owner || strings.TrimSpace(command.CommandID) == "" {
		return command, fmt.Errorf("private command requires exact native owner and binding")
	}
	if command.Target.CWD != "" && !pathcompare.SameCleanPlatformPath(command.Target.CWD, s.snapshot.CWD) {
		return command, fmt.Errorf("private command cwd differs from launch")
	}
	if command.Kind == agentproto.CommandPromptSend && (command.Target.CWD == "" || len(command.Prompt.Inputs) == 0) {
		return command, fmt.Errorf("private prompt requires cwd and input")
	}
	if command.Kind == agentproto.CommandTurnSteer {
		a := command.NativeInputAdmission
		if a == nil || a.Version != agentproto.NativeInputAdmissionProtocolVersion || a.ID == "" || len(a.InputIDs) == 0 || command.Target.ThreadID == "" || command.Target.TurnID == "" {
			return command, fmt.Errorf("private steer requires exact admission and turn")
		}
		seen := map[string]bool{}
		for _, id := range a.InputIDs {
			if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) || seen[id] {
				return command, fmt.Errorf("private admission requires unique input IDs")
			}
			seen[id] = true
		}
		ok, err := s.journal.PrivateActiveTurn(command.Target.ThreadID, command.Target.TurnID, *command.NativeBinding)
		if err != nil {
			return command, err
		}
		if !ok {
			return command, fmt.Errorf("private steer requires committed active native turn evidence")
		}
	}
	return command, nil
}

type privateNativeRuntime struct {
	backendRuntime
	state     *privateNativeState
	commandMu sync.Mutex
}

func (r *privateNativeRuntime) TranslateCommand(command agentproto.Command) (runtimeCommandResult, error) {
	r.commandMu.Lock()
	defer r.commandMu.Unlock()
	if _, err := r.state.prepare(command); err != nil {
		return runtimeCommandResult{}, err
	}
	result, err := r.backendRuntime.TranslateCommand(command)
	if err != nil {
		return result, err
	}
	if err = stampNativeUserUUIDs(r.backendRuntime, r.state.snapshot.Owner.OwnerID, r.state.snapshot.Owner.CarrierIncarnation, command, &result); err != nil {
		return result, err
	}
	r.state.annotate(result.Events)
	if err = r.registerFrames(command.CommandID, flattenRuntimeCommandPhaseFrames(result.Phases)); err != nil {
		return result, err
	}
	if command.Kind == agentproto.CommandThreadsRefresh && r.Backend() == agentproto.BackendCodex {
		broker, ok := r.backendRuntime.(interface {
			PrivateThreadListLink(string) (string, string, bool)
		})
		if !ok {
			return result, fmt.Errorf("private query link unavailable")
		}
		source, request, ok := broker.PrivateThreadListLink(command.CommandID)
		if !ok {
			return result, fmt.Errorf("private query lacks exact source")
		}
		if _, err = r.state.journal.RegisterPrivateQueryLink(command.CommandID, source, request); err != nil {
			return result, err
		}
	}
	if err = r.state.inspection.persistPrivate(r.state.journal, r.state.snapshot.Owner); err != nil {
		return result, err
	}
	return result, nil
}
func (r *privateNativeRuntime) ObserveClient([]byte) (runtimeObserveResult, error) {
	return runtimeObserveResult{Suppress: true}, fmt.Errorf("private managed wrapper requires authenticated relay commands")
}
func (r *privateNativeRuntime) ObserveServer(line []byte) (runtimeObserveResult, error) {
	if r.state.inspection.raw(line) {
		r.state.mu.Lock()
		delete(r.state.requests, nativeResponseRequestID(line))
		r.state.mu.Unlock()
		err := r.state.inspection.persistPrivate(r.state.journal, r.state.snapshot.Owner)
		return runtimeObserveResult{Suppress: true}, err
	}
	result, err := r.backendRuntime.ObserveServer(line)
	if err != nil {
		return result, err
	}
	result.Events = append(result.Events, r.state.inspection.drainReceipts()...)
	r.state.annotate(result.Events)
	r.state.inspection.events(result.Events)
	for i := range result.Events {
		event := &result.Events[i]
		if event.Kind != agentproto.EventNativeQueryCompleted {
			continue
		}
		source, ok := event.Metadata["sourceCommandId"].(string)
		if !ok {
			return result, fmt.Errorf("private query source missing")
		}
		link, e := r.state.journal.RegisterPrivateQueryLink(event.CommandID, source, event.RequestID)
		if e != nil {
			return result, e
		}
		event.Metadata["sourceCommandHash"] = link.SourceCommandHash
	}
	request := nativeResponseRequestID(line)
	r.state.mu.Lock()
	command := r.state.requests[request]
	delete(r.state.requests, request)
	r.state.mu.Unlock()
	if err = r.registerFrames(command, result.OutboundToChild); err != nil {
		return result, err
	}
	if err = r.state.inspection.persistPrivate(r.state.journal, r.state.snapshot.Owner); err != nil {
		return result, err
	}
	return result, nil
}
func (s *privateNativeState) annotate(events []agentproto.Event) {
	for i := range events {
		events[i].Execution = nil
		// Claude's synthetic turn ID is derived from the UUID in the sent
		// user frame; only the native-observed UUID proves this association.
		if events[i].Kind == agentproto.EventTurnStarted && events[i].RequestID == "" && events[i].Metadata["turnIdSource"] == "synthetic_from_observed_user_uuid" {
			if uuid, ok := events[i].Metadata["nativeUserUUID"].(string); ok && uuid != "" && events[i].TurnID == "relay-claude-turn-"+uuid {
				events[i].RequestID = uuid
			}
		}
		events[i].NativeBinding = nil
		if events[i].CommandID == "" {
			if events[i].Kind == agentproto.EventItemDelta || events[i].Kind == agentproto.EventRequestStarted || events[i].Kind == agentproto.EventRequestResolved {
				key := events[i].ThreadID + "\x00" + events[i].TurnID
				s.mu.Lock()
				cached, ok := s.liveBindings[key]
				s.mu.Unlock()
				if ok {
					events[i].NativeBinding = &cached
				} else {
					binding, err := s.journal.PrivateBindingForTurn(events[i].ThreadID, events[i].TurnID)
					if err == nil && binding != nil {
						s.mu.Lock()
						if s.liveBindings == nil {
							s.liveBindings = make(map[string]agentproto.NativeTurnBinding)
						}
						s.liveBindings[key] = *binding
						s.mu.Unlock()
						events[i].NativeBinding = binding
					}
				}
			}
			continue
		}
		binding, err := s.journal.PrivateBinding(events[i].CommandID)
		if err == nil && binding != nil {
			events[i].NativeBinding = binding
		}
	}
}
func (r *privateNativeRuntime) registerFrames(commandID string, frames [][]byte) error {
	if commandID == "" {
		if len(frames) > 0 {
			return fmt.Errorf("native follow-up has no exact command correlation")
		}
		return nil
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	for _, frame := range frames {
		if len(frame) == 0 {
			continue
		}
		if _, ok := r.state.frames[&frame[0]]; ok {
			continue
		}
		id, err := r.state.journal.PrepareFrame(commandID, frame)
		if err != nil {
			return err
		}
		r.state.frames[&frame[0]] = workFrameReference{id: id, commandID: commandID}
		r.state.inspection.outbound(commandID, frame)
		if request := nativeOutboundRequestID(frame); request != "" {
			r.state.requests[request] = commandID
		}
	}
	return nil
}
func (r *privateNativeRuntime) BeforeChildWrite(frame []byte) error {
	if len(frame) == 0 {
		return nil
	}
	r.state.mu.Lock()
	ref, ok := r.state.frames[&frame[0]]
	r.state.mu.Unlock()
	if !ok {
		return nil
	}
	err := r.state.journal.StartFrame(ref.id, frame)
	if !errors.Is(err, runtimejournal.ErrPrivateCompactNotIdle) {
		return err
	}
	events, e := r.state.journal.RejectPrivatePreparedFrame(ref.id, err)
	if e != nil {
		return e
	}
	r.state.mu.Lock()
	delete(r.state.frames, &frame[0])
	publish := r.state.publishWritten
	r.state.mu.Unlock()
	if publish != nil {
		_ = publish(events)
	}
	return runtimejournal.ErrPrivateCompactNotIdle
}
func (r *privateNativeRuntime) AfterChildWrite(frame []byte, writeErr error) error {
	if len(frame) == 0 {
		return nil
	}
	r.state.mu.Lock()
	ref, ok := r.state.frames[&frame[0]]
	delete(r.state.frames, &frame[0])
	r.state.mu.Unlock()
	if !ok {
		return nil
	}
	event, err := r.state.journal.FinishPrivateFrame(ref.id, writeErr)
	if err != nil {
		return err
	}
	if event != nil {
		r.state.mu.Lock()
		publish := r.state.publishWritten
		r.state.mu.Unlock()
		if publish != nil {
			_ = publish([]agentproto.Event{*event})
		}
	}
	return nil
}
func (r *privateNativeRuntime) BuildChildRestartRestoreFrame(commandID string) ([]byte, string, bool, error) {
	frame, request, ok, err := r.backendRuntime.BuildChildRestartRestoreFrame(commandID)
	if err == nil && ok {
		err = r.registerFrames(commandID, [][]byte{frame})
	}
	return frame, request, ok, err
}
func (r *privateNativeRuntime) Cleanup() {
	cleanupBackendRuntime(r.backendRuntime)
	r.state.inspection.mu.Lock()
	scopes := append([]*hostinventory.Scope(nil), r.state.inspection.scopes...)
	r.state.inspection.mu.Unlock()
	for _, scope := range scopes {
		scope.Close()
	}
	r.state.journal.Close()
}

// recordPrivatePrewriteRejection is called only before a frame is enqueued to
// the child. Once queued or written, transport outcome remains unknown.
func (a *App) recordPrivatePrewriteRejection(client *relayws.Client, command agentproto.Command, cause error) {
	if a.privateNative == nil || client == nil || cause == nil || command.NativeBinding == nil || command.NativeBinding.Owner != a.privateNative.snapshot.Owner {
		return
	}
	receipt, err := a.privateNative.journal.CommandReceipt(command.CommandID)
	if err != nil || receipt == nil || receipt.Stage != runtimejournal.StagePrepared || receipt.WrittenFrames != 0 {
		return
	}
	for _, f := range receipt.Frames {
		if f.Stage != runtimejournal.StagePrepared {
			return
		}
	}
	problem := agentproto.ErrorInfoFromError(cause, agentproto.ErrorInfo{Code: "command_rejected", Layer: "wrapper", Stage: "before_child_write", Operation: string(command.Kind), Message: "wrapper 无法执行当前命令。", SurfaceSessionID: command.Origin.Surface, CommandID: command.CommandID, ThreadID: command.Target.ThreadID, TurnID: command.Target.TurnID})
	problem.CommandID = command.CommandID
	problem.SurfaceSessionID = command.Origin.Surface
	binding := *command.NativeBinding
	_ = client.SendEvents([]agentproto.Event{{Kind: agentproto.EventNativeCommandRejected, CommandID: command.CommandID, NativeBinding: &binding, Status: "failed", Action: "wrapper.before_child_write", Problem: &problem}})
}
