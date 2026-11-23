package wrapper

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func runRelayClient(ctx context.Context, relayURL string, client *relayws.Client, manager *relayruntime.Manager, connectedOnce func() bool, onDisconnect func()) error {
	backoff := 200 * time.Millisecond
	for {
		if !connectedOnce() {
			if err := manager.EnsureReady(ctx); err != nil {
				return err
			}
		}
		err := client.RunOnce(ctx)
		onDisconnect()
		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}
		var fatal relayws.FatalError
		if errors.As(err, &fatal) {
			return err
		}
		if !connectedOnce() {
			log.Printf("relay bootstrap connection failed: url=%s err=%v", relayURL, err)
		} else {
			log.Printf("relay steady reconnect failed: url=%s err=%v", relayURL, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 5*time.Second {
			backoff *= 2
		}
	}
}

func relayWelcomeSummary(welcome agentproto.Welcome) string {
	if welcome.Server == nil {
		return "relay without server identity"
	}
	switch {
	case welcome.Server.BuildFingerprint != "":
		return welcome.Server.BuildFingerprint
	case welcome.Server.Version != "":
		return welcome.Server.Version
	default:
		return "unknown relay identity"
	}
}
