package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"log"
	"time"
)

func (r *privateWorkRunner) runOwnerDeadlines(ctx context.Context) error {
	for offset := 0; ; offset += 128 {
		owners, err := r.journal.DeadlineOwnersPage(ctx, 128, offset)
		if err != nil {
			return err
		}
		for _, owner := range owners {
			if _, err = r.ingress.privatePublicationBindingFor(ctx, owner.Owner, owner.Generation); err != nil {
				continue
			}
			now := time.Now().UTC().Truncate(time.Second)
			if _, err = r.processor.ApplyOwnerDeadline(ctx, owner.Owner, owner.Generation, fmt.Sprintf("tick:%d", now.Unix()), now); err != nil {
				log.Printf("private owner deadline gateway=%s generation=%d: %v", owner.Owner.GatewayID, owner.Generation, err)
			}
		}
		if len(owners) < 128 {
			break
		}
	}
	return nil
}

// Check the precise persisted process identity without treating carrier exit as
// evidence that any remote turn, descendant, or published action completed.
func (r *privateWorkRunner) observeExitedPendingCarrier(ctx context.Context, rec privateCarrierRecord) {
	if rec.Snapshot.Owner.HostID != r.native.hostID || rec.Process.PID <= 0 || rec.Process.StartIdentity == "" {
		return
	}
	if _, err := r.ingress.privatePublicationBindingFor(ctx, rec.Surface, rec.SurfaceGeneration); err != nil {
		return
	}
	pending, err := r.processor.OwnerPendingLaunchInstance(ctx, rec.Surface, rec.SurfaceGeneration)
	if err != nil {
		log.Printf("private pending launch gateway=%s generation=%d: %v", rec.Surface.GatewayID, rec.SurfaceGeneration, err)
		return
	}
	if pending != rec.Snapshot.Owner.InstanceID {
		return
	}
	r.deadlineMu.Lock()
	if r.exitProbeAt == nil {
		r.exitProbeAt = make(map[string]time.Time)
	}
	last := r.exitProbeAt[rec.Snapshot.Owner.CarrierIncarnation]
	if time.Since(last) < time.Second {
		r.deadlineMu.Unlock()
		return
	}
	r.exitProbeAt[rec.Snapshot.Owner.CarrierIncarnation] = time.Now()
	r.deadlineMu.Unlock()
	probe, cancel := context.WithTimeout(ctx, time.Millisecond)
	err = hostinventory.WaitProcessExit(probe, rec.Process)
	cancel()
	if err != nil {
		return
	}
	receipt, err := json.Marshal(struct {
		Instance, Incarnation string
		Process               hostinventory.ProcessIdentity
	}{rec.Snapshot.Owner.InstanceID, rec.Snapshot.Owner.CarrierIncarnation, rec.Process})
	if err != nil {
		return
	}
	if _, err = r.processor.ApplyOwnerLaunchExit(ctx, rec.Surface, rec.SurfaceGeneration, rec.Snapshot.Owner.InstanceID, string(receipt), time.Now().UTC()); err != nil {
		log.Printf("private launch exit gateway=%s generation=%d: %v", rec.Surface.GatewayID, rec.SurfaceGeneration, err)
	}
}
