package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"testing"
	"time"
)

func TestAutoContinueBootstrapOnlyPromotesExactCommittedThread(t *testing.T) {
	for _, tc := range []struct {
		name, thread, instance string
		policy                 agentproto.SurfaceBindingPolicy
		want                   bool
	}{{"same thread", "thread-1", "inst-1", agentproto.SurfaceBindingPolicyFollowExecutionThread, true}, {"different thread", "other", "inst-1", agentproto.SurfaceBindingPolicyFollowExecutionThread, false}, {"different instance", "thread-1", "other", agentproto.SurfaceBindingPolicyFollowExecutionThread, false}, {"detached branch", "thread-1", "inst-1", agentproto.SurfaceBindingPolicyKeepSurfaceSelection, false}} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			s := newServiceForTest(&now)
			v := setupAutoWhipSurface(t, s)
			v.AutoWhip.Enabled = false
			v.AutoContinue.Enabled = true
			v.RouteMode = state.RouteModePinned
			v.SelectedThreadID = tc.thread
			v.AutoContinue.Episode = &state.PendingAutoContinueEpisodeRecord{EpisodeID: "episode", InstanceID: tc.instance, FrozenRouteMode: state.RouteModeNewThreadReady, FrozenDispatchPlan: agentproto.PromptDispatchPlan{ExecutionMode: agentproto.PromptExecutionModeStartNew, ExecutionThreadID: "thread-1", CWD: "/work", SurfaceBindingPolicy: tc.policy}, State: state.AutoContinueEpisodeScheduled, PendingDueAt: now.Add(time.Minute)}
			_ = s.maybeDispatchPendingAutoContinue(v, now)
			ep := v.AutoContinue.Episode
			if !tc.want {
				if ep != nil {
					t.Fatalf("foreign route retained episode: %+v", ep)
				}
				return
			}
			if ep == nil || ep.FrozenRouteMode != state.RouteModePinned || ep.FrozenDispatchPlan.ExecutionMode != agentproto.PromptExecutionModeResumeExisting || ep.FrozenDispatchPlan.ExecutionThreadID != "thread-1" {
				t.Fatalf("exact bootstrap not promoted: %+v", ep)
			}
		})
	}
}
