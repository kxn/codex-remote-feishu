package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type workFrameReference struct {
	id        int64
	commandID string
}

func (a *App) prepareJournalCommand(command agentproto.Command) (bool, error) {
	if a.privateNative != nil {
		_, duplicate, err := a.privateNative.journal.PreparePrivateCommand(command)
		return duplicate, err
	}
	if a.work == nil || a.work.journal == nil {
		return false, nil
	}
	_, duplicate, err := a.work.journal.PrepareCommand(command)
	if err != nil {
		return false, workCommandProblem(command, "work_command_journal_conflict", "执行命令无法持久登记，或同一 command ID 的内容发生变化。")
	}
	return duplicate, nil
}

func (a *App) runtimeCommandReceipt(commandID string) (*agentproto.RuntimeCommandReceipt, error) {
	if a.privateNative != nil {
		return a.privateNative.journal.CommandReceipt(commandID)
	}
	if a.work == nil || a.work.journal == nil {
		return nil, nil
	}
	return a.work.journal.CommandReceipt(commandID)
}

func (a *App) journalProtocolVersion() int {
	if a.privateNative != nil {
		return agentproto.JournalProtocolVersion
	}
	if a.work != nil && a.work.journal != nil {
		return agentproto.JournalProtocolVersion
	}
	return 0
}

func (a *App) carrierIncarnation() string {
	if a.privateNative != nil {
		return a.privateNative.snapshot.Owner.CarrierIncarnation
	}
	if a.journalProtocolVersion() != 0 {
		return a.work.snapshot.CarrierIncarnation
	}
	return ""
}

func (w *workExecutionState) initializeJournal(cfg Config) error {
	if w.snapshot.JournalPath == "" {
		return nil
	}
	journal, err := runtimejournal.Open(context.Background(), w.snapshot.JournalPath, cfg.RuntimePaths.StateDir, w.snapshot.CWD, w.snapshot.CarrierIncarnation)
	if err != nil {
		return err
	}
	if err := journal.BindLaunchSnapshot(w.snapshot); err != nil {
		_ = journal.Close()
		return fmt.Errorf("launch contract changed without a new carrier incarnation")
	}
	w.journal = journal
	w.frames = make(map[*byte]workFrameReference)
	w.nativeRequests = make(map[string]string)
	if err := w.inspection.restore(journal, w.snapshot.Owner); err != nil {
		_ = journal.Close()
		return err
	}
	return nil
}

// Frames are correlated by the actual immutable slice sent through writeCh.
// Equal byte content is not sufficient: Claude can send identical user frames.
func (w *workExecutionState) registerFrames(commandID string, frames [][]byte) error {
	if w.journal == nil || commandID == "" {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, frame := range frames {
		if len(frame) == 0 {
			continue
		}
		if _, exists := w.frames[&frame[0]]; exists {
			continue
		}
		id, err := w.journal.PrepareFrame(commandID, frame)
		if err != nil {
			return err
		}
		w.frames[&frame[0]] = workFrameReference{id: id, commandID: commandID}
		w.inspection.outbound(commandID, frame)
		if requestID := nativeOutboundRequestID(frame); requestID != "" {
			w.nativeRequests[requestID] = commandID
		}
	}
	return nil
}

func nativeFrameRequestID(frame []byte) string {
	var message map[string]any
	if json.Unmarshal(frame, &message) != nil {
		return ""
	}
	if id, exists := message["id"]; exists {
		return fmt.Sprint(id)
	}
	if id, ok := message["request_id"].(string); ok {
		return id
	}
	if response, ok := message["response"].(map[string]any); ok {
		if id, ok := response["request_id"].(string); ok {
			return id
		}
	}
	return ""
}

func nativeOutboundRequestID(frame []byte) string {
	var message map[string]any
	if json.Unmarshal(frame, &message) != nil {
		return ""
	}
	if method, _ := message["method"].(string); method != "" {
		return nativeFrameRequestID(frame)
	}
	if message["type"] == "control_request" {
		return nativeFrameRequestID(frame)
	}
	return ""
}

func nativeResponseRequestID(frame []byte) string {
	var message map[string]any
	if json.Unmarshal(frame, &message) != nil {
		return ""
	}
	if message["type"] == "control_response" {
		return nativeFrameRequestID(frame)
	}
	if _, method := message["method"]; method {
		return ""
	}
	_, result := message["result"]
	_, failure := message["error"]
	if result || failure {
		return nativeFrameRequestID(frame)
	}
	return ""
}

func (r *workExecutionRuntime) BeforeChildWrite(frame []byte) error {
	if r.work.journal == nil || len(frame) == 0 {
		return nil
	}
	r.work.mu.Lock()
	ref, known := r.work.frames[&frame[0]]
	r.work.mu.Unlock()
	if !known {
		return nil
	}
	r.work.mu.Lock()
	m := r.work.maintenance
	var watermark int64
	if m != nil && m.command.CommandID == ref.commandID {
		watermark = m.command.Maintenance.NativeWriteWatermark
	}
	r.work.mu.Unlock()
	if watermark > 0 {
		return r.work.journal.StartMaintenanceFrame(ref.id, frame, watermark, ref.commandID)
	}
	return r.work.journal.StartFrame(ref.id, frame)
}

func (r *workExecutionRuntime) AfterChildWrite(frame []byte, writeErr error) error {
	if r.work.journal == nil || len(frame) == 0 {
		return nil
	}
	r.work.mu.Lock()
	ref, known := r.work.frames[&frame[0]]
	if known {
		delete(r.work.frames, &frame[0])
	}
	r.work.mu.Unlock()
	if !known {
		return nil
	}
	return r.work.journal.FinishFrame(ref.id, writeErr)
}

func (r *workExecutionRuntime) BuildChildRestartRestoreFrame(commandID string) ([]byte, string, bool, error) {
	frame, requestID, ok, err := r.backendRuntime.BuildChildRestartRestoreFrame(commandID)
	if err == nil && ok {
		err = r.work.registerFrames(commandID, [][]byte{frame})
	}
	return frame, requestID, ok, err
}

func writeJournaledChildFrame(childStdin io.Writer, line []byte, runtime backendRuntime) error {
	hooks, tracked := runtime.(interface {
		BeforeChildWrite([]byte) error
		AfterChildWrite([]byte, error) error
	})
	if tracked {
		if err := hooks.BeforeChildWrite(line); err != nil {
			if errors.Is(err, runtimejournal.ErrPrivateCompactNotIdle) {
				return nil
			}
			return err
		}
	}
	n, writeErr := childStdin.Write(line)
	if writeErr == nil && n != len(line) {
		writeErr = io.ErrShortWrite
	}
	if tracked {
		if err := hooks.AfterChildWrite(line, writeErr); err != nil {
			return err
		}
	}
	return writeErr
}
