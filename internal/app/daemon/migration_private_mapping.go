package daemon

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// PrivateMigrationReport is a proposed projection only; it is not a drain,
// import proof, target journal write, or permission to resume native work.
type PrivateMigrationReport struct {
	SourceHash string                         `json:"source_hash"`
	Checkpoint orchestrator.PrivateCheckpoint `json:"checkpoint"`
	History    []PrivateMigrationHistory      `json:"history"`
	Blockers   []PrivateMigrationBlocker      `json:"blockers"`
	Evidence   []PrivateMigrationEvidence     `json:"evidence"`
	InputHolds []PrivateMigrationInputHold    `json:"input_holds,omitempty"`
}
type PrivateMigrationInputHold struct{ InboxID, MessageID, Reason, PayloadHash string }
type PrivateMigrationHistory struct {
	ContextID, WorkerID, ThreadID, Namespace, CWD, ConnectionIdentity string
	LaunchRecordID                                                    string
}
type PrivateMigrationBlocker struct{ Table, Key, Reason string }
type PrivateMigrationEvidence struct {
	Table, Key string
	Payload    json.RawMessage
}

func mapPersonalMigrationSource(source PersonalMigrationSource, owner privatechat.Owner) (PrivateMigrationReport, error) {
	return mapPersonalMigrationSourceAssets(source, owner, nil)
}

// mapPersonalMigrationSourceWithAssets materializes immutable source blobs and
// proposes the corresponding local-image checkpoint records. It does not
// commit them to the target journal.
func mapPersonalMigrationSourceWithAssets(source PersonalMigrationSource, owner privatechat.Owner, root string) (PrivateMigrationReport, []PersonalMigrationAsset, error) {
	if _, err := mapPersonalMigrationSource(source, owner); err != nil {
		return PrivateMigrationReport{}, nil, err
	}
	assets, err := materializePersonalMigrationAssets(source, root)
	if err != nil {
		return PrivateMigrationReport{}, nil, err
	}
	report, err := mapPersonalMigrationSourceAssets(source, owner, assets)
	return report, assets, err
}

