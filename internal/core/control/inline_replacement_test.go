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
			name:   "workspace root page",
			action: Action{Kind: ActionWorkspaceRoot, Text: "/workspace"},
			want:   true,
		},
		{
			name:   "workspace new page",
			action: Action{Kind: ActionWorkspaceNew, Text: "/workspace new"},
			want:   true,
		},
		{
			name:   "workspace list page handoff",
			action: Action{Kind: ActionWorkspaceList, Text: "/workspace list"},
			want:   true,
		},
		{
			name:   "workspace new dir handoff",
			action: Action{Kind: ActionWorkspaceNewDir, Text: "/workspace new dir"},
			want:   true,
		},
		{
			name:   "workspace new git handoff",
			action: Action{Kind: ActionWorkspaceNewGit, Text: "/workspace new git"},
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
			name:   "request step navigation",
			action: Action{Kind: ActionRespondRequest, RequestID: "req-1", RequestOptionID: "step_next"},
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

func TestAllowsInlineCardReplacementForRequestStepRefresh(t *testing.T) {
	action := Action{
		Kind:            ActionRespondRequest,
		RequestID:       "req-1",
		RequestOptionID: "step_next",
		Inbound:         &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	}
	if !AllowsInlineCardReplacement(action) {
		t.Fatal("expected inline replacement for request step refresh")
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
			name: "list can replace stamped card with first real result",
			action: Action{
				Kind:    ActionListInstances,
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "use can replace stamped card with first real result",
			action: Action{
				Kind:    ActionShowThreads,
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "attach result can replace stamped selection card",
			action: Action{
				Kind:    ActionAttachInstance,
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "use thread result can replace stamped selection card",
			action: Action{
				Kind:    ActionUseThread,
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "bare upgrade root page can replace stamped command card",
			action: Action{
				Kind:    ActionUpgradeCommand,
				Text:    "/upgrade",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "upgrade track page can replace stamped command card",
			action: Action{
				Kind:    ActionUpgradeCommand,
				Text:    "/upgrade track",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "upgrade latest stays append-only",
			action: Action{
				Kind:    ActionUpgradeCommand,
				Text:    "/upgrade latest",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
		{
			name: "bare debug root page can replace stamped command card",
			action: Action{
				Kind:    ActionDebugCommand,
				Text:    "/debug",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "bare cron root page can replace stamped command card",
			action: Action{
				Kind:    ActionCronCommand,
				Text:    "/cron",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "cron reload stays append-only",
			action: Action{
				Kind:    ActionCronCommand,
				Text:    "/cron reload",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
		{
			name: "bare vscode migrate root page can replace stamped command card",
			action: Action{
				Kind:    ActionVSCodeMigrateCommand,
				Text:    "/vscode-migrate",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
		},
		{
			name: "vscode migrate owner flow result can replace stamped command card",
			action: Action{
				Kind:    ActionVSCodeMigrate,
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: true,
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
			name: "bare upgrade no longer uses bare continuation",
			action: Action{
				Kind:    ActionUpgradeCommand,
				Text:    "/upgrade",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
		{
			name: "bare debug no longer uses bare continuation",
			action: Action{
				Kind:    ActionDebugCommand,
				Text:    "/debug",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
		},
		{
			name: "bare cron no longer uses bare continuation",
			action: Action{
				Kind:    ActionCronCommand,
				Text:    "/cron",
				Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: false,
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
			name: "use no longer falls back to submission anchor",
			action: Action{
				Kind:    ActionShowThreads,
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

func TestResolveFeishuFrontstageActionContractFollowupPolicy(t *testing.T) {
	help := ResolveFeishuFrontstageActionContract(Action{Kind: ActionShowCommandHelp})
	if help.FollowupPolicy.Empty() {
		t.Fatalf("expected help action to define followup policy: %#v", help)
	}
	if !help.FollowupPolicy.ShouldDropHandoffClass(string(FeishuFollowupHandoffClassNotice)) {
		t.Fatalf("expected help action to drop notice followups: %#v", help.FollowupPolicy)
	}
	if !help.FollowupPolicy.ShouldDropHandoffClass(string(FeishuFollowupHandoffClassThreadSelection)) {
		t.Fatalf("expected help action to drop thread-selection followups: %#v", help.FollowupPolicy)
	}

	attach := ResolveFeishuFrontstageActionContract(Action{Kind: ActionAttachInstance})
	if attach.FollowupPolicy.Empty() {
		t.Fatalf("expected attach action to define followup policy: %#v", attach)
	}
	if attach.FollowupPolicy.ShouldDropHandoffClass(string(FeishuFollowupHandoffClassNotice)) {
		t.Fatalf("expected attach action to keep generic notices: %#v", attach.FollowupPolicy)
	}
	if !attach.FollowupPolicy.ShouldDropHandoffClass(string(FeishuFollowupHandoffClassThreadSelection)) {
		t.Fatalf("expected attach action to drop thread-selection followups: %#v", attach.FollowupPolicy)
	}
}

func TestFeishuFollowupPolicyKeepClassOverridesDrop(t *testing.T) {
	policy := FeishuFollowupPolicy{
		DropClasses: []FeishuFollowupHandoffClass{
			FeishuFollowupHandoffClassNotice,
		},
		KeepClasses: []FeishuFollowupHandoffClass{
			FeishuFollowupHandoffClassNotice,
		},
	}.Normalized()
	if policy.ShouldDropHandoffClass(string(FeishuFollowupHandoffClassNotice)) {
		t.Fatalf("expected keep override to win over drop: %#v", policy)
	}
}
