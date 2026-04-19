package control

import "testing"

func TestInlineCardReplacementPolicy(t *testing.T) {
	policy, ok := InlineCardReplacementPolicy(Action{
		Kind: ActionShowCommandMenu,
		Text: "/menu send_settings",
	})
	if !ok {
		t.Fatal("expected inline replacement policy for menu navigation")
	}
	if !policy.ReplaceCurrentCard || !policy.RequiresDaemonFreshness || policy.DaemonFreshness != FeishuUIInlineReplaceFreshnessDaemonLifecycle {
		t.Fatalf("unexpected daemon freshness policy: %#v", policy)
	}
	if policy.RequiresViewSession || policy.ViewSessionStrategy != FeishuUIInlineReplaceViewSessionSurfaceState {
		t.Fatalf("unexpected view/session policy: %#v", policy)
	}

	if _, ok := InlineCardReplacementPolicy(Action{
		Kind: ActionModeCommand,
		Text: "/mode vscode",
	}); !ok {
		t.Fatal("expected parameter apply to follow inline replacement policy when card freshness is present")
	}
}

func TestInlineCardReplacementPolicyActionSet(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   bool
	}{
		{
			name:   "menu navigation",
			action: Action{Kind: ActionShowCommandMenu, Text: "/menu send_settings"},
			want:   true,
		},
		{
			name:   "bare mode",
			action: Action{Kind: ActionModeCommand, Text: "/mode"},
			want:   true,
		},
		{
			name:   "bare verbose",
			action: Action{Kind: ActionVerboseCommand, Text: "/verbose"},
			want:   true,
		},
		{
			name:   "list handoff",
			action: Action{Kind: ActionListInstances},
			want:   true,
		},
		{
			name:   "send file handoff",
			action: Action{Kind: ActionSendFile},
			want:   true,
		},
		{
			name:   "bare history",
			action: Action{Kind: ActionShowHistory, Text: "/history"},
			want:   true,
		},
		{
			name:   "parameter apply",
			action: Action{Kind: ActionModeCommand, Text: "/mode vscode"},
			want:   true,
		},
		{
			name:   "verbose parameter apply",
			action: Action{Kind: ActionVerboseCommand, Text: "/verbose quiet"},
			want:   true,
		},
		{
			name:   "scoped thread expansion",
			action: Action{Kind: ActionShowScopedThreads},
			want:   true,
		},
		{
			name:   "workspace thread expansion",
			action: Action{Kind: ActionShowWorkspaceThreads},
			want:   true,
		},
		{
			name:   "workspace list expand",
			action: Action{Kind: ActionShowAllWorkspaces},
			want:   true,
		},
		{
			name:   "workspace list collapse",
			action: Action{Kind: ActionShowRecentWorkspaces},
			want:   true,
		},
		{
			name:   "thread workspace expand",
			action: Action{Kind: ActionShowAllThreadWorkspaces},
			want:   true,
		},
		{
			name:   "thread workspace collapse",
			action: Action{Kind: ActionShowRecentThreadWorkspaces},
			want:   true,
		},
		{
			name:   "thread return action",
			action: Action{Kind: ActionShowAllThreads},
			want:   true,
		},
		{
			name:   "thread attach action",
			action: Action{Kind: ActionUseThread},
			want:   false,
		},
		{
			name:   "path picker navigation",
			action: Action{Kind: ActionPathPickerEnter, PickerID: "picker-1", PickerEntry: "subdir"},
			want:   true,
		},
		{
			name:   "history page navigation",
			action: Action{Kind: ActionHistoryPage, PickerID: "history-1", Page: 1},
			want:   true,
		},
		{
			name:   "history detail navigation",
			action: Action{Kind: ActionHistoryDetail, PickerID: "history-1", TurnID: "turn-1"},
			want:   true,
		},
		{
			name:   "path picker confirm stays append-only",
			action: Action{Kind: ActionPathPickerConfirm, PickerID: "picker-1"},
			want:   false,
		},
		{
			name:   "path picker cancel stays append-only",
			action: Action{Kind: ActionPathPickerCancel, PickerID: "picker-1"},
			want:   false,
		},
		{
			name:   "workspace attach action",
			action: Action{Kind: ActionAttachWorkspace},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := InlineCardReplacementPolicy(tt.action)
			if ok != tt.want {
				t.Fatalf("InlineCardReplacementPolicy(%#v) ok = %v, want %v", tt.action, ok, tt.want)
			}
		})
	}
}

