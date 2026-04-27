package control

import "testing"

func TestResolveFeishuFrontstageActionContractInlineViewPolicy(t *testing.T) {
	contract := ResolveFeishuFrontstageActionContract(Action{
		Kind: ActionShowCommandMenu,
		Text: "/menu send_settings",
	})
	if contract.CurrentCardMode != FeishuFrontstageCurrentCardInlineView {
		t.Fatalf("expected inline_view contract for menu navigation, got %#v", contract)
	}
	if !contract.RequiresDaemonFreshness || contract.DaemonFreshness != FeishuUIInlineReplaceFreshnessDaemonLifecycle {
		t.Fatalf("unexpected daemon freshness policy: %#v", contract)
	}
	if contract.RequiresViewSession || contract.ViewSessionStrategy != FeishuUIInlineReplaceViewSessionSurfaceState {
		t.Fatalf("unexpected view/session policy: %#v", contract)
	}

	if contract := ResolveFeishuFrontstageActionContract(Action{
		Kind: ActionModeCommand,
		Text: "/mode vscode",
	}); contract.CurrentCardMode != FeishuFrontstageCurrentCardInlineView {
		t.Fatalf("expected parameter apply to follow inline_view contract, got %#v", contract)
	}
}

func TestResolveFeishuFrontstageActionContractInlineViewActionSet(t *testing.T) {
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
			name:   "workspace new worktree handoff",
			action: Action{Kind: ActionWorkspaceNewWorktree, Text: "/workspace new worktree"},
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
			name:   "thread selection pagination",
			action: Action{Kind: ActionThreadSelectionPage, ViewMode: string(FeishuThreadSelectionVSCodeAll), Cursor: 7},
			want:   true,
		},
		{
			name:   "path picker navigation",
			action: Action{Kind: ActionPathPickerEnter, PickerID: "picker-1", PickerEntry: "subdir"},
			want:   true,
		},
		{
			name:   "path picker pagination",
			action: Action{Kind: ActionPathPickerPage, PickerID: "picker-1", FieldName: "path_picker_file", Cursor: 7},
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
			action: Action{Kind: ActionRespondRequest, Request: &ActionRequestResponse{RequestID: "req-1", RequestOptionID: "step_next"}},
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
			contract := ResolveFeishuFrontstageActionContract(tt.action)
			got := contract.CurrentCardMode == FeishuFrontstageCurrentCardInlineView
			if got != tt.want {
				t.Fatalf("ResolveFeishuFrontstageActionContract(%#v).CurrentCardMode == inline_view = %v, want %v (contract=%#v)", tt.action, got, tt.want, contract)
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
		Kind:    ActionRespondRequest,
		Request: &ActionRequestResponse{RequestID: "req-1", RequestOptionID: "step_next"},
		Inbound: &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
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
			name: "upgrade codex stays append-only",
			action: Action{
				Kind:    ActionUpgradeCommand,
				Text:    "/upgrade codex",
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

func TestResolveFeishuFrontstageActionContractLauncherDisposition(t *testing.T) {
	tests := []struct {
		name string
		kind ActionKind
		want FeishuFrontstageLauncherDisposition
	}{
		{name: "menu stays launcher", kind: ActionShowCommandMenu, want: FeishuFrontstageLauncherKeep},
		{name: "mode stays launcher", kind: ActionModeCommand, want: FeishuFrontstageLauncherKeep},
		{name: "autowhip stays launcher", kind: ActionAutoWhipCommand, want: FeishuFrontstageLauncherKeep},
		{name: "autocontinue stays launcher", kind: ActionAutoContinueCommand, want: FeishuFrontstageLauncherKeep},
		{name: "reasoning stays launcher", kind: ActionReasoningCommand, want: FeishuFrontstageLauncherKeep},
		{name: "access stays launcher", kind: ActionAccessCommand, want: FeishuFrontstageLauncherKeep},
		{name: "plan stays launcher", kind: ActionPlanCommand, want: FeishuFrontstageLauncherKeep},
		{name: "model stays launcher", kind: ActionModelCommand, want: FeishuFrontstageLauncherKeep},
		{name: "verbose stays launcher", kind: ActionVerboseCommand, want: FeishuFrontstageLauncherKeep},
		{name: "help enters terminal", kind: ActionShowCommandHelp, want: FeishuFrontstageLauncherEnterTerminal},
		{name: "status enters terminal", kind: ActionStatus, want: FeishuFrontstageLauncherEnterTerminal},
		{name: "stop enters owner", kind: ActionStop, want: FeishuFrontstageLauncherEnterOwner},
		{name: "new enters owner", kind: ActionNewThread, want: FeishuFrontstageLauncherEnterOwner},
		{name: "follow enters owner", kind: ActionFollowLocal, want: FeishuFrontstageLauncherEnterOwner},
		{name: "detach enters owner", kind: ActionDetach, want: FeishuFrontstageLauncherEnterOwner},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveFeishuFrontstageActionContract(Action{Kind: tt.kind}).LauncherDisposition
			if got != tt.want {
				t.Fatalf("LauncherDisposition(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
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
