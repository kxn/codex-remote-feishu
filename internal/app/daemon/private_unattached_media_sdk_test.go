package daemon

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

// The legacy private surface rejected unattached media rather than retaining it.
func TestPrivateSDKUnattachedMediaRequiresResend(t *testing.T) {
	for _, tc := range []struct{ name, kind, body string }{
		{"image", "image", `{"image_key":"one"}`},
		{"post", "post", `{"title":"rich text"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stager := newStructuredUATStager(t)
			f := newContinuityFixtureBare(t, func(a *App) { a.applications.ingress.stager = stager })
			raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "cold-media", MessageID: "cold-media", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: tc.kind, RawContent: tc.body, ReceivedAt: time.Now()}
			before := len(f.gateway.snapshotOperations())
			if err := f.app.applications.hook.Receive(f.ctx, raw); err != nil {
				t.Fatal(err)
			}
			f.user.waitVisible(f.app, before, "/list")
			f.user.chooseNew(f.app, "after-unattached-media")
			before = len(f.gateway.snapshotOperations())
			f.user.send(f.app, "after-unattached", "fresh-after-unattached")
			f.user.waitFinal(f.app, before, "after-unattached", "continuity-final")
			data, err := os.ReadFile(f.trace)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), `"type":"localImage"`) || strings.Contains(string(data), "rich text") {
				t.Fatal("unattached media was replayed")
			}
			if !strings.Contains(string(data), "fresh-after-unattached") {
				t.Fatal("new input did not reach native")
			}
		})
	}
}
