package daemon

import (
	"testing"
	"time"
)

func TestPrivateEffectRunnerIsolatesBlockedWorkAndDeduplicates(t *testing.T) {
	r := &privateWorkRunner{}
	blocked := make(chan struct{})
	started := make(chan struct{}, 16)
	for i := 0; i < 8; i++ {
		r.startEffect(string(rune('a'+i)), "", false, false, func() { started <- struct{}{}; <-blocked })
	}
	for i := 0; i < 8; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("normal work failed to start")
		}
	}
	cancel := make(chan struct{}, 1)
	r.startEffect("cancel", "", true, false, func() { cancel <- struct{}{} })
	select {
	case <-cancel:
	case <-time.After(time.Second):
		t.Fatal("cancel blocked behind slow work")
	}
	publication := make(chan struct{}, 1)
	r.startEffect("menu", "", false, true, func() { publication <- struct{}{} })
	select {
	case <-publication:
	case <-time.After(time.Second):
		t.Fatal("publication blocked behind slow work")
	}
	duplicate := make(chan struct{}, 1)
	r.startEffect("a", "", true, false, func() { duplicate <- struct{}{} })
	select {
	case <-duplicate:
		t.Fatal("same effect ran twice")
	default:
	}
	close(blocked)
	r.effectWG.Wait()
}

func TestPrivateEffectRunnerSerializesOwnerInput(t *testing.T) {
	r := &privateWorkRunner{}
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	next := make(chan struct{}, 1)
	r.startEffect("first", "owner-input", false, false, func() { started <- struct{}{}; <-release })
	<-started
	r.startEffect("second", "owner-input", false, false, func() { next <- struct{}{} })
	select {
	case <-next:
		t.Fatal("owner input ran concurrently")
	default:
	}
	close(release)
	r.effectWG.Wait()
	r.startEffect("second", "owner-input", false, false, func() { next <- struct{}{} })
	select {
	case <-next:
	case <-time.After(time.Second):
		t.Fatal("next input did not start")
	}
	r.effectWG.Wait()
}
