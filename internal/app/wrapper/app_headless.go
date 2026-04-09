package wrapper

import (
	"io"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
)

func (a *App) bootstrapAgent(childStdin io.Writer, rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) error {
	frames, err := a.translator.BootstrapFrames(a.config.Source, a.config.Version)
	if err != nil || len(frames) == 0 {
		return err
	}
	a.debugf("agent bootstrap: frames=%s", summarizeFrames(frames))
	for _, frame := range frames {
		if err := writeAgentFrame(childStdin, frame, a.debugf, rawLogger, reportProblem); err != nil {
			return err
		}
	}
	return nil
}
