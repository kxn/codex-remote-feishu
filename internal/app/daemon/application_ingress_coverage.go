package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

// previousIngressVerifier checks the old process/hook's source-specific durable
// stop and drain evidence. Startup proof alone cannot establish that fact.
type previousIngressVerifier interface {
	VerifyPreviousIngressStopped(context.Context, ingressowner.GatewayCoverage, json.RawMessage) error
}
type startupCoverageEvidence struct {
	Startup  feishu.StartupIngressProof `json:"startup"`
	Previous json.RawMessage            `json:"previous"`
}
type applicationCoverageVerifier struct {
	controller *feishu.MultiGatewayController
	previous   previousIngressVerifier
}

func (v applicationCoverageVerifier) VerifyGatewayCoverage(ctx context.Context, coverage ingressowner.GatewayCoverage, proof []byte) error {
	if v.controller == nil || v.previous == nil {
		return fmt.Errorf("gateway coverage authority unavailable")
	}
	var evidence startupCoverageEvidence
	if json.Unmarshal(proof, &evidence) != nil || !json.Valid(evidence.Previous) || string(evidence.Previous) == "null" {
		return ingressowner.ErrInvalid
	}
	if evidence.Startup.GatewayID != coverage.GatewayID || evidence.Startup.AppID != coverage.AppID || evidence.Startup.RouterID != coverage.RouterID {
		return ingressowner.ErrConflict
	}
	if err := v.controller.VerifyStartupIngressProof(evidence.Startup); err != nil {
		return err
	}
	return v.previous.VerifyPreviousIngressStopped(ctx, coverage, evidence.Previous)
}

// prepareStartupHook returns an installable hook for a fresh gateway. Existing
// running gateway replacement is deliberately rejected by the adapter.
func (g *applicationIngress) prepareStartupHook() (feishu.RawIngressHook, error) {
	if g == nil || g.router == nil {
		return feishu.RawIngressHook{}, ingressowner.ErrInvalid
	}
	hook := *g.hook()
	if g.host != nil {
		hook = g.host.guardApplicationIngress(hook)
	}
	return feishu.PrepareStartupIngressHook(g.router.RouterID(), hook)
}

type daemonLockPreviousVerifier struct{ lock *relayruntime.FileLock }

func (v daemonLockPreviousVerifier) VerifyPreviousIngressStopped(_ context.Context, coverage ingressowner.GatewayCoverage, raw json.RawMessage) error {
	var proof struct {
		Kind      string `json:"kind"`
		RouterID  string `json:"router_id"`
		CutoverID string `json:"cutover_id"`
	}
	if json.Unmarshal(raw, &proof) != nil || proof.Kind != "exclusive-daemon-lock" || proof.RouterID != coverage.RouterID || proof.CutoverID != coverage.CutoverID {
		return ingressowner.ErrConflict
	}
	if v.lock == nil {
		return ingressowner.ErrInvalid
	}
	return v.lock.VerifyHeld()
}
