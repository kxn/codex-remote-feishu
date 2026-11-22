package feishu

import (
	"strings"
	"sync"
	"time"
)

// Feishu's recipient send quota is shared across application identities. Live
// gateways share this process budget in addition to their app/API/message
// budgets. Product-level background windows live in the durable publisher.
type sharedReceiveBudget struct {
	mu      sync.Mutex
	buckets map[string]*callBucketState
}

var liveReceiveBudget = &sharedReceiveBudget{buckets: map[string]*callBucketState{}}

func receiveBudgetKey(spec CallSpec) string {
	if spec.Class != CallClassIMSend && spec.Class != CallClassIMPatch {
		return ""
	}
	key := strings.TrimSpace(spec.ResourceKey.ReceiveTarget)
	key = strings.TrimPrefix(key, "chat_id:")
	// Open chat identifiers are globally unique; do not combine user open IDs
	// across Bot applications, since those IDs have application-specific scope.
	if !strings.HasPrefix(key, "oc_") {
		return ""
	}
	return key
}

func (b *sharedReceiveBudget) bucket(key string, now time.Time) *callBucketState {
	// Remove expired destinations occasionally; long-running daemons may serve
	// many chats, while only recent destinations need a quota reservation.
	if len(b.buckets) > 4096 {
		for id, bucket := range b.buckets {
			if bucket.nextAllowedAt.Before(now.Add(-time.Minute)) && bucket.cooldownUntil.Before(now) {
				delete(b.buckets, id)
			}
		}
	}
	bucket := b.buckets[key]
	if bucket == nil {
		bucket = &callBucketState{}
		b.buckets[key] = bucket
	}
	return bucket
}
