package feishu

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

func CompleteAppAutoConfig(ctx context.Context, cfg LiveGatewayConfig, manifest feishuapp.Manifest, policy feishuapp.FixedPolicy, req AutoConfigPublishRequest) (AutoConfigCompleteResult, error) {
	return NewSetupClient(SetupClientConfigFromLiveGatewayConfig(cfg)).CompleteAppAutoConfig(ctx, manifest, policy, req)
}

func (c *SetupClient) CompleteAppAutoConfig(ctx context.Context, manifest feishuapp.Manifest, policy feishuapp.FixedPolicy, req AutoConfigPublishRequest) (AutoConfigCompleteResult, error) {
	return newAutoConfigService(c, manifest, policy).Complete(ctx, req)
}

func (s *autoConfigService) Complete(ctx context.Context, req AutoConfigPublishRequest) (AutoConfigCompleteResult, error) {
	plan, err := s.Plan(ctx)
	if err != nil {
		return AutoConfigCompleteResult{}, err
	}
	result := AutoConfigCompleteResult{
		Status:         plan.Status,
		Summary:        plan.Summary,
		BlockingReason: plan.BlockingReason,
		Plan:           plan,
	}
	if autoConfigCompleteTerminal(plan) {
		result.Actions = append(result.Actions, AutoConfigAction{Name: "complete", Outcome: "skipped", Details: "no automatic changes can be applied"})
		return result, nil
	}
	if plan.Diff.ConfigPatchRequired || plan.Diff.AbilityPatchRequired {
		applyResult, err := s.Apply(ctx)
		if err != nil {
			return AutoConfigCompleteResult{}, err
		}
		result.Actions = append(result.Actions, applyResult.Actions...)
		result.Status = applyResult.Status
		result.Summary = applyResult.Summary
		result.BlockingReason = applyResult.BlockingReason
		result.Plan = applyResult.Plan
		result.VerificationStatus = applyResult.VerificationStatus
		result.VerificationError = applyResult.VerificationError
		plan = applyResult.Plan
		if autoConfigCompleteTerminalStatus(result.Status) {
			return result, nil
		}
	}
	if plan.Publish.NeedsPublish {
		publishResult, err := s.Publish(ctx, req)
		if err != nil {
			return AutoConfigCompleteResult{}, err
		}
		result.Actions = append(result.Actions, publishResult.Actions...)
		result.Status = publishResult.Status
		result.Summary = publishResult.Summary
		result.BlockingReason = publishResult.BlockingReason
		result.VersionID = publishResult.VersionID
		result.Version = publishResult.Version
		result.Plan = publishResult.Plan
		result.VerificationStatus = publishResult.VerificationStatus
		result.VerificationError = publishResult.VerificationError
		return result, nil
	}
	return result, nil
}

func autoConfigCompleteTerminal(plan AutoConfigPlan) bool {
	return autoConfigCompleteTerminalStatus(plan.Status)
}

func autoConfigCompleteTerminalStatus(status string) bool {
	switch status {
	case AutoConfigStatusAwaitingReview, AutoConfigStatusBlocked, AutoConfigStatusUnsupported, AutoConfigStatusVerificationFailed:
		return true
	default:
		return false
	}
}
