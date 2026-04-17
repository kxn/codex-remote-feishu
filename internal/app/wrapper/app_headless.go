package wrapper

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
)

const relayBootstrapInitializeID = "relay-bootstrap-initialize"

func (a *App) bootstrapHeadlessCodex(childStdin io.Writer, childStdout io.Reader, rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) (io.Reader, error) {
	initializeFrame, err := a.syntheticInitializeFrame()
	if err != nil || len(initializeFrame) == 0 {
		return childStdout, err
	}

	a.debugf("headless bootstrap: sending initialize: %s", summarizeFrame(initializeFrame))
	if err := writeCodexFrame(childStdin, initializeFrame, a.debugf, rawLogger, reportProblem); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(childStdout)
	var replay bytes.Buffer
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			logRawFrame(rawLogger, "codex.stdout", "in", line, "", "")
			a.debugf("headless bootstrap: stdout from codex: %s", summarizeFrame(line))
			matched, err := matchBootstrapInitializeResponse(line)
			if err != nil {
				return nil, err
			}
			if matched {
				initializedFrame, err := a.syntheticInitializedFrame()
				if err != nil {
					return nil, err
				}
				a.debugf("headless bootstrap: initialize acknowledged, sending initialized")
				if err := writeCodexFrame(childStdin, initializedFrame, a.debugf, rawLogger, reportProblem); err != nil {
					return nil, err
				}
				return io.MultiReader(bytes.NewReader(replay.Bytes()), reader), nil
			}
			replay.Write(line)
		}

		if readErr == nil {
			continue
		}
		if readErr == io.EOF {
			return nil, fmt.Errorf("headless bootstrap: initialize response %q not received before codex stdout closed", relayBootstrapInitializeID)
		}
		return nil, readErr
	}
}

func (a *App) needsSyntheticBootstrap() bool {
	// Daemon-launched hidden clients must complete initialize/initialized
	// themselves before the first thread/start reaches Codex app-server.
	switch {
	case strings.EqualFold(strings.TrimSpace(a.config.Source), "headless"):
		return true
	case strings.EqualFold(strings.TrimSpace(a.config.Source), "cron"):
		return true
	default:
		return false
	}
}

func (a *App) syntheticInitializeFrame() ([]byte, error) {
	if !a.needsSyntheticBootstrap() {
		return nil, nil
	}
	payload := map[string]any{
		"id":     relayBootstrapInitializeID,
		"method": "initialize",
		"params": map[string]any{
			"clientInfo": map[string]any{
				"name":    "Codex Remote Headless",
				"title":   "Codex Remote Headless",
				"version": firstNonEmpty(a.config.Version, "dev"),
			},
			"capabilities": map[string]any{
				"experimentalApi": true,
				"optOutNotificationMethods": []string{
					"item/agentMessage/delta",
				},
			},
		},
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return append(bytes, '\n'), nil
}

func (a *App) syntheticInitializedFrame() ([]byte, error) {
	if !a.needsSyntheticBootstrap() {
		return nil, nil
	}
	bytes, err := json.Marshal(map[string]any{
		"method": "initialized",
		"params": map[string]any{},
	})
	if err != nil {
		return nil, err
	}
	return append(bytes, '\n'), nil
}

func matchBootstrapInitializeResponse(line []byte) (bool, error) {
	var message map[string]any
	if err := json.Unmarshal(line, &message); err != nil {
		return false, nil
	}
	if lookupStringFromMap(message, "id") != relayBootstrapInitializeID {
		return false, nil
	}
	if errMsg := strings.TrimSpace(extractJSONRPCErrorMessage(message)); errMsg != "" {
		return true, fmt.Errorf("headless bootstrap initialize failed: %s", errMsg)
	}
	if _, ok := message["result"]; !ok {
		return true, fmt.Errorf("headless bootstrap initialize response missing result")
	}
	return true, nil
}
