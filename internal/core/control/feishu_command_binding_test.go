package control

import "testing"

func TestResolveFeishuCommandBindingFromActionClassifiesEntryKinds(t *testing.T) {
	tests := []struct {
		name              string
		action            Action
		wantFamily        string
		wantKind          FeishuCommandBindingKind
		wantLauncher      FeishuFrontstageLauncherDisposition
		wantDirectDaemon  DaemonCommandKind
		wantContinuation  DaemonCommandKind
		wantHasFollowup   bool
		wantPropagateCard bool
	}{
		{
			name:         "config flow",
			action:       Action{Kind: ActionModeCommand, Text: "/mode"},
			wantFamily:   FeishuCommandMode,
			wantKind:     FeishuCommandBindingConfigFlow,
			wantLauncher: FeishuFrontstageLauncherKeep,
		},
		{
			name:       "inline page",
			action:     Action{Kind: ActionWorkspaceRoot, Text: "/workspace"},
			wantFamily: FeishuCommandWorkspace,
			wantKind:   FeishuCommandBindingInlinePage,
		},
		{
			name:       "workspace session provenance override",
			action:     Action{Kind: ActionShowThreads, CatalogFamilyID: FeishuCommandUseAll, CatalogVariantID: "useall.default"},
			wantFamily: FeishuCommandUseAll,
			wantKind:   FeishuCommandBindingWorkspaceSession,
		},
		{
			name:            "terminal page",
			action:          Action{Kind: ActionShowCommandHelp, Text: "/help"},
			wantFamily:      FeishuCommandHelp,
			wantKind:        FeishuCommandBindingTerminalPage,
			wantLauncher:    FeishuFrontstageLauncherEnterTerminal,
			wantHasFollowup: true,
		},
		{
			name:              "daemon command",
			action:            Action{Kind: ActionUpgradeCommand, Text: "/upgrade"},
			wantFamily:        FeishuCommandUpgrade,
			wantKind:          FeishuCommandBindingDaemonCommand,
			wantDirectDaemon:  DaemonCommandUpgrade,
			wantContinuation:  DaemonCommandUpgrade,
			wantPropagateCard: true,
		},
		{
			name:       "owner entry",
			action:     Action{Kind: ActionTurnPatchCommand, Text: "/bendtomywill"},
			wantFamily: FeishuCommandPatch,
			wantKind:   FeishuCommandBindingOwnerEntry,
		},
		{
			name:       "review owner entry",
			action:     Action{Kind: ActionReviewCommand, Text: "/review uncommitted"},
			wantFamily: FeishuCommandReview,
			wantKind:   FeishuCommandBindingOwnerEntry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding, ok := ResolveFeishuCommandBindingFromAction(tt.action)
			if !ok {
				t.Fatalf("expected binding for %#v", tt.action)
			}
			if binding.FamilyID != tt.wantFamily {
				t.Fatalf("FamilyID = %q, want %q", binding.FamilyID, tt.wantFamily)
			}
			if binding.Kind != tt.wantKind {
				t.Fatalf("Kind = %q, want %q", binding.Kind, tt.wantKind)
			}
			if binding.LauncherDisposition != tt.wantLauncher {
				t.Fatalf("LauncherDisposition = %q, want %q", binding.LauncherDisposition, tt.wantLauncher)
			}
			if binding.DirectDaemonCommand != tt.wantDirectDaemon {
				t.Fatalf("DirectDaemonCommand = %q, want %q", binding.DirectDaemonCommand, tt.wantDirectDaemon)
			}
			if binding.ContinuationDaemonCommand != tt.wantContinuation {
				t.Fatalf("ContinuationDaemonCommand = %q, want %q", binding.ContinuationDaemonCommand, tt.wantContinuation)
			}
			if binding.PropagateCardActionToDaemon != tt.wantPropagateCard {
				t.Fatalf("PropagateCardActionToDaemon = %v, want %v", binding.PropagateCardActionToDaemon, tt.wantPropagateCard)
			}
			if got := !binding.FollowupPolicy.Empty(); got != tt.wantHasFollowup {
				t.Fatalf("has followup policy = %v, want %v", got, tt.wantHasFollowup)
			}
		})
	}
}

func TestResolveFeishuCommandBindingIntentBuilder(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   FeishuUIIntentKind
		ok     bool
	}{
		{name: "history bare command", action: Action{Kind: ActionShowHistory, Text: "/history"}, want: FeishuUIIntentShowHistory, ok: true},
		{name: "history non-bare stays product owned", action: Action{Kind: ActionShowHistory, Text: "/history latest"}, ok: false},
		{name: "sendfile opens picker", action: Action{Kind: ActionSendFile, Text: "/sendfile"}, want: FeishuUIIntentOpenSendFilePicker, ok: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding, ok := ResolveFeishuCommandBindingFromAction(tt.action)
			if !ok {
				t.Fatalf("expected binding for %#v", tt.action)
			}
			intent, got := binding.IntentFromAction(tt.action)
			if got != tt.ok {
				t.Fatalf("IntentFromAction(%#v) ok = %v, want %v", tt.action, got, tt.ok)
			}
			if !tt.ok {
				if intent != nil {
					t.Fatalf("expected nil intent, got %#v", intent)
				}
				return
			}
			if intent == nil || intent.Kind != tt.want {
				t.Fatalf("intent = %#v, want kind %q", intent, tt.want)
			}
		})
	}
}
