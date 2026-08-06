package feishu

import (
	"context"
	"sort"
	"strings"
	"time"
)

func (c *MultiGatewayController) UpsertApp(ctx context.Context, cfg GatewayAppConfig) error {
	cfg = normalizeGatewayAppConfig(cfg)
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	c.mu.Lock()
	worker := c.workers[cfg.GatewayID]
	if worker == nil {
		worker = &gatewayWorker{}
		c.workers[cfg.GatewayID] = worker
	}
	gate := c.retireWorkerLocked(worker)
	c.mu.Unlock()
	gate.wait()

	c.mu.Lock()
	c.finishWorkerStopLocked(worker)
	worker.config = cfg
	worker.status = GatewayStatus{
		GatewayID:      cfg.GatewayID,
		Name:           cfg.Name,
		State:          GatewayStateStopped,
		Disabled:       !cfg.Enabled,
		LastVerifiedAt: worker.status.LastVerifiedAt,
	}
	if !cfg.Enabled {
		worker.status.State = GatewayStateDisabled
		c.mu.Unlock()
		return nil
	}
	if !workerHasCredentials(cfg) {
		worker.status.State = GatewayStateAuthFailed
		worker.status.LastError = "missing app credentials"
		c.mu.Unlock()
		return nil
	}
	if c.started && c.startCtx != nil {
		err := c.ensureWorkerRunningLocked(cfg.GatewayID)
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	_ = ctx
	return nil
}

func (c *MultiGatewayController) RemoveApp(_ context.Context, gatewayID string) error {
	gatewayID = normalizeGatewayID(gatewayID)
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	c.mu.Lock()
	worker := c.workers[gatewayID]
	if worker == nil {
		c.mu.Unlock()
		return nil
	}
	gate := c.retireWorkerLocked(worker)
	c.mu.Unlock()
	gate.wait()

	c.mu.Lock()
	c.finishWorkerStopLocked(worker)
	delete(c.workers, gatewayID)
	c.mu.Unlock()
	return nil
}

func (c *MultiGatewayController) Verify(ctx context.Context, cfg GatewayAppConfig) (VerifyResult, error) {
	cfg = normalizeGatewayAppConfig(cfg)
	result, err := VerifyGatewayConnection(ctx, LiveGatewayConfig{
		GatewayID:      cfg.GatewayID,
		AppID:          cfg.AppID,
		AppSecret:      cfg.AppSecret,
		Domain:         cfg.Domain,
		TempDir:        cfg.ImageTempDir,
		UseSystemProxy: cfg.UseSystemProxy,
	})
	if err == nil {
		c.mu.Lock()
		if worker := c.workers[cfg.GatewayID]; worker != nil {
			worker.status.LastVerifiedAt = time.Now().UTC()
			worker.status.LastError = ""
		}
		c.mu.Unlock()
	}
	return result, err
}

func (c *MultiGatewayController) Status() []GatewayStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	values := make([]GatewayStatus, 0, len(c.workers))
	for _, worker := range c.workers {
		if worker == nil {
			continue
		}
		values = append(values, worker.status)
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].GatewayID < values[j].GatewayID
	})
	return values
}

func (c *MultiGatewayController) SetBotOpenID(gatewayID, openID string) {
	gatewayID = normalizeGatewayID(gatewayID)
	if gatewayID == "" || strings.TrimSpace(openID) == "" {
		return
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	worker := c.workers[gatewayID]
	if worker == nil || worker.runtime == nil {
		return
	}
	if setter, ok := worker.runtime.(interface{ SetBotOpenID(string) }); ok {
		setter.SetBotOpenID(strings.TrimSpace(openID))
	}
}
