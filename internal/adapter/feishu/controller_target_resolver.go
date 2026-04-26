package feishu

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

type gatewayTargetRequirement string

const (
	gatewayTargetRequireRuntime   gatewayTargetRequirement = "runtime"
	gatewayTargetRequirePreviewer gatewayTargetRequirement = "previewer"
)

type gatewayTargetOutcome string

const (
	gatewayTargetResolved          gatewayTargetOutcome = "resolved"
	gatewayTargetNoop              gatewayTargetOutcome = "noop"
	gatewayTargetMissingGatewayID  gatewayTargetOutcome = "missing_gateway_id"
	gatewayTargetGatewayNotRunning gatewayTargetOutcome = "gateway_not_running"
)

type gatewayTargetResolution struct {
	Target    eventcontract.TargetRef
	GatewayID string
	Worker    *gatewayWorker
	Outcome   gatewayTargetOutcome
}

func (r gatewayTargetResolution) ok() bool {
	return r.Outcome == gatewayTargetResolved && r.Worker != nil
}

func (r gatewayTargetResolution) errorf(prefix string) error {
	switch r.Outcome {
	case gatewayTargetMissingGatewayID:
		return fmt.Errorf("%s: missing gateway id", prefix)
	case gatewayTargetGatewayNotRunning:
		return fmt.Errorf("%s: gateway %s is not running", prefix, r.GatewayID)
	default:
		return nil
	}
}

func (r gatewayTargetResolution) fileSendError() error {
	return &IMFileSendError{
		Code: IMFileSendErrorGatewayNotRunning,
		Err:  r.errorf("send file failed"),
	}
}

func (r gatewayTargetResolution) imageSendError() error {
	return &IMImageSendError{
		Code: IMImageSendErrorGatewayNotRunning,
		Err:  r.errorf("send image failed"),
	}
}

func (r gatewayTargetResolution) videoSendError() error {
	return &IMVideoSendError{
		Code: IMVideoSendErrorGatewayNotRunning,
		Err:  r.errorf("send video failed"),
	}
}

func (r gatewayTargetResolution) driveFileCommentReadError() error {
	return &DriveFileCommentReadError{
		Code: DriveFileCommentReadErrorGatewayNotRunning,
		Err:  r.errorf("read drive comments failed"),
	}
}

func (c *MultiGatewayController) resolveGatewayTarget(target eventcontract.TargetRef, requirement gatewayTargetRequirement) gatewayTargetResolution {
	target = target.Normalized()
	gatewayID := target.GatewayID
	if gatewayID == "" {
		switch target.SelectionPolicy {
		case eventcontract.GatewaySelectionAllowSoleGatewayFallback:
			gatewayID = c.soleGatewayID()
		case eventcontract.GatewaySelectionAllowSurfaceDerived:
			gatewayID = normalizeGatewayID(gatewayIDFromSurface(target.SurfaceSessionID))
		}
	}

	result := gatewayTargetResolution{
		Target:    target,
		GatewayID: gatewayID,
	}
	if gatewayID == "" {
		result.Outcome = gatewayTargetOutcomeForFailurePolicy(target.FailurePolicy, gatewayTargetMissingGatewayID)
		return result
	}

	c.mu.RLock()
	worker := c.workers[gatewayID]
	c.mu.RUnlock()
	if !gatewayTargetRequirementSatisfied(worker, requirement) {
		result.Outcome = gatewayTargetOutcomeForFailurePolicy(target.FailurePolicy, gatewayTargetGatewayNotRunning)
		return result
	}

	result.Worker = worker
	result.Outcome = gatewayTargetResolved
	return result
}

func (c *MultiGatewayController) soleGatewayID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.workers) != 1 {
		return ""
	}
	for gatewayID := range c.workers {
		return gatewayID
	}
	return ""
}

func gatewayTargetRequirementSatisfied(worker *gatewayWorker, requirement gatewayTargetRequirement) bool {
	if worker == nil {
		return false
	}
	switch requirement {
	case gatewayTargetRequirePreviewer:
		return worker.previewer != nil
	default:
		return worker.runtime != nil
	}
}

func gatewayTargetOutcomeForFailurePolicy(policy eventcontract.GatewayFailurePolicy, failure gatewayTargetOutcome) gatewayTargetOutcome {
	if policy == eventcontract.GatewayFailureNoop {
		return gatewayTargetNoop
	}
	return failure
}
