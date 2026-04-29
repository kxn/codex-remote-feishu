package control

import "strings"

type FeishuCommandBindingKind string

const (
	FeishuCommandBindingConfigFlow       FeishuCommandBindingKind = "config_flow"
	FeishuCommandBindingWorkspaceSession FeishuCommandBindingKind = "workspace_session"
	FeishuCommandBindingInlinePage       FeishuCommandBindingKind = "inline_page"
	FeishuCommandBindingTerminalPage     FeishuCommandBindingKind = "terminal_page"
	FeishuCommandBindingDaemonCommand    FeishuCommandBindingKind = "daemon_command"
	FeishuCommandBindingOwnerEntry       FeishuCommandBindingKind = "owner_entry"
)

type FeishuCommandBinding struct {
	FamilyID                    string
	Kind                        FeishuCommandBindingKind
	LauncherDisposition         FeishuFrontstageLauncherDisposition
	DirectDaemonCommand         DaemonCommandKind
	PropagateCardActionToDaemon bool
	ContinuationDaemonCommand   DaemonCommandKind
	FollowupPolicy              FeishuFollowupPolicy
	intentBuilder               func(Action) (*FeishuUIIntent, bool)
}

var feishuCommandBindingsByFamilyID = buildFeishuCommandBindings()

func ResolveFeishuCommandBindingByFamilyID(familyID string) (FeishuCommandBinding, bool) {
	binding, ok := feishuCommandBindingsByFamilyID[strings.TrimSpace(familyID)]
	return binding, ok
}

func ResolveFeishuCommandBindingFromAction(action Action) (FeishuCommandBinding, bool) {
	if resolved, ok := ResolveFeishuActionCatalog(CatalogContext{}, action); ok {
		if binding, ok := ResolveFeishuCommandBindingByFamilyID(resolved.FamilyID); ok {
			return binding, true
		}
	}
	return FeishuCommandBinding{}, false
}

func (b FeishuCommandBinding) IntentFromAction(action Action) (*FeishuUIIntent, bool) {
	if b.intentBuilder == nil {
		return nil, false
	}
	return b.intentBuilder(action)
}

func buildFeishuCommandBindings() map[string]FeishuCommandBinding {
	bindings := make(map[string]FeishuCommandBinding)

	for _, def := range FeishuConfigFlowDefinitions() {
		bindings[def.CommandID] = FeishuCommandBinding{
			FamilyID:            def.CommandID,
			Kind:                FeishuCommandBindingConfigFlow,
			LauncherDisposition: FeishuFrontstageLauncherKeep,
			intentBuilder:       bareInlineIntentBuilder(def.IntentKind, def.BareCommand, false),
		}
	}

	for _, familyID := range []string{
		FeishuCommandList,
		FeishuCommandUse,
		FeishuCommandUseAll,
		FeishuCommandNew,
		FeishuCommandFollow,
	} {
		flow, ok := resolveFeishuWorkspaceSessionFlow(familyID)
		if !ok {
			continue
		}
		binding := FeishuCommandBinding{
			FamilyID: familyID,
			Kind:     FeishuCommandBindingWorkspaceSession,
		}
		if flow.IntentKind != "" {
			binding.intentBuilder = workspaceSessionIntentBuilder(flow)
		}
		if policy, ok := followupPolicyForFamilyID(familyID); ok {
			binding.FollowupPolicy = policy
		}
		bindings[familyID] = binding
	}

	for _, familyID := range []string{
		FeishuCommandWorkspaceDetach,
		FeishuCommandCompact,
		FeishuCommandSteerAll,
		FeishuCommandReview,
		FeishuCommandPatch,
	} {
		bindings[familyID] = FeishuCommandBinding{
			FamilyID: familyID,
			Kind:     FeishuCommandBindingOwnerEntry,
		}
	}

	bindings[FeishuCommandWorkspace] = FeishuCommandBinding{
		FamilyID:      FeishuCommandWorkspace,
		Kind:          FeishuCommandBindingInlinePage,
		intentBuilder: bareInlineIntentBuilder(FeishuUIIntentShowWorkspaceRoot, "/workspace", true),
	}
	bindings[FeishuCommandWorkspaceList] = FeishuCommandBinding{
		FamilyID:      FeishuCommandWorkspaceList,
		Kind:          FeishuCommandBindingInlinePage,
		intentBuilder: fixedInlineIntentBuilder(FeishuUIIntentShowWorkspaceList, nil),
	}
	bindings[FeishuCommandWorkspaceNew] = FeishuCommandBinding{
		FamilyID:      FeishuCommandWorkspaceNew,
		Kind:          FeishuCommandBindingInlinePage,
		intentBuilder: bareInlineIntentBuilder(FeishuUIIntentShowWorkspaceNew, "/workspace new", true),
	}
	bindings[FeishuCommandWorkspaceNewDir] = FeishuCommandBinding{
		FamilyID:      FeishuCommandWorkspaceNewDir,
		Kind:          FeishuCommandBindingInlinePage,
		intentBuilder: fixedInlineIntentBuilder(FeishuUIIntentShowWorkspaceNewDir, nil),
	}
	bindings[FeishuCommandWorkspaceNewGit] = FeishuCommandBinding{
		FamilyID:      FeishuCommandWorkspaceNewGit,
		Kind:          FeishuCommandBindingInlinePage,
		intentBuilder: fixedInlineIntentBuilder(FeishuUIIntentShowWorkspaceNewGit, nil),
	}
	bindings[FeishuCommandWorkspaceNewWorktree] = FeishuCommandBinding{
		FamilyID:      FeishuCommandWorkspaceNewWorktree,
		Kind:          FeishuCommandBindingInlinePage,
		intentBuilder: fixedInlineIntentBuilder(FeishuUIIntentShowWorkspaceNewWorktree, nil),
	}
	bindings[FeishuCommandMenu] = FeishuCommandBinding{
		FamilyID:            FeishuCommandMenu,
		Kind:                FeishuCommandBindingInlinePage,
		LauncherDisposition: FeishuFrontstageLauncherKeep,
		intentBuilder:       fixedInlineIntentBuilder(FeishuUIIntentShowCommandMenu, nil),
	}
	bindings[FeishuCommandHistory] = FeishuCommandBinding{
		FamilyID:      FeishuCommandHistory,
		Kind:          FeishuCommandBindingInlinePage,
		intentBuilder: bareInlineIntentBuilder(FeishuUIIntentShowHistory, "/history", false),
	}
	bindings[FeishuCommandSendFile] = FeishuCommandBinding{
		FamilyID:      FeishuCommandSendFile,
		Kind:          FeishuCommandBindingInlinePage,
		intentBuilder: fixedInlineIntentBuilder(FeishuUIIntentOpenSendFilePicker, nil),
	}

	bindings[FeishuCommandHelp] = terminalPageBinding(FeishuCommandHelp)
	bindings[FeishuCommandStatus] = terminalPageBinding(FeishuCommandStatus)
	bindings[FeishuCommandStop] = ownerEntryBindingWithPolicy(FeishuCommandStop)
	bindings[FeishuCommandDetach] = ownerEntryBindingWithPolicy(FeishuCommandDetach)

	bindings[FeishuCommandDebug] = daemonCommandBinding(FeishuCommandDebug, DaemonCommandDebug, false)
	bindings[FeishuCommandCron] = daemonCommandBinding(FeishuCommandCron, DaemonCommandCron, false)
	bindings[FeishuCommandRestart] = daemonCommandBinding(FeishuCommandRestart, DaemonCommandRestart, true)
	bindings[FeishuCommandUpgrade] = daemonCommandBinding(FeishuCommandUpgrade, DaemonCommandUpgrade, true)
	bindings[FeishuCommandVSCodeMigrate] = daemonCommandBinding(FeishuCommandVSCodeMigrate, DaemonCommandVSCodeMigrateCommand, true)

	return bindings
}

