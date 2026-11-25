package privatechat

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestPendingDetachStopStillFencesAttachAfterProcessorRestart(t *testing.T) {
	j, p, lease, owner, _ := effectFixture(t)
	detach := privateAction(owner, control.ActionDetach)
	_, effects := applyEffectAction(t, j, p, owner, "stop-before-restart", "control", nil, detach)
	found := false
	for _, effect := range effects {
		event, err := DecodeEffectEvent(effect.Effect.Body)
		if err != nil {
			t.Fatal(err)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandKillHeadless && event.DaemonCommand.InstanceID == "instance" {
			found = true
		}
	}
	if !found {
		t.Fatal("detach did not persist an exact carrier stop")
	}
	p.poisoned = true
	var err error
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyAuthenticatedNativeHello(t.Context(), lease, nativeHello(lease.native), "stop-after-restart"); err != nil {
		t.Fatal(err)
	}
	attach := privateAction(owner, control.ActionAttachInstance)
	attach.InstanceID = "instance"
	_, result := applyEffectAction(t, j, p, owner, "attach-stopping-after-restart", "control", nil, attach)
	for _, effect := range result {
		event, err := DecodeEffectEvent(effect.Effect.Body)
		if err != nil {
			t.Fatal(err)
		}
		if event.Notice != nil && event.Notice.Code == "private_instance_stopping" {
			if p.app.Service().Surface(owner.SurfaceID).AttachedInstanceID != "" {
				t.Fatal("stopping carrier attached")
			}
			return
		}
	}
	t.Fatal("pending stop did not fence attach after restart")
}
