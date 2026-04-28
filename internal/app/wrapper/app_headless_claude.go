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

const relayClaudeBootstrapInitializeID = "relay-bootstrap-initialize"

func (a *App) bootstrapClaude(childStdin io.Writer, childStdout io.Reader, rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) (io.Reader, error) {
	frame, err := a.claudeBootstrapInitializeFrame()
	if err != nil {
		return nil, err
	}
	if err := writeChildFrame(childStdin, frame, a.debugf, rawLogger, reportProblem); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(childStdout)
	var replay bytes.Buffer
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			logRawFrame(rawLogger, "codex.stdout", "in", line, "", "")
			a.debugf("claude bootstrap: stdout from child: %s", summarizeFrame(line))
			matched, err := matchClaudeBootstrapInitializeResponse(line)
			if err != nil {
				return nil, err
			}
			if matched {
				return io.MultiReader(bytes.NewReader(replay.Bytes()), reader), nil
			}
			replay.Write(line)
		}
		if readErr == nil {
			continue
		}
		if readErr == io.EOF {
			return nil, fmt.Errorf("claude bootstrap: initialize response %q not received before stdout closed", relayClaudeBootstrapInitializeID)
		}
		return nil, readErr
	}
}

func (a *App) claudeBootstrapInitializeFrame() ([]byte, error) {
	bytes, err := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": relayClaudeBootstrapInitializeID,
		"request": map[string]any{
			"subtype": "initialize",
			"hooks":   map[string]any{},
		},
	})
	if err != nil {
		return nil, err
	}
	return append(bytes, '\n'), nil
}

func matchClaudeBootstrapInitializeResponse(line []byte) (bool, error) {
	var message map[string]any
	if err := json.Unmarshal(line, &message); err != nil {
		return false, nil
	}
	if strings.TrimSpace(lookupStringFromMap(message, "type")) != "control_response" {
		return false, nil
	}
	response, _ := message["response"].(map[string]any)
	if strings.TrimSpace(lookupStringFromMap(response, "request_id")) != relayClaudeBootstrapInitializeID {
		return false, nil
	}
	if subtype := strings.TrimSpace(lookupStringFromMap(response, "subtype")); subtype != "success" {
		return true, fmt.Errorf("claude bootstrap initialize failed: %s", strings.TrimSpace(lookupStringFromMap(response, "error")))
	}
	return true, nil
}
