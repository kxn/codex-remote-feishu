package claudesessionstore

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func localSessionPlaneError(command agentproto.Command, code, message string, err error) agentproto.ErrorInfo {
	return agentproto.ErrorInfo{
		Code:             code,
		Layer:            "wrapper",
		Stage:            "local_session_plane",
		Operation:        string(command.Kind),
		Message:          message,
		Details:          strings.TrimSpace(err.Error()),
		SurfaceSessionID: command.Origin.Surface,
		CommandID:        command.CommandID,
		ThreadID:         command.Target.ThreadID,
		TurnID:           command.Target.TurnID,
	}
}
