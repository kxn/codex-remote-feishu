package wrapper

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync/atomic"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
)

func stdinLoop(ctx context.Context, stdin io.Reader, writeCh chan<- []byte, runtime backendRuntime, client *relayws.Client, errCh chan<- error, debugf func(string, ...any), rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) {
	reader := bufio.NewReader(stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			forwardOriginal := true
			logRawFrame(rawLogger, "parent.stdin", "in", line, "", "")
			if debugf != nil {
				debugf("stdin from parent: %s", summarizeFrame(line))
			}
			if result, parseErr := runtime.ObserveClient(line); parseErr == nil {
				if debugf != nil && (len(result.Events) > 0 || len(result.OutboundToChild) > 0 || result.Suppress) {
					debugf("stdin observe result: events=%s followups=%d suppress=%t", summarizeEventKinds(result.Events), len(result.OutboundToChild), result.Suppress)
				}
				if sendErr := client.SendEvents(result.Events); sendErr != nil {
					log.Printf("relay send client events failed: %v", sendErr)
					if reportProblem != nil {
						reportProblem(agentproto.ErrorInfoFromError(sendErr, agentproto.ErrorInfo{
							Code:      "relay_send_client_events_failed",
							Layer:     "wrapper",
							Stage:     "forward_client_events",
							Operation: "parent.stdin",
							Message:   "wrapper 无法把本地客户端事件发送到 relay。",
							Retryable: true,
						}))
					}
				}
				forwardOriginal = !result.Suppress
			} else {
				if debugf != nil {
					debugf("stdin observe parse failed: err=%v preview=%q", parseErr, previewRawLine(line))
				}
				if reportProblem != nil {
					reportProblem(agentproto.ErrorInfo{
						Code:      "stdin_parse_failed",
						Layer:     "wrapper",
						Stage:     "observe_parent_stdin",
						Operation: "parent.stdin",
						Message:   "wrapper 无法解析上游传来的 JSON-RPC 帧。",
						Details:   fmt.Sprintf("%v; frame=%q", parseErr, previewRawLine(line)),
					})
				}
			}
			if forwardOriginal {
				select {
				case writeCh <- line:
					if debugf != nil {
						debugf("stdin forwarded to child: %s", summarizeFrame(line))
					}
				case <-ctx.Done():
					return
				}
			} else if debugf != nil {
				debugf("stdin suppressed before codex: %s", summarizeFrame(line))
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			return
		}
		errCh <- err
		return
	}
}

