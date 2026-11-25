package daemon

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (r *privateWorkRunner) scheduleHostEffects(ctx, stop context.Context) error {
	return r.scheduleHostEffectRounds(ctx, stop, func(work privatechat.PendingEffectWork, route privateEffectRoute) privateEffectAdmission {
		return r.admitEffect(ctx, stop, work, route)
	})
}

// Each independent pool resumes after its last admission and wraps when it
// reaches the end. Retrying records keep their place without starving work
// behind them. Select each owner's first eligible input in chronological
// order before rotating, so rotation never swaps two inputs for that owner.
func (r *privateWorkRunner) scheduleHostEffectRounds(ctx, stop context.Context, admit func(privatechat.PendingEffectWork, privateEffectRoute) privateEffectAdmission) error {
	start := r.effectCursors
	var full [privateEffectPoolCount]bool
	inputHeads := make(map[string]string)
	schedule := func(work privatechat.PendingEffectWork, route privateEffectRoute) {
		switch admit(work, route) {
		case privateEffectAdmitted:
			r.effectCursors[route.pool] = work.Position()
		case privateEffectNoSlot:
			full[route.pool] = true
		}
	}
	if err := r.scanHostEffects(ctx, stop, func(work privatechat.PendingEffectWork) bool {
		route, ok := r.routeEffect(ctx, work)
		if !ok {
			return true
		}
		if route.inputKey != "" {
			if _, seen := inputHeads[route.inputKey]; seen {
				return true
			}
			inputHeads[route.inputKey] = work.Effect.Effect.ID
		}
		if !full[route.pool] && start[route.pool].Before(work.Position()) {
			schedule(work, route)
		}
		return true
	}); err != nil {
		return err
	}
	var wrapThrough privatechat.HostEffectPosition
	for pool, position := range start {
		if !full[pool] && wrapThrough.Before(position) {
			wrapThrough = position
		}
	}
	if wrapThrough.ID == "" {
		return nil
	}
	return r.scanHostEffects(ctx, stop, func(work privatechat.PendingEffectWork) bool {
		if wrapThrough.Before(work.Position()) {
			return false
		}
		route, ok := r.routeEffect(ctx, work)
		if ok && !full[route.pool] && !start[route.pool].Before(work.Position()) &&
			(route.inputKey == "" || inputHeads[route.inputKey] == work.Effect.Effect.ID) {
			schedule(work, route)
		}
		return true
	})
}

func (r *privateWorkRunner) scanHostEffects(ctx, stop context.Context, visit func(privatechat.PendingEffectWork) bool) error {
	const pageSize = 128
	var after privatechat.HostEffectPosition
	for {
		effects, err := r.journal.RunnableHostEffectsAfter(ctx, pageSize, after)
		if err != nil {
			return err
		}
		for _, work := range effects {
			if stop.Err() != nil {
				return stop.Err()
			}
			if !visit(work) {
				return nil
			}
			after = work.Position()
		}
		if len(effects) < pageSize {
			return nil
		}
	}
}

// scheduleEffect isolates slow network/process work from durable ingress and
// gives control/cancellation an independent bounded execution pool.
func (r *privateWorkRunner) scheduleEffect(ctx context.Context, work privatechat.PendingEffectWork) {
	r.scheduleEffectWithStop(ctx, ctx, work)
}

func (r *privateWorkRunner) scheduleEffectWithStop(ctx, stop context.Context, work privatechat.PendingEffectWork) privateEffectAdmission {
	route, ok := r.routeEffect(ctx, work)
	if !ok {
		return privateEffectSkipped
	}
	return r.admitEffect(ctx, stop, work, route)
}

type privateEffectPool uint8

const (
	privateEffectNormal privateEffectPool = iota
	privateEffectControl
	privateEffectPublication
	privateEffectPoolCount
)

type privateEffectRoute struct {
	pool           privateEffectPool
	inputKey       string
	returnsLocally bool
}

