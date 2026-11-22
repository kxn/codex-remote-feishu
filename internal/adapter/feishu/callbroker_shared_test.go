package feishu

import (
	"testing"
	"time"
)

func TestLiveGatewaysShareRecipientBudgetAcrossBotsAndMessages(t *testing.T) {
	a := NewLiveGateway(LiveGatewayConfig{GatewayID: "a"})
	b := NewLiveGateway(LiveGatewayConfig{GatewayID: "b"})
	if a.broker.sharedReceive == nil || a.broker.sharedReceive != b.broker.sharedReceive {
		t.Fatal("live gateway wiring omitted shared budget")
	}
	// Isolated clock domain, still shared by both distinct bot gateways.
	budget := &sharedReceiveBudget{buckets: map[string]*callBucketState{}}
	a.broker.sharedReceive, b.broker.sharedReceive = budget, budget
	now := time.Date(2026, 11, 22, 1, 0, 0, 0, time.UTC)
	a.broker.now = func() time.Time { return now }
	b.broker.now = a.broker.now
	spec := CallSpec{API: "im.v1.message.create", Class: CallClassIMSend, ResourceKey: FeishuResourceKey{ReceiveTarget: "oc_group"}}
	if _, ok := a.broker.reserveNow(spec); !ok {
		t.Fatal("first send denied")
	}
	spec.ResourceKey.ReceiveTarget = "chat_id:oc_group"
	spec.ResourceKey.MessageID = "om_different"
	if wait, ok := b.broker.reserveNow(spec); ok || wait != 250*time.Millisecond {
		t.Fatalf("cross-bot quota bypass wait=%s ok=%v", wait, ok)
	}
	now = now.Add(250 * time.Millisecond)
	if _, ok := b.broker.reserveNow(spec); !ok {
		t.Fatal("budget did not reopen")
	}
	spec.ResourceKey.ReceiveTarget = "oc_other"
	now = now.Add(50 * time.Millisecond)
	if _, ok := a.broker.reserveNow(spec); !ok {
		t.Fatal("unrelated group blocked")
	}
}
