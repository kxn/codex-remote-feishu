package feishu

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
	lark "github.com/larksuite/oapi-sdk-go/v3"
)

const (
	callBrokerAppInterval              = 25 * time.Millisecond
	callBrokerClassIMSendInterval      = 50 * time.Millisecond
	callBrokerClassIMPatchInterval     = 80 * time.Millisecond
	callBrokerClassIMReadInterval      = 60 * time.Millisecond
	callBrokerDefaultRateLimitCooldown = 2 * time.Second
	callBrokerPermissionBlockTTL       = 3 * time.Minute
	callBrokerPermissionWaitPoll       = 5 * time.Second
	callBrokerMessageResourceInterval  = 250 * time.Millisecond
	callBrokerReadResourceInterval     = 100 * time.Millisecond
	callBrokerReactionResourceInterval = 120 * time.Millisecond
)

type CallClass string

const (
	CallClassIMSend   CallClass = "im_send"
	CallClassIMPatch  CallClass = "im_patch"
	CallClassIMRead   CallClass = "im_read"
	CallClassDrive    CallClass = "drive"
	CallClassBitable  CallClass = "bitable"
	CallClassMetaHTTP CallClass = "meta_http"
)

type CallPriority string

const (
	CallPriorityInteractive CallPriority = "interactive"
	CallPriorityReadAssist  CallPriority = "read_assist"
	CallPriorityBackground  CallPriority = "background"
)

type RetryPolicy string

const (
	RetryOff           RetryPolicy = "off"
	RetryRateLimitOnly RetryPolicy = "rate_limit_only"
	RetrySafe          RetryPolicy = "safe"
)

type PermissionPolicy string

const (
	PermissionFailFast     PermissionPolicy = "fail_fast"
	PermissionCooldownOnly PermissionPolicy = "cooldown_only"
	PermissionWaitForGrant PermissionPolicy = "wait_for_grant"
)

type FeishuResourceKey struct {
	ReceiveTarget string
	MessageID     string
	CardID        string
	DocToken      string
	TableID       string
	FileKey       string
}

func (k FeishuResourceKey) bucketKey() string {
	switch {
	case strings.TrimSpace(k.MessageID) != "" && strings.TrimSpace(k.FileKey) != "":
		return "message_resource:" + strings.TrimSpace(k.MessageID) + ":" + strings.TrimSpace(k.FileKey)
	case strings.TrimSpace(k.MessageID) != "":
		return "message:" + strings.TrimSpace(k.MessageID)
	case strings.TrimSpace(k.ReceiveTarget) != "":
		return "receive:" + strings.TrimSpace(k.ReceiveTarget)
	case strings.TrimSpace(k.CardID) != "":
		return "card:" + strings.TrimSpace(k.CardID)
	case strings.TrimSpace(k.DocToken) != "":
		return "doc:" + strings.TrimSpace(k.DocToken)
	case strings.TrimSpace(k.TableID) != "":
		return "table:" + strings.TrimSpace(k.TableID)
	case strings.TrimSpace(k.FileKey) != "":
		return "file:" + strings.TrimSpace(k.FileKey)
	default:
		return ""
	}
}

type CallSpec struct {
	GatewayID   string
	API         string
	Class       CallClass
	Priority    CallPriority
	ResourceKey FeishuResourceKey
	Retry       RetryPolicy
	Permission  PermissionPolicy
}

type PermissionBlockedError struct {
	GatewayID    string
	API          string
	BlockedUntil time.Time
	gap          PermissionGapEvidence
}

func (e *PermissionBlockedError) Error() string {
	if e == nil {
		return ""
	}
	api := strings.TrimSpace(e.API)
	if api == "" {
		api = "unknown"
	}
	scope := strings.TrimSpace(e.gap.Scope)
	if scope == "" {
		scope = "unknown"
	}
	msg := fmt.Sprintf("feishu api %s blocked by known missing permission %s", api, scope)
	if !e.BlockedUntil.IsZero() {
		msg += " until " + e.BlockedUntil.UTC().Format(time.RFC3339)
	}
	return msg
}

type callBucketState struct {
	nextAllowedAt time.Time
	cooldownUntil time.Time
}

type permissionBlockState struct {
	gap          PermissionGapEvidence
	blockedUntil time.Time
}

type FeishuCallBroker struct {
	gatewayID string
	sdkClient *lark.Client
	now       func() time.Time
	sleep     func(context.Context, time.Duration) error

	mu               sync.Mutex
	appBucket        callBucketState
	classBuckets     map[CallClass]*callBucketState
	resourceBuckets  map[string]*callBucketState
	permissionBlocks map[string]*permissionBlockState
	apiPermissions   map[string]string
}

