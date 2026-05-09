package wrapper

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func flattenRuntimeCommandPhaseFrames(phases []runtimeCommandPhase) [][]byte {
	if len(phases) == 0 {
		return nil
	}
	frames := make([][]byte, 0, len(phases))
	for _, phase := range phases {
		frames = append(frames, phase.OutboundToChild...)
	}
	return frames
}

func executeCommandPhases(
	ctx context.Context,
	writeCh chan<- []byte,
	commandResponses *commandResponseTracker,
	commandID string,
	phases []runtimeCommandPhase,
	debugf func(string, ...any),
) error {
	for index, phase := range phases {
		var waitCh <-chan *agentproto.ErrorInfo
		if gate := phase.ResponseGate; gate != nil {
			waitCh = commandResponses.Register(
				gate.RequestID,
				gate.RejectProblem,
				gate.SuppressFrame,
			)
		}
		for _, line := range phase.OutboundToChild {
			select {
			case writeCh <- line:
				if debugf != nil {
					debugf(
						"relay command queued for child: command=%s phase=%d frame=%s",
						commandID,
						index+1,
						summarizeFrame(line),
					)
				}
			case <-ctx.Done():
				if phase.ResponseGate != nil {
					commandResponses.Cancel(phase.ResponseGate.RequestID)
				}
				return ctx.Err()
			}
		}
		if phase.ResponseGate == nil {
			continue
		}
		err := waitCommandResponse(ctx, waitCh, phase.ResponseGate.Timeout, phase.ResponseGate.TimeoutProblem)
		if err != nil {
			commandResponses.Cancel(phase.ResponseGate.RequestID)
			if debugf != nil {
				debugf(
					"relay command response failed: command=%s phase=%d request=%s err=%v",
					commandID,
					index+1,
					phase.ResponseGate.RequestID,
					err,
				)
			}
			return err
		}
		if debugf != nil {
			debugf(
				"relay command response accepted: command=%s phase=%d request=%s",
				commandID,
				index+1,
				phase.ResponseGate.RequestID,
			)
		}
	}
	return nil
}
