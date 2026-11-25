package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"log"
	"time"
)

func (r *privateWorkRunner) runOwnerDeadlines(ctx, stop context.Context) error {
	for offset := 0; ; offset += 128 {
		owners, err := r.journal.DeadlineOwnersPage(ctx, 128, offset)
		if err != nil {
			return err
		}
		for _, owner := range owners {
			if stop.Err() != nil {
				return stop.Err()
			}
			if _, err = r.ingress.privatePublicationBindingFor(ctx, owner.Owner, owner.Generation); err != nil {
				continue
			}
			// Queued inputs may already be applied ingress, so no pending SDK
			// event remains to trigger replacement after a clean restart.
			// Only exact proven-unsent input can initiate this recovery.
			checkpoint, checkpointErr := r.journal.LoadCheckpoint(ctx, owner.Owner, owner.Generation)
			if checkpointErr == nil && len(checkpoint.Core.QueuedQueueItemIDs) > 0 && checkpoint.Core.ActiveQueueItemID == "" && retiredCheckpointHasTarget(checkpoint.Core) && r.native != nil {
				proven, proofErr := r.processor.QueuedRecoveryProven(ctx, owner.Owner, owner.Generation)
				if proofErr == nil && proven {
					if stop.Err() != nil {
						return stop.Err()
					}
					_, _ = r.prepareRetiredRuntime(ctx, owner.Owner, owner.Generation)
				}
			}
			if stop.Err() != nil {
				return stop.Err()
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