func (r *privateWorkRunner) routeEffect(ctx context.Context, work privatechat.PendingEffectWork) (privateEffectRoute, bool) {
	event, _, target, err := privatechat.DecodeStoredEffectTarget(work.Effect.Effect.Body)
	if err == nil && event.Command != nil && work.Effect.State != "confirmed" {
		// Older effects without a frozen target cannot dispatch. Retain the
		// durable record for explicit reconciliation without holding an input.
		if target == nil {
			return privateEffectRoute{}, false
		}
		registered, lookupErr := r.journal.HasNativeDispatch(ctx, work.Effect.Effect.ID)
		if lookupErr != nil || (registered && (privateSessionCommandScope(event.Command.Kind) != privateSessionRead || (event.Command.Target.ThreadID == "" && event.Command.Kind != agentproto.CommandMCPOAuthLogin))) {
			return privateEffectRoute{}, false
		}
		if registered {
			state, e := r.journal.DispatchState(ctx, work.Effect.Effect.ID+":native")
			if e != nil || (state != "prepared" && state != "sending" && state != "unknown" && state != "sent") {
				return privateEffectRoute{}, false
			}
		}
	}
	route := privateEffectRoute{}
	if err == nil && event.DaemonCommand != nil {
		kind := event.DaemonCommand.Kind
		route.returnsLocally = localEffectReturnsAfterHostCall(kind)
		switch kind {
		case control.DaemonCommandKillHeadless, control.DaemonCommandGitWorkspaceImportCancel, control.DaemonCommandGitWorkspaceWorktreeCancel:
			route.pool = privateEffectControl
		}
	}
	if work.Effect.Effect.Lane == "interactive" {
		route.pool = privateEffectPublication
	}
	if err == nil && event.Command != nil && work.Effect.State != "confirmed" && (event.Command.Kind == agentproto.CommandPromptSend || event.Command.Kind == agentproto.CommandTurnSteer) {
		route.inputKey = fmt.Sprintf("input:%v:%d", work.Owner, work.Generation)
	}
	return route, true
}

func (r *privateWorkRunner) admitEffect(ctx, stop context.Context, work privatechat.PendingEffectWork, route privateEffectRoute) privateEffectAdmission {
	key := fmt.Sprintf("%v:%d:%s", work.Owner, work.Generation, work.Effect.Effect.ID)
	return r.startEffect(key, route.inputKey, route.pool, func() {
		if stop.Err() != nil {
			return
		}
		// Scheduled effects can be in a local receipt transaction when worker
		// shutdown begins. stopWorkers waits for their completion; if the
		// caller's shutdown deadline expires, stores remain open.
		effectCtx := context.WithoutCancel(ctx)
		var err error
		if route.returnsLocally {
			err = r.runLocalEffectWithRecovery(effectCtx, work, func() error { return r.runEffect(effectCtx, work) })
		} else {
			err = r.runEffect(effectCtx, work)
		}
		r.diagnostics.report("effect", work.Effect.Effect.ID, err)
	})
}

type privateEffectAdmission uint8

const (
	privateEffectSkipped privateEffectAdmission = iota
	privateEffectAdmitted
	privateEffectNoSlot
)

func (r *privateWorkRunner) startEffect(key, laneKey string, pool privateEffectPool, run func()) privateEffectAdmission {
	r.effectMu.Lock()
	if r.running == nil {
		r.running = make(map[string]struct{})
		r.normalSlots = make(chan struct{}, 8)
		r.controlSlots = make(chan struct{}, 4)
		r.publishSlots = make(chan struct{}, 8)
	}
	if _, exists := r.running[key]; exists {
		r.effectMu.Unlock()
		return privateEffectSkipped
	}
	if laneKey != "" {
		if _, exists := r.running[laneKey]; exists {
			r.effectMu.Unlock()
			return privateEffectSkipped
		}
	}
	slots := r.normalSlots
	switch pool {
	case privateEffectControl:
		slots = r.controlSlots
	case privateEffectPublication:
		slots = r.publishSlots
	}
	select {
	case slots <- struct{}{}:
	default:
		r.effectMu.Unlock()
		return privateEffectNoSlot
	}
	r.running[key] = struct{}{}
	r.effectWG.Add(1)
	if laneKey != "" {
		r.running[laneKey] = struct{}{}
	}
	r.effectMu.Unlock()
	go func() {
		defer r.effectWG.Done()
		defer func() {
			r.effectMu.Lock()
			delete(r.running, key)
			if laneKey != "" {
				delete(r.running, laneKey)
			}
			<-slots
			r.effectMu.Unlock()
		}()
		run()
	}()
	return privateEffectAdmitted
}