func NewFeishuCallBroker(gatewayID string, sdkClient *lark.Client) *FeishuCallBroker {
	return &FeishuCallBroker{
		gatewayID:        gatewaypkg.NormalizeGatewayID(gatewayID),
		sdkClient:        sdkClient,
		now:              func() time.Time { return time.Now().UTC() },
		sleep:            sleepWithContext,
		classBuckets:     map[CallClass]*callBucketState{},
		resourceBuckets:  map[string]*callBucketState{},
		permissionBlocks: map[string]*permissionBlockState{},
		apiPermissions:   map[string]string{},
	}
}

func DoSDK[T any](ctx context.Context, broker *FeishuCallBroker, spec CallSpec, fn func(context.Context, *lark.Client) (T, error)) (T, error) {
	var zero T
	if fn == nil {
		return zero, fmt.Errorf("feishu sdk call failed: nil function")
	}
	if broker == nil || broker.sdkClient == nil {
		return zero, fmt.Errorf("feishu sdk call failed: broker client not configured")
	}
	result, err := broker.do(ctx, spec, func(callCtx context.Context) (any, error) {
		return fn(callCtx, broker.sdkClient)
	})
	if result == nil {
		return zero, err
	}
	typed, ok := result.(T)
	if !ok {
		return zero, fmt.Errorf("feishu sdk call failed: unexpected result type %T", result)
	}
	return typed, err
}

func (b *FeishuCallBroker) do(ctx context.Context, spec CallSpec, fn func(context.Context) (any, error)) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	spec = b.normalizeSpec(spec)
	attempt := 0
	for {
		if err := b.waitForPermissionAllowance(ctx, spec); err != nil {
			return nil, err
		}
		if err := b.waitForBudget(ctx, spec); err != nil {
			return nil, err
		}
		result, err := fn(ctx)
		if err == nil {
			return result, nil
		}
		if gap, ok := ExtractPermissionGap(err); ok {
			b.markPermissionBlocked(spec, gap)
			return result, err
		}
		if rate, ok := ExtractRateLimit(err); ok {
			b.noteRateLimited(spec, rate)
			if spec.Retry.allowsRateLimitRetry(attempt) {
				attempt++
				continue
			}
		}
		return result, err
	}
}

func (b *FeishuCallBroker) normalizeSpec(spec CallSpec) CallSpec {
	spec.GatewayID = gatewaypkg.NormalizeGatewayID(xutil.FirstNonEmpty(spec.GatewayID, b.gatewayID))
	spec.API = strings.TrimSpace(spec.API)
	if spec.Retry == "" {
		spec.Retry = RetryOff
	}
	if spec.Permission == "" {
		spec.Permission = PermissionFailFast
	}
	return spec
}

func (b *FeishuCallBroker) waitForPermissionAllowance(ctx context.Context, spec CallSpec) error {
	for {
		blockedErr, waitFor := b.currentPermissionBlock(spec)
		if blockedErr == nil {
			return nil
		}
		if spec.Permission != PermissionWaitForGrant || waitFor <= 0 {
			return blockedErr
		}
		if waitFor > callBrokerPermissionWaitPoll {
			waitFor = callBrokerPermissionWaitPoll
		}
		if err := b.sleep(ctx, waitFor); err != nil {
			return err
		}
	}
}

func (b *FeishuCallBroker) currentPermissionBlock(spec CallSpec) (*PermissionBlockedError, time.Duration) {
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	api := strings.TrimSpace(spec.API)
	if api == "" {
		return nil, 0
	}
	permissionKey := strings.TrimSpace(b.apiPermissions[api])
	if permissionKey == "" {
		return nil, 0
	}
	block := b.permissionBlocks[permissionKey]
	if block == nil {
		delete(b.apiPermissions, api)
		return nil, 0
	}
	if !block.blockedUntil.IsZero() && !now.Before(block.blockedUntil) {
		b.dropPermissionKeyLocked(permissionKey)
		return nil, 0
	}
	return &PermissionBlockedError{
		GatewayID:    spec.GatewayID,
		API:          api,
		BlockedUntil: block.blockedUntil,
		gap:          block.gap,
	}, positiveDuration(block.blockedUntil.Sub(now))
}

