package daemon

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishubotidentity"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/feishuidentity"
)

type feishuBotIdentityTransitionKind uint8

const (
	feishuBotIdentityUnchanged feishuBotIdentityTransitionKind = iota
	feishuBotIdentityBootstrap
	feishuBotIdentityReplace
	feishuBotIdentityRemove
)

type feishuBotIdentityTransition struct {
	Kind       feishuBotIdentityTransitionKind
	GatewayID  string
	Current    feishubotidentity.Record
	DesiredApp string
}

func (a *App) configureFeishuBotIdentityStateLocked(stateDir string) {
	path := feishubotidentity.StatePath(stateDir)
	a.feishuBotIdentityState.persistedStoreRuntimeState = loadPersistedStore("Feishu bot identity", path, feishubotidentity.LoadStore)
}

func (a *App) planFeishuBotIdentityTransition(gatewayID, desiredAppID string) (feishuBotIdentityTransition, error) {
	gatewayID = canonicalGatewayID(gatewayID)
	desiredAppID = strings.TrimSpace(desiredAppID)
	transition := feishuBotIdentityTransition{GatewayID: gatewayID, DesiredApp: desiredAppID}
	if gatewayID == "" {
		return transition, fmt.Errorf("feishu bot identity requires gateway id")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if err := persistedStateWriteError("Feishu bot identity", a.feishuBotIdentityState.persistedStoreRuntimeState); err != nil {
		return transition, err
	}
	current, exists := a.feishuBotIdentityState.store.Get(gatewayID)
	transition.Current = current
	pending := exists && current.Pending != nil
	switch {
	case !exists && desiredAppID == "":
		transition.Kind = feishuBotIdentityUnchanged
	case !exists:
		transition.Kind = feishuBotIdentityBootstrap
	case pending && desiredAppID == "":
		transition.Kind = feishuBotIdentityRemove
	case pending:
		transition.Kind = feishuBotIdentityReplace
	case desiredAppID == "":
		transition.Kind = feishuBotIdentityRemove
	case current.AppID == desiredAppID:
		transition.Kind = feishuBotIdentityUnchanged
	default:
		transition.Kind = feishuBotIdentityReplace
	}
	if transition.Kind == feishuBotIdentityReplace || transition.Kind == feishuBotIdentityRemove {
		if err := a.ensureFeishuIdentityCleanupStoresWritableLocked(); err != nil {
			return transition, err
		}
	}
	return transition, nil
}

func (a *App) ensureFeishuIdentityCleanupStoresWritableLocked() error {
	if err := persistedStateWriteError("surface resume", a.surfaceResumeRuntime.persistedStoreRuntimeState); err != nil {
		return err
	}
	if err := persistedStateWriteError("bot capability settings", a.botCapabilitySettingsState.persistedStoreRuntimeState); err != nil {
		return err
	}
	if err := persistedStateWriteError("Feishu room", a.feishuRoomState.persistedStoreRuntimeState); err != nil {
		return err
	}
	return nil
}

func persistedStateWriteError[T persistedStore](label string, runtime persistedStoreRuntimeState[T]) error {
	if runtime.writable() {
		return nil
	}
	if runtime.diagnosticErr != nil {
		return fmt.Errorf("%s state is not writable: %w", label, runtime.diagnosticErr)
	}
	return fmt.Errorf("%s state is not writable", label)
}

func (a *App) commitFeishuBotIdentityTransition(transition feishuBotIdentityTransition) error {
	if transition.Kind == feishuBotIdentityUnchanged {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	current, exists := a.feishuBotIdentityState.store.Get(transition.GatewayID)
	if transition.Kind == feishuBotIdentityBootstrap {
		if exists {
			return fmt.Errorf("feishu bot identity changed while applying gateway %s", transition.GatewayID)
		}
		return a.feishuBotIdentityState.store.Put(feishubotidentity.Record{
			GatewayID:  transition.GatewayID,
			AppID:      transition.DesiredApp,
			Generation: 1,
			UpdatedAt:  time.Now().UTC(),
		})
	}
	if !exists || !sameFeishuBotIdentityRecord(current, transition.Current) {
		return fmt.Errorf("feishu bot identity changed while applying gateway %s", transition.GatewayID)
	}

	now := time.Now().UTC()
	if err := a.feishuBotIdentityState.store.Put(feishubotidentity.Record{
		GatewayID:  current.GatewayID,
		AppID:      current.AppID,
		Generation: current.Generation,
		UpdatedAt:  now,
		Pending: &feishubotidentity.PendingTransition{
			DesiredAppID: transition.DesiredApp,
			StartedAt:    feishuBotIdentityPendingStartedAt(current, now),
		},
	}); err != nil {
		return fmt.Errorf("mark Feishu bot identity transition pending: %w", err)
	}

	removedSurfaceIDs, err := a.purgeFeishuGatewayDurableStateLocked(transition.GatewayID)
	if err != nil {
		return err
	}
	if transition.Kind == feishuBotIdentityRemove {
		if err := a.feishuBotIdentityState.store.Delete(transition.GatewayID); err != nil {
			return fmt.Errorf("commit removed Feishu bot identity: %w", err)
		}
	} else {
		if err := a.feishuBotIdentityState.store.Put(feishubotidentity.Record{
			GatewayID:  transition.GatewayID,
			AppID:      transition.DesiredApp,
			Generation: transition.Current.Generation + 1,
			UpdatedAt:  now,
		}); err != nil {
			return fmt.Errorf("commit replacement Feishu bot identity: %w", err)
		}
	}

	removedSurfaceIDs = append(removedSurfaceIDs, a.service.PurgeGatewayIdentityState(transition.GatewayID)...)
	a.purgeFeishuGatewayRuntimeStateLocked(transition.GatewayID, removedSurfaceIDs)
	return nil
}

func sameFeishuBotIdentityRecord(left, right feishubotidentity.Record) bool {
	left, leftOK := feishubotidentity.NormalizeRecord(left)
	right, rightOK := feishubotidentity.NormalizeRecord(right)
	if !leftOK || !rightOK {
		return false
	}
	if left.GatewayID != right.GatewayID || left.AppID != right.AppID || left.Generation != right.Generation || !left.UpdatedAt.Equal(right.UpdatedAt) {
		return false
	}
	if left.Pending == nil || right.Pending == nil {
		return left.Pending == nil && right.Pending == nil
	}
	return left.Pending.DesiredAppID == right.Pending.DesiredAppID && left.Pending.StartedAt.Equal(right.Pending.StartedAt)
}

func feishuBotIdentityPendingStartedAt(record feishubotidentity.Record, fallback time.Time) time.Time {
	if record.Pending != nil && !record.Pending.StartedAt.IsZero() {
		return record.Pending.StartedAt
	}
	return fallback
}

func (a *App) purgeFeishuGatewayDurableStateLocked(gatewayID string) ([]string, error) {
	resumeEntries := a.surfaceResumeRuntime.store.Entries()
	removedSurfaceIDs := make([]string, 0)
	for surfaceID, entry := range resumeEntries {
		if surfaceResumeEntryBelongsToGateway(entry, gatewayID) {
			removedSurfaceIDs = append(removedSurfaceIDs, surfaceID)
			delete(resumeEntries, surfaceID)
		}
	}
	if err := a.surfaceResumeRuntime.store.ReplaceAll(resumeEntries); err != nil {
		return nil, fmt.Errorf("clear surface resume state for gateway %s: %w", gatewayID, err)
	}

	settings := a.botCapabilitySettingsState.store.Entries()
	settingsRecords := make([]state.BotCapabilitySettingsRecord, 0, len(settings))
	for _, record := range settings {
		if state.BotCapabilitySettingsKey(record.GatewayID) != state.BotCapabilitySettingsKey(gatewayID) {
			settingsRecords = append(settingsRecords, record)
		}
	}
	if err := a.botCapabilitySettingsState.store.ReplaceAll(settingsRecords); err != nil {
		return nil, fmt.Errorf("clear bot capability settings for gateway %s: %w", gatewayID, err)
	}

	rooms := a.feishuRoomState.store.Entries()
	roomRecords := make([]state.FeishuRoomStateRecord, 0, len(rooms))
	for _, record := range rooms {
		if canonicalGatewayID(record.PrimaryGatewayID) == gatewayID {
			record.PrimaryGatewayID = ""
			record.PrimaryUpdatedBy = ""
			record.PrimaryUpdatedAt = time.Time{}
		}
		if record.WorkspaceKey != "" || record.WorkspaceResetGeneration != 0 || record.PrimaryGatewayID != "" {
			roomRecords = append(roomRecords, record)
		}
	}
	if err := a.feishuRoomState.store.ReplaceAll(roomRecords); err != nil {
		return nil, fmt.Errorf("clear room primary state for gateway %s: %w", gatewayID, err)
	}
	sort.Strings(removedSurfaceIDs)
	return removedSurfaceIDs, nil
}

func surfaceResumeEntryBelongsToGateway(entry surfaceresume.Entry, gatewayID string) bool {
	if canonicalGatewayID(entry.GatewayID) == gatewayID {
		return true
	}
	ref, ok := feishuidentity.ParseSurfaceRef(entry.SurfaceSessionID)
	return ok && canonicalGatewayID(ref.GatewayID) == gatewayID
}

func (a *App) purgeFeishuGatewayRuntimeStateLocked(gatewayID string, surfaceIDs []string) {
	surfaceSet := make(map[string]bool, len(surfaceIDs))
	addSurface := func(surfaceID string) {
		surfaceID = strings.TrimSpace(surfaceID)
		if surfaceID == "" {
			return
		}
		ref, ok := feishuidentity.ParseSurfaceRef(surfaceID)
		if ok && canonicalGatewayID(ref.GatewayID) == gatewayID {
			surfaceSet[surfaceID] = true
		}
	}
	for _, surfaceID := range surfaceIDs {
		addSurface(surfaceID)
	}
	for surfaceID, target := range a.feishuRuntime.timeSensitive {
		if surfaceSet[surfaceID] || canonicalGatewayID(target.GatewayID) == gatewayID {
			delete(a.feishuRuntime.timeSensitive, surfaceID)
			surfaceSet[surfaceID] = true
		}
	}
	for surfaceID := range a.surfaceResumeRuntime.recovery {
		addSurface(surfaceID)
	}
	for surfaceID := range a.surfaceResumeRuntime.groupOnDemandContinuations {
		addSurface(surfaceID)
	}
	for surfaceID := range a.surfaceResumeRuntime.vscodeMigrationFlows {
		addSurface(surfaceID)
	}
	for surfaceID := range a.surfaceResumeRuntime.vscodeResumeNotices {
		addSurface(surfaceID)
	}
	for surfaceID := range a.pendingGlobalRuntimeNotices {
		addSurface(surfaceID)
	}
	for surfaceID := range a.recentGlobalRuntimeNotices {
		addSurface(surfaceID)
	}
	for _, pending := range a.pendingThreadHistoryReads {
		addSurface(pending.SurfaceSessionID)
	}
	for _, pending := range a.pendingMCPOAuthLogins {
		addSurface(pending.SurfaceSessionID)
	}
	for _, surfaceID := range a.surfaceResumeRuntime.workspaceContextRoots {
		addSurface(surfaceID)
	}
	for key := range a.gitWorkspaceImports {
		surfaceID, _, _ := strings.Cut(key, "::")
		addSurface(surfaceID)
	}
	for key := range a.gitWorkspaceWorktrees {
		surfaceID, _, _ := strings.Cut(key, "::")
		addSurface(surfaceID)
	}
	for key := range a.feishuRuntime.attentionRequests {
		for surfaceID := range surfaceSet {
			if strings.HasPrefix(key, surfaceID+"::") {
				delete(a.feishuRuntime.attentionRequests, key)
				break
			}
		}
	}
	turnPatchResumeEvents := a.purgeFeishuGatewayTurnPatchRuntimeLocked(gatewayID, surfaceSet, addSurface)
	for surfaceID := range surfaceSet {
		delete(a.surfaceResumeRuntime.recovery, surfaceID)
		delete(a.surfaceResumeRuntime.groupOnDemandContinuations, surfaceID)
		delete(a.surfaceResumeRuntime.vscodeMigrationFlows, surfaceID)
		delete(a.surfaceResumeRuntime.vscodeResumeNotices, surfaceID)
		delete(a.pendingGlobalRuntimeNotices, surfaceID)
		delete(a.recentGlobalRuntimeNotices, surfaceID)
	}
	for key, pending := range a.pendingThreadHistoryReads {
		if surfaceSet[strings.TrimSpace(pending.SurfaceSessionID)] {
			delete(a.pendingThreadHistoryReads, key)
		}
	}
	for key, pending := range a.pendingMCPOAuthLogins {
		if surfaceSet[strings.TrimSpace(pending.SurfaceSessionID)] {
			delete(a.pendingMCPOAuthLogins, key)
		}
	}
	for workspaceRoot, surfaceID := range a.surfaceResumeRuntime.workspaceContextRoots {
		if surfaceSet[strings.TrimSpace(surfaceID)] {
			delete(a.surfaceResumeRuntime.workspaceContextRoots, workspaceRoot)
		}
	}
	for key, runtime := range a.gitWorkspaceImports {
		surfaceID, _, _ := strings.Cut(key, "::")
		if !surfaceSet[strings.TrimSpace(surfaceID)] {
			continue
		}
		if runtime != nil && runtime.cancel != nil {
			runtime.cancelled = true
			runtime.cancel()
		}
		delete(a.gitWorkspaceImports, key)
	}
	for key, runtime := range a.gitWorkspaceWorktrees {
		surfaceID, _, _ := strings.Cut(key, "::")
		if !surfaceSet[strings.TrimSpace(surfaceID)] {
			continue
		}
		if runtime != nil && runtime.cancel != nil {
			runtime.cancelled = true
			runtime.cancel()
		}
		delete(a.gitWorkspaceWorktrees, key)
	}

	a.feishuRuntime.permissionMu.Lock()
	delete(a.feishuRuntime.permissionGaps, gatewayID)
	delete(a.feishuRuntime.primaryPermissionCache, gatewayID)
	a.feishuRuntime.permissionMu.Unlock()
	a.feishuChatAdmins.purgeGateway(gatewayID)
	if len(turnPatchResumeEvents) != 0 {
		a.handleUIEventsLocked(context.Background(), turnPatchResumeEvents)
	}
}

func (a *App) purgeFeishuGatewayTurnPatchRuntimeLocked(gatewayID string, surfaceSet map[string]bool, addSurface func(string)) []eventcontract.Event {
	removedFlowIDs := map[string]bool{}
	for requestID, flow := range a.turnPatchRuntime.ActiveFlows {
		if flow == nil {
			delete(a.turnPatchRuntime.ActiveFlows, requestID)
			continue
		}
		surfaceID := strings.TrimSpace(flow.SurfaceSessionID)
		if !surfaceSet[surfaceID] && !feishuSurfaceIDBelongsToGateway(surfaceID, gatewayID) {
			continue
		}
		addSurface(surfaceID)
		if flowID := strings.TrimSpace(flow.FlowID); flowID != "" {
			removedFlowIDs[flowID] = true
		}
		delete(a.turnPatchRuntime.ActiveFlows, requestID)
	}

	var resumeEvents []eventcontract.Event
	for instanceID, tx := range a.turnPatchRuntime.ActiveTx {
		if tx == nil {
			delete(a.turnPatchRuntime.ActiveTx, instanceID)
			continue
		}
		if !removedFlowIDs[strings.TrimSpace(tx.FlowID)] && !feishuSurfaceIDBelongsToGateway(tx.InitiatorSurface, gatewayID) {
			continue
		}
		delete(a.turnPatchRuntime.ActiveTx, instanceID)
		if flowID := strings.TrimSpace(tx.FlowID); flowID != "" {
			removedFlowIDs[flowID] = true
		}
		for surfaceID := range tx.PausedSurfaceIDs {
			resumeEvents = append(resumeEvents, a.service.ResumeSurfaceDispatch(surfaceID, nil)...)
		}
	}
	for requestID, flow := range a.turnPatchRuntime.ActiveFlows {
		if flow == nil {
			delete(a.turnPatchRuntime.ActiveFlows, requestID)
			continue
		}
		if !removedFlowIDs[strings.TrimSpace(flow.FlowID)] {
			continue
		}
		addSurface(flow.SurfaceSessionID)
		delete(a.turnPatchRuntime.ActiveFlows, requestID)
	}
	return resumeEvents
}

func feishuSurfaceIDBelongsToGateway(surfaceID, gatewayID string) bool {
	ref, ok := feishuidentity.ParseSurfaceRef(surfaceID)
	return ok && canonicalGatewayID(ref.GatewayID) == canonicalGatewayID(gatewayID)
}
