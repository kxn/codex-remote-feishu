package privatechat

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"testing"
)

func TestPrivateOldCheckpointUsesAppliedCarrierThreadFact(t *testing.T) {
	j, p, lease, o, binding := effectFixture(t)
	event := agentproto.Event{Kind: agentproto.EventThreadDiscovered, ThreadID: "thread", CWD: "/tmp/workspace"}
	ref := persistNative(t, t.Context(), lease, binding, 1, event)
	if _, err := p.ApplyNative(t.Context(), lease, ref, CoreNativeReducer{}); err != nil {
		t.Fatal(err)
	}
	cp, err := j.LoadCheckpoint(t.Context(), o, 1)
	if err != nil || cp.Core.Navigation != nil {
		t.Fatalf("fixture must emulate old checkpoint: %v", err)
	}
	p.poisoned = true
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyAuthenticatedNativeHello(t.Context(), lease, nativeHello(lease.native), "reconnect", orchestrator.PrivateNativeProfiles{})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range effects {
		e, err := DecodeEffectEvent(x.Effect.Body)
		if err != nil {
			t.Fatal(err)
		}
		if e.Command != nil && e.Command.Kind == agentproto.CommandThreadHistoryRead {
			t.Fatal("queried despite verified applied fact")
		}
	}
	cp, err = j.LoadCheckpoint(t.Context(), o, 1)
	if err != nil || cp.Core.Navigation == nil || cp.Core.Navigation.ThreadID != "thread" {
		t.Fatalf("navigation not reconstructed: %v", err)
	}
}

func TestPrivateOldCheckpointQueryPersistsAcrossRepeatedRestart(t *testing.T) {
	j, p, lease, o, _ := effectFixture(t)
	p.poisoned = true
	var err error
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyAuthenticatedNativeHello(t.Context(), lease, nativeHello(lease.native), "connect-1", orchestrator.PrivateNativeProfiles{})
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, x := range effects {
		e, _ := DecodeEffectEvent(x.Effect.Body)
		if e.Command != nil && e.Command.Kind == agentproto.CommandThreadHistoryRead {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("query effects=%d", found)
	}
	cp, err := j.LoadCheckpoint(t.Context(), o, 1)
	if err != nil || cp.Core.ReconnectQuery == nil {
		t.Fatalf("missing frozen query: %v", err)
	}
	original := *cp.Core.ReconnectQuery
	p.poisoned = true
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	effects, err = p.ApplyAuthenticatedNativeHello(t.Context(), lease, nativeHello(lease.native), "connect-2", orchestrator.PrivateNativeProfiles{})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range effects {
		e, _ := DecodeEffectEvent(x.Effect.Body)
		if e.Command != nil && e.Command.Kind == agentproto.CommandThreadHistoryRead {
			t.Fatal("issued duplicate query")
		}
	}
	cp, err = j.LoadCheckpoint(t.Context(), o, 1)
	if err != nil || cp.Core.ReconnectQuery == nil || *cp.Core.ReconnectQuery != original {
		t.Fatal("query identity/deadline changed")
	}
}
