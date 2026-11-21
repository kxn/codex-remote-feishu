package worktransport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// DiscoverModels returns one exact native page. NextCursor remains explicit;
// callers can request that cursor without borrowing another carrier's catalog.
// Call outside the ingress pump, which commits the correlated native response.
func (d *Driver) DiscoverModels(ctx context.Context, workerID string, pages ...agentproto.ModelListCommand) (workstore.RuntimeModelCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var value workstore.RuntimeModelCatalog
	var command agentproto.Command
	var instance string
	id := make([]byte, 20)
	if _, err := rand.Read(id); err != nil {
		return value, err
	}
	err := d.store.Within(ctx, func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker(workerID)
		if e != nil {
			return e
		}
		carrier, e := tx.GetCarrier(worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		if !carrier.MatchesPhysical(worker) {
			return fmt.Errorf("model catalog requires this exact carrier")
		}
		query := agentproto.ModelListCommand{Limit: 100}
		if len(pages) > 0 {
			query = pages[0]
		}
		binding := carrier.Owner
		command = agentproto.Command{CommandID: "catalog_" + hex.EncodeToString(id), IssuedAt: d.now(), Kind: agentproto.CommandModelList, Execution: &binding, Target: agentproto.Target{ThreadID: worker.NativeSessionID}, ModelList: query}
		instance = worker.InstanceID
		return nil
	})
	if err != nil {
		return value, err
	}
	if err = d.persistInspection(ctx, command); err != nil {
		return value, err
	}
	if err = d.SendCommand(ctx, instance, command); err != nil {
		_ = d.finishInspection(context.WithoutCancel(ctx), command.CommandID, "", err)
		return value, err
	}
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		err = d.store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			value, e = tx.GetRuntimeModelCatalogForCommand(command.CommandID)
			return e
		})
		if err == nil {
			return value, nil
		}
		if !errors.Is(err, workstore.ErrNotFound) {
			return value, err
		}
		select {
		case <-ctx.Done():
			_ = d.finishInspection(context.WithoutCancel(ctx), command.CommandID, "", ctx.Err())
			return value, ctx.Err()
		case <-tick.C:
		}
	}
}