func (b *FeishuCallBroker) markPermissionBlocked(spec CallSpec, gap PermissionGapEvidence) {
	key := permissionBlockKey(gap.Scope, gap.ScopeType)
	if key == "" {
		return
	}
	now := b.now()
	api := strings.TrimSpace(spec.API)
	b.mu.Lock()
	defer b.mu.Unlock()
	block := b.permissionBlocks[key]
	if block == nil {
		block = &permissionBlockState{}
		b.permissionBlocks[key] = block
	}
	block.gap = gap
	block.blockedUntil = now.Add(callBrokerPermissionBlockTTL)
	if api != "" {
		b.apiPermissions[api] = key
	}
}

func (b *FeishuCallBroker) ClearGrantedPermissionBlocks(scopes []AppScopeStatus) {
	granted := map[string]bool{}
	for _, item := range scopes {
		if !scopeGranted(item) {
			continue
		}
		key := permissionBlockKey(item.ScopeName, item.ScopeType)
		if key != "" {
			granted[key] = true
		}
		if fallback := permissionBlockKey(item.ScopeName, ""); fallback != "" {
			granted[fallback] = true
		}
	}
	if len(granted) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for key := range granted {
		b.dropPermissionKeyLocked(key)
	}
}

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
	if due.After(now) {
		return positiveDuration(due.Sub(now)), false
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
	b.mu.Lock()
	defer b.mu.Unlock()
	if resourceKey != "" {
		applyBucketCooldown(b.resourceBucketLocked(resourceKey), until)
		return
	}
	applyBucketCooldown(b.classBucketLocked(spec.Class), until)
}

func (b *FeishuCallBroker) classBucketLocked(class CallClass) *callBucketState {
	bucket := b.classBuckets[class]
	if bucket == nil {
		bucket = &callBucketState{}
		b.classBuckets[class] = bucket
	}
	return bucket
}

func (b *FeishuCallBroker) resourceBucketLocked(key string) *callBucketState {
	bucket := b.resourceBuckets[key]
	if bucket == nil {
		bucket = &callBucketState{}
		b.resourceBuckets[key] = bucket
	}
	return bucket
}

func (b *FeishuCallBroker) dropPermissionKeyLocked(permissionKey string) {
	delete(b.permissionBlocks, permissionKey)
	for api, key := range b.apiPermissions {
		if key == permissionKey {
			delete(b.apiPermissions, api)
		}
	}
}

func appIntervalForSpec(_ CallSpec) time.Duration {
	return callBrokerAppInterval
}

func classIntervalForSpec(class CallClass) time.Duration {
	switch class {
	case CallClassIMSend:
		return callBrokerClassIMSendInterval
	case CallClassIMPatch:
		return callBrokerClassIMPatchInterval
	case CallClassIMRead:
		return callBrokerClassIMReadInterval
	default:
		return 0
	}
}

func resourceIntervalForSpec(spec CallSpec) time.Duration {
	api := strings.TrimSpace(spec.API)
	switch api {
	case "im.v1.message.create", "im.v1.message.reply", "im.v1.message.patch":
		if spec.ResourceKey.bucketKey() != "" {
			return callBrokerMessageResourceInterval
		}
	case "im.v1.message_reaction.create", "im.v1.message_reaction.delete":
		if strings.TrimSpace(spec.ResourceKey.MessageID) != "" {
			return callBrokerReactionResourceInterval
		}
	case "im.v1.message.get", "im.v1.message_resource.get":
		if spec.ResourceKey.bucketKey() != "" {
			return callBrokerReadResourceInterval
		}
	}
	return 0
}

func rateLimitCooldown(rate RateLimitEvidence) time.Duration {
	if rate.RateLimitResetAfter > 0 {
		return rate.RateLimitResetAfter
	}
	if rate.RetryAfter > 0 {
		return rate.RetryAfter
	}
	return 0
}

func (p RetryPolicy) allowsRateLimitRetry(attempt int) bool {
	switch p {
	case RetryRateLimitOnly, RetrySafe:
		return attempt < 2
	default:
		return false
	}
}

func permissionBlockKey(scope, scopeType string) string {
	scope = strings.TrimSpace(scope)
	scopeType = strings.TrimSpace(scopeType)
	if scope == "" {
		return ""
	}
	return scope + "|" + scopeType
}

func applyBucketCooldown(bucket *callBucketState, until time.Time) {
	if bucket == nil {
		return
	}
	if until.After(bucket.cooldownUntil) {
		bucket.cooldownUntil = until
	}
	if bucket.nextAllowedAt.Before(bucket.cooldownUntil) {
		bucket.nextAllowedAt = bucket.cooldownUntil
	}
}

func maxTime(base time.Time, values ...time.Time) time.Time {
	result := base
	for _, value := range values {
		if value.After(result) {
			result = value
		}
	}
	return result
}

func sleepWithContext(ctx context.Context, wait time.Duration) error {
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
