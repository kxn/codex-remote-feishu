package daemon

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
)

func TestPrivateDisconnectRejectsUnauthenticatedAndIgnoresReplacedSocket(t *testing.T) {
	host := &privateNativeHost{connections: map[string]uint64{"instance": 2}}
	if err := host.disconnect(context.Background(), relayws.ConnectionMeta{ConnectionID: 2}, "instance"); err == nil {
		t.Fatal("unauthenticated close accepted")
	}
	if err := host.disconnect(context.Background(), relayws.ConnectionMeta{ConnectionID: 1, PrivateAuthenticated: true}, "instance"); err != nil {
		t.Fatal(err)
	}
	if host.connections["instance"] != 2 {
		t.Fatal("old connection removed replacement")
	}
}