func stdoutLoop(ctx context.Context, childStdout io.Reader, parentStdout io.Writer, writeCh chan<- []byte, runtime backendRuntime, client *relayws.Client, commandResponses *commandResponseTracker, turnTracker *runtimeTurnTracker, activeGeneration *int64, generation int64, errCh chan<- error, debugf func(string, ...any), rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo), done chan<- struct{}) {
	defer close(done)
	reader := bufio.NewReader(childStdout)
	coalescer := newRelayEventCoalescer(nil, 0, 0)
	sendRelayEvents := func(events []agentproto.Event) {
		if len(events) == 0 {
			return
		}
		if sendErr := client.SendEvents(events); sendErr != nil {
			log.Printf("relay send server events failed: %v", sendErr)
			if reportProblem != nil {
				reportProblem(agentproto.ErrorInfoFromError(sendErr, agentproto.ErrorInfo{
					Code:      "relay_send_server_events_failed",
					Layer:     "wrapper",
					Stage:     "forward_server_events",
					Operation: "codex.stdout",
					Message:   "wrapper 无法把 Codex 事件发送到 relay。",
					Retryable: true,
				}))
			}
		}
	}
	defer sendRelayEvents(coalescer.Flush())
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			logRawFrame(rawLogger, "codex.stdout", "in", line, "", "")
			if debugf != nil {
				debugf("stdout from child: %s", summarizeFrame(line))
			}
			if activeGeneration != nil {
				currentGeneration := atomic.LoadInt64(activeGeneration)
				if currentGeneration != generation {
					if debugf != nil {
						debugf("stdout from stale child ignored: generation=%d active=%d frame=%s", generation, currentGeneration, summarizeFrame(line))
					}
					continue
				}
			}
			_, suppressCommandResponse := commandResponses.Resolve(line)
			result, parseErr := runtime.ObserveServer(line)
			if parseErr == nil {
				if debugf != nil {
					debugf(
						"stdout observe result: events=%s followups=%d frames=%s suppress=%t",
						summarizeEventKinds(result.Events),
						len(result.OutboundToChild),
						summarizeFrames(result.OutboundToChild),
						result.Suppress,
					)
				}
				turnTracker.ObserveEvents(result.Events)
				sendRelayEvents(coalescer.Push(result.Events))
				for _, followup := range result.OutboundToChild {
					select {
					case writeCh <- followup:
						if debugf != nil {
							debugf("stdout queued followup to child: %s", summarizeFrame(followup))
						}
					case <-ctx.Done():
						return
					}
				}
				if !result.Suppress && !suppressCommandResponse {
					if _, writeErr := parentStdout.Write(line); writeErr != nil {
						if reportProblem != nil {
							reportProblem(agentproto.ErrorInfoFromError(writeErr, agentproto.ErrorInfo{
								Code:      "write_parent_stdout_failed",
								Layer:     "wrapper",
								Stage:     "write_parent_stdout",
								Operation: "parent.stdout",
								Message:   "wrapper 无法把 Codex 输出回传给上游客户端。",
							}))
						}
						errCh <- writeErr
						return
					}
				}
				for _, parentFrame := range result.OutboundToParent {
					if len(parentFrame) == 0 {
						continue
					}
					if _, writeErr := parentStdout.Write(parentFrame); writeErr != nil {
						if reportProblem != nil {
							reportProblem(agentproto.ErrorInfoFromError(writeErr, agentproto.ErrorInfo{
								Code:      "write_parent_stdout_failed",
								Layer:     "wrapper",
								Stage:     "write_parent_stdout",
								Operation: "parent.stdout",
								Message:   "wrapper 无法把合并后的 Codex 输出回传给上游客户端。",
							}))
						}
						errCh <- writeErr
						return
					}
				}
			} else {
				if debugf != nil {
					debugf("stdout observe parse failed: err=%v preview=%q", parseErr, previewRawLine(line))
				}
				if reportProblem != nil {
					reportProblem(agentproto.ErrorInfo{
						Code:      "stdout_parse_failed",
						Layer:     "wrapper",
						Stage:     "observe_codex_stdout",
						Operation: "codex.stdout",
						Message:   "wrapper 无法解析 Codex 子进程输出的 JSON-RPC 帧。",
						Details:   fmt.Sprintf("%v; frame=%q", parseErr, previewRawLine(line)),
					})
				}
				if suppressCommandResponse {
					continue
				}
				if _, writeErr := parentStdout.Write(line); writeErr != nil {
					if reportProblem != nil {
						reportProblem(agentproto.ErrorInfoFromError(writeErr, agentproto.ErrorInfo{
							Code:      "write_parent_stdout_failed",
							Layer:     "wrapper",
							Stage:     "write_parent_stdout",
							Operation: "parent.stdout",
							Message:   "wrapper 无法把 Codex 输出回传给上游客户端。",
						}))
					}
					errCh <- writeErr
					return
				}
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			return
		}
		if ctx.Err() != nil || strings.Contains(err.Error(), "file already closed") {
			return
		}
		errCh <- err
		return
	}
}

func writeLoop(ctx context.Context, childStdin io.WriteCloser, writeCh <-chan []byte, errCh chan<- error, debugf func(string, ...any), rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo), done chan<- struct{}) {
	defer childStdin.Close()
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case line := <-writeCh:
			if len(line) == 0 {
				continue
			}
			if err := writeChildFrame(childStdin, line, debugf, rawLogger, reportProblem); err != nil {
				errCh <- err
				return
			}
		}
	}
}

func writeChildFrame(childStdin io.Writer, line []byte, debugf func(string, ...any), rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) error {
	if len(line) == 0 {
		return nil
	}
	if debugf != nil {
		debugf("write to child: %s", summarizeFrame(line))
	}
	logRawFrame(rawLogger, "codex.stdin", "out", line, "", "")
	if _, err := childStdin.Write(line); err != nil {
		if reportProblem != nil {
			reportProblem(agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
				Code:      "write_codex_stdin_failed",
				Layer:     "wrapper",
				Stage:     "write_codex_stdin",
				Operation: "codex.stdin",
				Message:   "wrapper 无法继续向 Codex 子进程写入数据。",
			}))
		}
		return err
	}
	return nil
}

func logRawFrame(rawLogger *debuglog.RawLogger, channel, direction string, payload []byte, envelopeType, commandID string) {
	if rawLogger == nil {
		return
	}
	rawLogger.Log(debuglog.RawEntry{
		Channel:      channel,
		Direction:    direction,
		EnvelopeType: envelopeType,
		CommandID:    commandID,
		Frame:        payload,
	})
}

func streamCopy(src io.Reader, dst io.Writer, errCh chan<- error) {
	if _, err := io.Copy(dst, src); err != nil && !strings.Contains(err.Error(), "file already closed") {
		errCh <- err
	}
}