func mapPersonalMigrationSourceAssets(source PersonalMigrationSource, owner privatechat.Owner, assets []PersonalMigrationAsset) (PrivateMigrationReport, error) {
	var report PrivateMigrationReport
	if err := source.Verify(); err != nil {
		return report, err
	}
	if owner.SurfaceID == "" || owner.GatewayID == "" || owner.TenantID == "" || owner.AppID == "" || owner.ChatID == "" || owner.UserID == "" {
		return report, fmt.Errorf("exact destination owner required")
	}
	if owner.GatewayID != source.Settings.GatewayRef {
		return report, fmt.Errorf("destination gateway differs from source")
	}
	rows := map[string][]workstore.PersonalExportRecord{}
	content := map[string]workstore.PersonalExportRecord{}
	for _, r := range source.Source.Records {
		rows[r.Table] = append(rows[r.Table], r)
		if r.Table == "content" {
			content[r.Key] = r
		}
	}
	if len(rows["role_bot_bindings"]) != 1 {
		return report, fmt.Errorf("source bot binding missing or ambiguous")
	}
	var binding work.RoleBotBinding
	if err := json.Unmarshal(rows["role_bot_bindings"][0].Payload, &binding); err != nil {
		return report, err
	}
	if binding.RoleID != source.Source.RoleID || binding.GatewayRef != owner.GatewayID || binding.AppID != owner.AppID {
		return report, fmt.Errorf("destination owner differs from source bot binding")
	}
	matched := false
	for _, r := range rows["ingress_observations"] {
		var o work.IngressObservation
		if err := json.Unmarshal(r.Payload, &o); err != nil {
			return report, err
		}
		if o.Envelope.ChatType == "p2p" && o.Envelope.Tenant == owner.TenantID && o.Envelope.AppID == owner.AppID && o.Envelope.ChatID == owner.ChatID && o.Envelope.SenderIDs.OpenID == owner.UserID && o.Gateway.GatewayRef == owner.GatewayID && o.Gateway.AppID == owner.AppID && o.Gateway.RoleID == source.Source.RoleID && o.Gateway.BotGeneration == binding.IdentityGeneration {
			matched = true
		}
	}
	if !matched {
		return report, fmt.Errorf("destination owner lacks matching source p2p observation")
	}
	c := orchestrator.PrivateCheckpoint{Version: 1, SurfaceID: owner.SurfaceID, GatewayID: owner.GatewayID, ChatID: owner.ChatID, ActorUserID: owner.UserID, Backend: source.Settings.Backend, RouteMode: state.RouteModeUnbound, QueueItems: map[string]*state.QueueItemRecord{}, StagedImages: map[string]*state.StagedImageRecord{}, StagedFiles: map[string]*state.StagedFileRecord{}, PickerWatermarks: &orchestrator.PrivatePickerWatermarks{}}
	switch c.Backend {
	case agentproto.BackendCodex:
		c.CodexProfileID = source.Settings.ProfileID
	case agentproto.BackendClaude:
		c.ClaudeProfileID = source.Settings.ProfileID
	case agentproto.BackendOpenCode:
		c.OpenCodeProfileID = source.Settings.ProfileID
	default:
		return report, fmt.Errorf("unsupported source backend")
	}
	c.PromptOverride.Model = source.Settings.BotModel
	c.PromptOverride.ReasoningEffort = source.Settings.BotReasoningEffort
	c.PromptOverride.AccessMode = agentproto.EffectiveAccessMode(source.Settings.AccessMode)
	if source.Settings.PlanModeOverrideSet {
		c.PlanMode = state.NormalizePlanModeSetting(state.PlanModeSetting(source.Settings.PlanMode))
		c.PlanModeOverrideSet = true
	}
	report.SourceHash = source.SHA256
	block := func(table, key, reason string) {
		report.Blockers = append(report.Blockers, PrivateMigrationBlocker{table, key, reason})
	}
	var nav work.PrivateNavigation
	if len(rows["private_navigation"]) != 1 {
		block("private_navigation", source.Source.ScopeID, "missing_or_ambiguous_navigation")
	} else if err := json.Unmarshal(rows["private_navigation"][0].Payload, &nav); err != nil {
		return report, err
	}
	if nav.Mode != work.PrivatePersonal {
		block("private_navigation", source.Source.ScopeID, "project_view_requires_separate_restore")
	}
	for _, r := range rows["contexts"] {
		var context work.WorkContext
		if err := json.Unmarshal(r.Payload, &context); err != nil {
			return report, err
		}
		report.Evidence = append(report.Evidence, PrivateMigrationEvidence{"contexts", r.Key, append(json.RawMessage(nil), r.Payload...)})
		// Non-front contexts remain imported source records. They never select
		// the current private session or authorize replay of their inputs.
	}
	for _, r := range rows["context_preferences"] {
		var p work.ContextPreferences
		if err := json.Unmarshal(r.Payload, &p); err != nil {
			return report, err
		}
		if p.ContextID != nav.FrontContextID {
			continue
		}
		if p.AccessMode != nil {
			c.PromptOverride.AccessMode = *p.AccessMode
		}
		if p.PlanMode != nil {
			c.PlanMode = state.PlanModeSetting(*p.PlanMode)
			c.PlanModeOverrideSet = true
		}
		if p.Verbose != nil {
			c.Verbosity = state.SurfaceVerbosity(*p.Verbose)
		}
	}
	for _, r := range rows["workers"] {
		var w work.WorkerSession
		if err := json.Unmarshal(r.Payload, &w); err != nil {
			return report, err
		}
		if w.ContextID != nav.FrontContextID || w.State == work.WorkerRetired {
			continue
		}
		h := PrivateMigrationHistory{ContextID: w.ContextID, WorkerID: w.ID, ThreadID: w.NativeSessionID, Namespace: w.NativeNamespace}
		var matches int
		for _, oprow := range rows["operations"] {
			var op work.Operation
			if json.Unmarshal(oprow.Payload, &op) != nil {
				return report, fmt.Errorf("invalid operation")
			}
			if op.Kind != "worker.launch" || op.TargetID != w.ID || op.TargetGeneration != w.Generation {
				continue
			}
			record, ok := content[op.RequestRef]
			if !ok {
				block("operations", op.ID, "launch_content_unavailable")
				continue
			}
			var launch workhost.LaunchRecord
			if err := json.Unmarshal(record.Data, &launch); err != nil {
				return report, err
			}
			if launch.WorkerID != w.ID || launch.Generation != w.Generation || launch.ScopeID != w.ScopeID {
				return report, fmt.Errorf("launch identity mismatch")
			}
			matches++
			h.LaunchRecordID = op.RequestRef
			h.CWD = launch.Snapshot.CWD
			h.ConnectionIdentity = launch.ConnectionIdentity
			if h.Namespace == "" {
				h.Namespace = launch.NativeHistoryNamespace
			} else if launch.NativeHistoryNamespace != "" && h.Namespace != launch.NativeHistoryNamespace {
				block("workers", w.ID, "native_namespace_conflict")
			}
		}
		if matches != 1 {
			block("workers", w.ID, "launch_missing_or_ambiguous")
		}
		if h.ThreadID != "" && h.Namespace == "" {
			block("workers", w.ID, "thread_namespace_unavailable")
		}
		if h.ThreadID != "" {
			block("workers", w.ID, "target_native_session_ownership_unverified")
		}
		report.History = append(report.History, h)
	}
	if len(report.History) > 1 {
		block("workers", nav.FrontContextID, "multiple_current_workers")
	}
	if len(report.History) == 1 && report.History[0].CWD == "" {
		block("workers", report.History[0].WorkerID, "launch_cwd_unavailable")
	}
	archivedReads, err := verifiedPersonalReadArchives(rows)
	if err != nil {
		return report, err
	}
	heldPublications, heldOutboxes, err := verifiedPersonalPublicationHolds(rows)
	if err != nil {
		return report, err
	}
	heldAttempts, heldExecutionOps, heldExecutionCommands, err := verifiedPersonalExecutionHolds(rows)
	if err != nil {
		return report, err
	}
	heldControlOps, heldControlCommands, err := verifiedPersonalControlHolds(rows)
	if err != nil {
		return report, err
	}
	for id := range heldControlOps {
		heldExecutionOps[id] = true
	}
	for id := range heldControlCommands {
		heldExecutionCommands[id] = true
	}
	heldRequests, err := verifiedPersonalRequestComponents(rows, heldAttempts)
	if err != nil {
		return report, err
	}
	preservedOps, preservedOutbox, err := verifiedPersonalPreserved(rows)
	if err != nil {
		return report, err
	}
	for _, table := range []string{"attempts", "operations", "outbox", "native_commands", "team_ui_views", "menu_interactions", "native_ui_requests", "native_upgrades"} {
		for _, r := range rows[table] {
			var x map[string]any
			if json.Unmarshal(r.Payload, &x) != nil {
				return report, fmt.Errorf("invalid %s", table)
			}
			stateValue, _ := x["state"].(string)
			stage, _ := x["stage"].(string)
			pending := table == "attempts" && (stateValue == "prepared" || stateValue == "dispatch_pending" || stateValue == "running" || stateValue == "reconciling") || table == "operations" && stateValue != "confirmed" && stateValue != "terminal" || table == "outbox" && stateValue != "sent" && stateValue != "terminal" || table == "native_commands" || table == "native_ui_requests" && stateValue != "resolved" && stateValue != "stale" || table == "native_upgrades" && stage != "completed"
			report.Evidence = append(report.Evidence, PrivateMigrationEvidence{table, r.Key, append(json.RawMessage(nil), r.Payload...)})
			if table == "team_ui_views" || table == "menu_interactions" || table == "outbox" {
				block(table, r.Key, "target_journal_or_view_import_unavailable")
			}
			if pending && !(table == "operations" && preservedOps[r.Key]) && !(table == "outbox" && preservedOutbox[r.Key]) && !((table == "operations" || table == "native_commands") && archivedReads[r.Key]) && !(table == "operations" && heldPublications[r.Key]) && !(table == "outbox" && heldOutboxes[r.Key]) && !(table == "attempts" && heldAttempts[r.Key]) && !(table == "operations" && heldExecutionOps[r.Key]) && !(table == "native_commands" && heldExecutionCommands[r.Key]) && !(table == "native_ui_requests" && heldRequests[r.Key]) {
				block(table, r.Key, "requires_recovery_reconciliation")
				c.Holds = append(c.Holds, orchestrator.PrivateRecoveryHold{Kind: "source_" + table, Frozen: append(json.RawMessage(nil), r.Payload...)})
			}
		}
	}
	if err := mapPersonalMigrationInputs(&report, &c, rows, content, nav, owner, assets); err != nil {
		return PrivateMigrationReport{}, err
	}
	for _, u := range source.Source.Unresolved {
		block(u.Table, u.Key, u.Reason)
	}
	block("source", source.Source.ScopeID, "drain_and_final_export_proof_required")
	barrier, _ := json.Marshal(struct {
		SourceHash string `json:"source_hash"`
		Reason     string `json:"reason"`
	}{source.SHA256, "drain_and_final_export_proof_required"})
	c.Holds = append(c.Holds, orchestrator.PrivateRecoveryHold{Kind: "migration_source_unverified", Frozen: barrier})
	sort.Slice(report.Blockers, func(i, j int) bool {
		a, b := report.Blockers[i], report.Blockers[j]
		return strings.Join([]string{a.Table, a.Key, a.Reason}, "\x00") < strings.Join([]string{b.Table, b.Key, b.Reason}, "\x00")
	})
	report.Checkpoint = c
	return report, nil
}
