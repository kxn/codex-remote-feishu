package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

type continuityDelayedImage struct {
	started, release chan struct{}
	completed        chan struct{}
	once             sync.Once
	completeOnce     sync.Once
	delegate         controlledSDKImageStager
}

func (s *continuityDelayedImage) StagePrivateMessage(ctx context.Context, raw platformingress.RawIngressEnvelope, router string) (control.Action, bool, error) {
	defer s.completeOnce.Do(func() { close(s.completed) })
	s.once.Do(func() { close(s.started) })
	select {
	case <-s.release:
	case <-ctx.Done():
		return control.Action{}, false, ctx.Err()
	}
	return s.delegate.StagePrivateMessage(ctx, raw, router)
}

func TestPrivateSDKDelayedImageDiscardedAcrossRouteCommands(t *testing.T) {
	for _, tc := range []struct {
		name, command, notice string
		reselect              bool
	}{{"new", "/new", "下一条文本", false}, {"detach", "/detach", "已解除", true}, {"stop", "/stop", "没有正在运行", false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newContinuityFixture(t)
			path := filepath.Join(t.TempDir(), "late-image.png")
			if err := os.WriteFile(path, []byte("controlled image"), 0600); err != nil {
				t.Fatal(err)
			}
			stager := &continuityDelayedImage{started: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{}), delegate: controlledSDKImageStager{path: path}}
			f.app.applications.ingress.stager = stager
			var release sync.Once
			defer release.Do(func() { close(stager.release) })
			raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "late-image", MessageID: "late-image", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "image", RawContent: `{"image_key":"late"}`, ReceivedAt: time.Now()}
			if err := f.app.applications.hook.Receive(f.ctx, raw); err != nil {
				t.Fatal(err)
			}
			select {
			case <-stager.started:
			case <-f.ctx.Done():
				t.Fatal("image download did not start")
			}
			n := len(f.gateway.snapshotOperations())
			f.user.send(f.app, "discard-"+tc.name, tc.command)
			f.user.waitVisible(f.app, n, tc.notice)
			if tc.reselect {
				f.user.chooseNew(f.app, "after-detach")
			}
			release.Do(func() { close(stager.release) })
			select {
			case <-stager.completed:
			case <-f.ctx.Done():
				t.Fatal("late download did not return")
			}
			n = len(f.gateway.snapshotOperations())
			f.user.send(f.app, "fresh-"+tc.name, "fresh-after-route-change")
			f.user.waitFinal(f.app, n, "fresh-"+tc.name, "continuity-final")
			n = len(f.gateway.snapshotOperations())
			f.user.send(f.app, "next-"+tc.name, "second-after-route-change")
			f.user.waitFinal(f.app, n, "next-"+tc.name, "continuity-final")
			data, e := os.ReadFile(f.trace)
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(data), `"type":"localImage"`) {
				t.Fatal("discarded delayed image was sent into the new turn")
			}
			if !strings.Contains(string(data), "fresh-after-route-change") {
				t.Fatal("fresh message did not reach native")
			}
		})
	}
}