func TestAllowsInlineCardReplacementRequiresDaemonFreshness(t *testing.T) {
	action := Action{
		Kind: ActionShowCommandMenu,
		Text: "/menu send_settings",
	}
	if AllowsInlineCardReplacement(action) {
		t.Fatal("expected unstamped navigation to stay async")
	}

	action.Inbound = &ActionInboundMeta{CardDaemonLifecycleID: "life-1"}
	if !AllowsInlineCardReplacement(action) {
		t.Fatal("expected stamped navigation to allow inline replacement")
	}
}

func TestAllowsInlineCardReplacementForPathPickerNavigation(t *testing.T) {
	action := Action{
		Kind:     ActionPathPickerEnter,
		PickerID: "picker-1",
		Inbound:  &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	}
	if !AllowsInlineCardReplacement(action) {
		t.Fatal("expected inline replacement for path picker navigation")
	}
}

func TestAllowsInlineCardReplacementForCommandCardApply(t *testing.T) {
	action := Action{
		Kind:    ActionReasoningCommand,
		Text:    "/reasoning high",
		Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	}
	if !AllowsInlineCardReplacement(action) {
		t.Fatal("expected inline replacement for command-card apply")
	}
}

func TestAllowsCommandCardResultReplacement(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   bool
	}{
		{
			name: "help from stamped card callback",
			action: Action{
				Kind:    ActionShowCommandHelp,
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "status from stamped card callback",
			action: Action{
				Kind:    ActionStatus,
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "typed status stays append-only",
			action: Action{
				Kind: ActionStatus,
				Text: "/status",
			},
			want: false,
		},
		{
			name: "list does not become stamped result replacement",
			action: Action{
				Kind:    ActionListInstances,
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllowsCommandCardResultReplacement(tt.action); got != tt.want {
				t.Fatalf("AllowsCommandCardResultReplacement(%#v) = %v, want %v", tt.action, got, tt.want)
			}
		})
	}
}

func TestAllowsBareCommandContinuation(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   bool
	}{
		{
			name: "bare upgrade from stamped card callback",
			action: Action{
				Kind:    ActionUpgradeCommand,
				Text:    "/upgrade",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "bare debug from stamped card callback",
			action: Action{
				Kind:    ActionDebugCommand,
				Text:    "/debug",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "bare cron from stamped card callback",
			action: Action{
				Kind:    ActionCronCommand,
				Text:    "/cron",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "cron with args stays async",
			action: Action{
				Kind:    ActionCronCommand,
				Text:    "/cron status",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
		{
			name: "typed bare cron stays async",
			action: Action{
				Kind: ActionCronCommand,
				Text: "/cron",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllowsBareCommandContinuation(tt.action); got != tt.want {
				t.Fatalf("AllowsBareCommandContinuation(%#v) = %v, want %v", tt.action, got, tt.want)
			}
		})
	}
}

func TestAllowsCommandSubmissionAnchorReplacement(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   bool
	}{
		{
			name: "status from stamped card callback",
			action: Action{
				Kind:    ActionStatus,
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
		{
			name: "list from stamped card callback",
			action: Action{
				Kind:    ActionListInstances,
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
		{
			name: "bare menu navigation stays inline policy path",
			action: Action{
				Kind:    ActionShowCommandMenu,
				Text:    "/menu maintenance",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
		{
			name: "parameter apply does not become submission anchor",
			action: Action{
				Kind:    ActionModeCommand,
				Text:    "/mode vscode",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
		{
			name: "unstamped status stays async",
			action: Action{
				Kind: ActionStatus,
			},
			want: false,
		},
		{
			name: "bare upgrade from stamped card callback",
			action: Action{
				Kind:    ActionUpgradeCommand,
				Text:    "/upgrade",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
		{
			name: "bare debug from stamped card callback",
			action: Action{
				Kind:    ActionDebugCommand,
				Text:    "/debug",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
		{
			name: "bare cron from stamped card callback",
			action: Action{
				Kind:    ActionCronCommand,
				Text:    "/cron",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
		{
			name: "steerall from stamped card callback",
			action: Action{
				Kind:    ActionSteerAll,
				Text:    "/steerall",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
		{
			name: "debug with form args stays async",
			action: Action{
				Kind:    ActionDebugCommand,
				Text:    "/debug admin",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllowsCommandSubmissionAnchorReplacement(tt.action); got != tt.want {
				t.Fatalf("AllowsCommandSubmissionAnchorReplacement(%#v) = %v, want %v", tt.action, got, tt.want)
			}
		})
	}
}