func daemonCommandBinding(familyID string, daemonCommand DaemonCommandKind, propagateCardAction bool) FeishuCommandBinding {
	return FeishuCommandBinding{
		FamilyID:                    familyID,
		Kind:                        FeishuCommandBindingDaemonCommand,
		DirectDaemonCommand:         daemonCommand,
		PropagateCardActionToDaemon: propagateCardAction,
		ContinuationDaemonCommand:   daemonCommand,
	}
}

func terminalPageBinding(familyID string) FeishuCommandBinding {
	policy, _ := followupPolicyForFamilyID(familyID)
	return FeishuCommandBinding{
		FamilyID:            familyID,
		Kind:                FeishuCommandBindingTerminalPage,
		LauncherDisposition: FeishuFrontstageLauncherEnterTerminal,
		FollowupPolicy:      policy,
	}
}

func ownerEntryBindingWithPolicy(familyID string) FeishuCommandBinding {
	policy, _ := followupPolicyForFamilyID(familyID)
	return FeishuCommandBinding{
		FamilyID:       familyID,
		Kind:           FeishuCommandBindingOwnerEntry,
		FollowupPolicy: policy,
	}
}

func followupPolicyForFamilyID(familyID string) (FeishuFollowupPolicy, bool) {
	switch strings.TrimSpace(familyID) {
	case FeishuCommandHelp, FeishuCommandStatus, FeishuCommandStop, FeishuCommandNew, FeishuCommandFollow, FeishuCommandDetach:
		return FeishuFollowupPolicy{
			DropClasses: []FeishuFollowupHandoffClass{
				FeishuFollowupHandoffClassNotice,
				FeishuFollowupHandoffClassThreadSelection,
			},
		}.Normalized(), true
	default:
		return FeishuFollowupPolicy{}, false
	}
}

func baseInlineIntent(action Action, kind FeishuUIIntentKind) *FeishuUIIntent {
	return &FeishuUIIntent{
		Kind:            kind,
		RawText:         action.Text,
		SourceMessageID: action.MessageID,
		Inline:          action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != "",
	}
}

func fixedInlineIntentBuilder(kind FeishuUIIntentKind, fill func(*FeishuUIIntent, Action)) func(Action) (*FeishuUIIntent, bool) {
	return func(action Action) (*FeishuUIIntent, bool) {
		intent := baseInlineIntent(action, kind)
		if fill != nil {
			fill(intent, action)
		}
		return intent, true
	}
}

func bareInlineIntentBuilder(kind FeishuUIIntentKind, bareCommand string, allowEmpty bool) func(Action) (*FeishuUIIntent, bool) {
	return func(action Action) (*FeishuUIIntent, bool) {
		text := strings.TrimSpace(action.Text)
		if !isBareInlineCommand(text, bareCommand) && !(allowEmpty && text == "") {
			return nil, false
		}
		return baseInlineIntent(action, kind), true
	}
}

func workspaceSessionIntentBuilder(flow FeishuWorkspaceSessionFlow) func(Action) (*FeishuUIIntent, bool) {
	return func(action Action) (*FeishuUIIntent, bool) {
		intent := baseInlineIntent(action, flow.IntentKind)
		switch flow.IntentKind {
		case FeishuUIIntentShowList:
			return intent, true
		case FeishuUIIntentShowThreads, FeishuUIIntentShowAllThreads:
			intent.ViewMode = action.ViewMode
			intent.Page = action.Page
			return intent, true
		default:
			return nil, false
		}
	}
}
