package daemon

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (a *App) authorizeApplicationHello(hello agentproto.Hello, credential string) error {
	done, ok := a.beginApplicationNativeDelivery()
	if !ok {
		return errApplicationClosing
	}
	defer done()
	if a.applications == nil || a.applications.native == nil {
		if hello.PrivateNativeProtocolVersion != 0 || credential != "" {
			return fmt.Errorf("private application is not configured")
		}
		return nil
	}
	if hello.PrivateNativeProtocolVersion != 0 {
		return a.applications.native.store.authorize(hello, credential)
	}
	if hello.WorkerProtocolVersion != 0 && a.applications.teamOwners != nil {
		owners := a.applications.teamOwners
		reg, old, err := owners.commandAuthority(context.Background(), hello.CarrierIncarnation, hello.Instance.InstanceID)
		if err != nil {
			return err
		}
		if old {
			if credential != "" {
				return fmt.Errorf("legacy team carrier cannot use shared credential")
			}
		} else {
			if a.teamRuntime == nil || a.teamRuntime.signer == nil {
				return fmt.Errorf("team relay signer unavailable")
			}
			expected, e := a.teamRuntime.signer.RelayCredential(reg.Carrier.Incarnation, reg.Carrier.InstanceID, reg.Carrier.Backend)
			if e != nil || credential == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(credential)) != 1 {
				return fmt.Errorf("team carrier authentication failed")
			}
		}
		// Also reject collisions with a protected private instance.
		return a.applications.native.store.authorize(hello, "")
	}
	if a.applications.teamOwners != nil {
		if handled, err := a.applications.teamOwners.rejectUnboundSharedInstance(context.Background(), hello.Instance.InstanceID); handled {
			if err != nil {
				return err
			}
			return fmt.Errorf("team protocol identity required")
		}
	}
	return a.applications.native.store.authorize(hello, credential)
}

func (a *App) applicationHello(ctx context.Context, meta relayws.ConnectionMeta, hello agentproto.Hello) {
	done, ok := a.beginApplicationNativeDelivery()
	if !ok {
		return
	}
	defer done()
	if !meta.PrivateAuthenticated {
		if handled, err := a.teamHello(ctx, meta, hello); handled {
			if err != nil {
				log.Printf("team shared-owner hello rejected: instance=%s err=%v", hello.Instance.InstanceID, err)
			}
			return
		}
		a.enqueueHello(ctx, meta, hello)
		return
	}
	if a.applications == nil || a.applications.native == nil {
		return
	}
	if err := a.applications.native.acceptHello(ctx, meta, hello); err != nil {
		log.Printf("private native hello not applied: instance=%s err=%v", hello.Instance.InstanceID, err)
		return
	}
	a.wakePrivateApplication()
}

func (a *App) applicationEvents(ctx context.Context, meta relayws.ConnectionMeta, instance string, events []agentproto.Event) {
	done, ok := a.beginApplicationNativeDelivery()
	if !ok {
		return
	}
	defer done()
	if !meta.PrivateAuthenticated {
		if handled, err := a.teamEvents(ctx, meta, instance, events); handled {
			if err != nil {
				log.Printf("team shared-owner event rejected: instance=%s err=%v", instance, err)
			}
			return
		}
		a.enqueueEvents(ctx, meta, instance, events)
		return
	}
	if a.applications == nil || a.applications.native == nil {
		return
	}
	applyPrivateEventBatch(events, func(event agentproto.Event) error {
		return a.applications.native.receive(ctx, meta, instance, event)
	}, func(event agentproto.Event, err error) {
		log.Printf("private native event not applied: instance=%s kind=%s err=%v", instance, event.Kind, err)
	})
	a.wakePrivateApplication()
}

// Live display frames are individually authenticated and validated by receive;
// rejecting one must not drop later durable frames in the same envelope.
// Durable failures stop this batch so their journal sequence remains ordered.
func applyPrivateEventBatch(events []agentproto.Event, receive func(agentproto.Event) error, report func(agentproto.Event, error)) {
	for _, event := range events {
		if err := receive(event); err != nil {
			report(event, err)
			if event.Live == nil {
				return
			}
		}
	}
}

func (a *App) applicationAck(ctx context.Context, meta relayws.ConnectionMeta, instance string, ack agentproto.CommandAck) {
	done, ok := a.beginApplicationNativeDelivery()
	if !ok {
		return
	}
	defer done()
	if !meta.PrivateAuthenticated {
		if handled, err := a.teamAck(ctx, meta, instance, ack); handled {
			if err != nil {
				log.Printf("team shared-owner ack rejected: instance=%s err=%v", instance, err)
			}
			return
		}
		a.enqueueCommandAck(ctx, meta, instance, ack)
		return
	}
	if a.applications == nil || a.applications.native == nil {
		return
	}
	if err := a.applications.native.receiveAck(ctx, meta, instance, ack); err != nil {
		log.Printf("private native command receipt pending: instance=%s command=%s err=%v", instance, ack.CommandID, err)
	}
	a.wakePrivateApplication()
}

func (a *App) applicationDisconnect(ctx context.Context, meta relayws.ConnectionMeta, instance string) {
	done, ok := a.beginApplicationNativeDelivery()
	if !ok {
		return
	}
	defer done()
	if !meta.PrivateAuthenticated {
		if handled, err := a.teamDisconnect(ctx, meta, instance); handled {
			if err != nil {
				log.Printf("team shared-owner disconnect failed: instance=%s err=%v", instance, err)
			}
			return
		}
		a.enqueueDisconnect(ctx, meta, instance)
		return
	}
	if a.applications == nil || a.applications.native == nil {
		return
	}
	if err := a.applications.native.disconnect(ctx, meta, instance); err != nil {
		log.Printf("private native disconnect not applied: instance=%s err=%v", instance, err)
	}
	a.wakePrivateApplication()
}

func (a *App) wakePrivateApplication() {
	if a.applications != nil && a.applications.runner != nil {
		a.applications.runner.signal()
	}
}
