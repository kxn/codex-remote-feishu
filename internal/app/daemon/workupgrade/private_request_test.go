package workupgrade

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

type privateRequestVerifier struct{ allow bool }

func (v privateRequestVerifier) WithPrivateUpgradeRequest(_ context.Context, _ PrivateRequest, commit func() error) error {
	if !v.allow {
		return workstore.ErrScopeDenied
	}
	return commit()
}

func TestPrivateRequestUsesSharedInstallTransactionAndReplays(t *testing.T) {
	c, base, _ := upgradeFixture(t)
	req := PrivateRequest{ID: "private-upgrade", TargetVersion: base.TargetVersion, InstallationFingerprint: base.InstallationFingerprint, Source: workstore.NativeUpgradePrivateSource{Tenant: "tenant", AppID: "app", Gateway: "g", Surface: "s", Chat: "c", User: "u", Generation: 1, EffectID: "effect"}}
	if _, err := c.RequestPrivate(t.Context(), req, privateRequestVerifier{}); err == nil {
		t.Fatal("untrusted private request accepted")
	}
	if _, err := c.RequestPrivate(t.Context(), req, privateRequestVerifier{true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	result, err := c.RequestPrivate(t.Context(), req, privateRequestVerifier{true})
	if err != nil || result.Stage != "completed" {
		t.Fatalf("idempotent request after install: %+v %v", result, err)
	}
	foreign := req
	foreign.Source.User = "other"
	if _, err = c.RequestPrivate(t.Context(), foreign, privateRequestVerifier{true}); err == nil {
		t.Fatal("other owner reused private request")
	}
}
