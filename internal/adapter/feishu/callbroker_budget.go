package feishu

import (
	"context"
	"time"
)

func (b *FeishuCallBroker) waitForBudget(ctx context.Context, spec CallSpec) error {
	for {
		waitFor, reserved := b.reserveNow(spec)
		if reserved {
			return nil
		}
		if err := b.sleep(ctx, waitFor); err != nil {
			return err
		}
	}
}

func (b *FeishuCallBroker) reserveNow(spec CallSpec) (time.Duration, bool) {
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	due := now
	appInterval := appIntervalForSpec(spec)
	due = maxTime(due, b.appBucket.nextAllowedAt, b.appBucket.cooldownUntil)
	classBucket := b.classBucketLocked(spec.Class)
	due = maxTime(due, classBucket.nextAllowedAt, classBucket.cooldownUntil)
	resourceInterval := resourceIntervalForSpec(spec)
	resourceKey := spec.ResourceKey.bucketKey()
	var resourceBucket *callBucketState
	if resourceKey != "" {
		resourceBucket = b.resourceBucketLocked(resourceKey)
		due = maxTime(due, resourceBucket.nextAllowedAt, resourceBucket.cooldownUntil)
	}
	var recipient *callBucketState
	if key := receiveBudgetKey(spec); key != "" && b.sharedReceive != nil {
		b.sharedReceive.mu.Lock()
		defer b.sharedReceive.mu.Unlock()
		recipient = b.sharedReceive.bucket(key, now)
		due = maxTime(due, recipient.nextAllowedAt, recipient.cooldownUntil)
	}
	if due.After(now) {
		return positiveDuration(due.Sub(now)), false
	}
	if recipient != nil {
		recipient.nextAllowedAt = now.Add(250 * time.Millisecond)
	}

	if appInterval > 0 {
		b.appBucket.nextAllowedAt = now.Add(appInterval)
	}
	if classInterval := classIntervalForSpec(spec.Class); classInterval > 0 {
		classBucket.nextAllowedAt = now.Add(classInterval)
	}
	if resourceBucket != nil && resourceInterval > 0 {
		resourceBucket.nextAllowedAt = now.Add(resourceInterval)
	}
	return 0, true
}

func (b *FeishuCallBroker) noteRateLimited(spec CallSpec, rate RateLimitEvidence) {
	cooldown := rateLimitCooldown(rate)
	if cooldown <= 0 {
		cooldown = callBrokerDefaultRateLimitCooldown
	}
	until := b.now().Add(cooldown)
	resourceKey := spec.ResourceKey.bucketKey()
	if key := receiveBudgetKey(spec); key != "" && b.sharedReceive != nil {
		b.sharedReceive.mu.Lock()
		applyBucketCooldown(b.sharedReceive.bucket(key, b.now()), until)
		b.sharedReceive.mu.Unlock()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if resourceKey != "" {
		applyBucketCooldown(b.resourceBucketLocked(resourceKey), until)
		return
	}
	applyBucketCooldown(b.classBucketLocked(spec.Class), until)
}
