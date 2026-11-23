package privatechat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func validMigrationHash(s string) bool {
	raw, e := hex.DecodeString(s)
	return e == nil && len(raw) == 32
}

var migrationAllowedTables = func() map[string]bool {
	m := map[string]bool{}
	for _, name := range strings.Fields(`migration_manifest scopes personal_scoped_content personal_migration_fences personal_migration_preserved personal_migration_read_archives personal_migration_publication_holds personal_migration_reaction_holds personal_migration_execution_holds authority principals roles role_settings_references role_bot_bindings external_identities private_navigation contexts context_preferences execution_preferences desired_execution_locations locations raw_ingress ingress_observations messages inbox workers attempts migration_stop_evidence migration_intake_evidence attempt_inputs attempt_input_admissions outcome_reports outcomes operations outbox memories sharing_grants carrier_registrations carrier_occupancy resource_occupancy runtime_controller_occupancy runtime_carrier_processes runtime_gates runtime_gate_sequences runtime_continuations native_commands runtime_model_catalogs native_turns runtime_receipts runtime_quarantine quiescence_receipts team_ui_views private_execution_display team_private_reactions team_private_reaction_actions team_platform_observations private_shell_operations private_commands menu_interactions human_interactions native_ui_requests platform_message_mappings pause_gates native_maintenance native_upgrades session_goal_intents private_instruction_adoptions native_thread_token_usage automation_policies automation_episodes maintenance_admission_blocks work_migration_units work_migration_records work_migration_native_owners work_legacy_observations work_migration_resource_fences content`) {
		m[name] = true
	}
	return m
}()

func migrationRecordScopeValid(r MigrationImportedRecord, scope string) bool {
	var obj map[string]json.RawMessage
	if json.Unmarshal(r.Payload, &obj) != nil {
		return false
	}
	if raw, ok := obj["scope_id"]; ok {
		var value string
		if json.Unmarshal(raw, &value) != nil || value != scope {
			return false
		}
	}
	if r.Table == "scopes" {
		var v struct{ ID, Kind string }
		if json.Unmarshal(r.Payload, &v) != nil || v.ID != scope || v.Kind != "personal" {
			return false
		}
	}
	if r.Table == "memories" {
		var v struct {
			StorageScopeID string `json:"storage_scope_id"`
			Revision       int64  `json:"revision"`
			Scope          struct {
				Kind string `json:"kind"`
			} `json:"scope"`
		}
		if json.Unmarshal(r.Payload, &v) != nil || v.StorageScopeID != scope || v.Scope.Kind != "personal" || v.Revision != r.Revision || v.Revision < 1 {
			return false
		}
	}
	if r.Table == "private_execution_display" {
		var v struct {
			Binding struct {
				ScopeID string `json:"scopeId"`
			}
		}
		if json.Unmarshal(r.Payload, &v) != nil || v.Binding.ScopeID != scope {
			return false
		}
	}
	if r.Table == "raw_ingress" {
		var v struct {
			Route struct {
				ScopeID string `json:"scope_id"`
			} `json:"route"`
		}
		if json.Unmarshal(r.Payload, &v) != nil || v.Route.ScopeID != scope {
			return false
		}
	}
	return true
}
func (i *MigrationImporter) validateBundle(req MigrationImportRequest, b MigrationImportBundle) error {
	if b.SourceScope == "" || !validMigrationHash(b.SourceHash) || !validMigrationHash(b.ProofHash) || b.Checkpoint.SurfaceID != req.Owner.SurfaceID || b.Checkpoint.GatewayID != req.Owner.GatewayID || b.Checkpoint.ChatID != req.Owner.ChatID || b.Checkpoint.ActorUserID != req.Owner.UserID || len(b.Records) > 100000 || len(b.Events) > 100000 {
		return ErrJournalState
	}
	if b.Checkpoint.MigrationRecoveryReleased {
		return ErrJournalState
	}
	if b.SuspendQueue && (b.Resume != nil || len(b.Checkpoint.QueuedQueueItemIDs) == 0 || b.Checkpoint.ActiveQueueItemID != "") {
		return ErrJournalState
	}
	held := false
	for _, h := range b.Checkpoint.Holds {
		if h.Kind == "migration_source_unverified" {
			held = true
			break
		}
	}
	if !held {
		return ErrJournalState
	}
	var size int64
	records := map[string]MigrationImportedRecord{}
	for _, r := range b.Records {
		size += int64(len(r.Payload) + len(r.Data))
		if size > 128<<20 {
			return ErrJournalState
		}
		if !migrationAllowedTables[r.Table] || r.Key == "" || r.ScopeID != b.SourceScope || !json.Valid(r.Payload) || !migrationRecordScopeValid(r, b.SourceScope) || migrationSHA(r.Payload) != r.PayloadHash || migrationSHA(r.Data) != r.DataHash {
			return ErrJournalState
		}
		key := r.Table + "\x00" + r.Key
		if _, ok := records[key]; ok {
			return ErrJournalConflict
		}
		records[key] = r
	}
	if _, ok := records["scopes\x00"+b.SourceScope]; !ok {
		return ErrJournalState
	}
	manifest, ok := records["migration_manifest\x00"+b.SourceHash]
	if !ok || manifest.PayloadHash != b.SourceHash {
		return ErrJournalState
	}
	var manifestOwner struct {
		Source struct {
			ScopeID string `json:"scope_id"`
		} `json:"source"`
	}
	if json.Unmarshal(manifest.Payload, &manifestOwner) != nil || manifestOwner.Source.ScopeID != b.SourceScope {
		return ErrJournalState
	}
	seenHolds := map[string]bool{}
	for _, h := range b.InputHolds {
		if h.InboxID == "" || seenHolds[h.InboxID] || !validMigrationHash(h.PayloadHash) {
			return ErrJournalState
		}
		seenHolds[h.InboxID] = true
		switch h.Reason {
		case "claimed_input_requires_native_reconciliation", "input_content_not_complete", "input_route_requires_reconciliation", "input_platform_source_unverified", "media_requires_target_managed_file_import", "text_content_unavailable", "input_content_type_not_proven_text", "command_requires_target_ingress_replay":
		default:
			return ErrJournalState
		}
		r, ok := records["inbox\x00"+h.InboxID]
		if !ok || r.PayloadHash != h.PayloadHash {
			return ErrJournalConflict
		}
		var v struct {
			ID        string `json:"id"`
			MessageID string `json:"message_id"`
		}
		if json.Unmarshal(r.Payload, &v) != nil || v.ID != h.InboxID || v.MessageID != h.MessageID {
			return ErrJournalConflict
		}
	}
	if p := b.Resume; p != nil {
		if p.SourceHash != b.SourceHash || p.SourceScope != b.SourceScope || !validMigrationHash(p.SourceFingerprint) || !validMigrationHash(p.LaunchContentHash) || p.WorkerID == "" || p.LaunchContentID == "" || p.ThreadID == "" || p.Namespace == "" || !filepath.IsAbs(p.CWD) || (p.Backend != "codex" && p.Backend != "claude") {
			return ErrJournalState
		}
		if p.EvidenceVersion == "legacy-codex-v1" {
			if p.Backend != "codex" || p.LegacyEvidenceID == "" || !validMigrationHash(p.LegacyEvidenceHash) {
				return ErrJournalState
			}
			e, ok := records["content\x00"+p.LegacyEvidenceID]
			if !ok || e.DataHash != p.LegacyEvidenceHash {
				return ErrJournalState
			}
		} else if p.EvidenceVersion != "" && p.EvidenceVersion != "launch-v1" {
			return ErrJournalState
		} else if p.ConnectionIdentity == "" {
			return ErrJournalState
		}
		worker, ok := records["workers\x00"+p.WorkerID]
		if !ok || len(worker.Payload) == 0 {
			return ErrJournalState
		}
		launch, ok := records["content\x00"+p.LaunchContentID]
		if !ok || launch.DataHash != p.LaunchContentHash {
			return ErrJournalState
		}
	}
	for _, r := range b.Records {
		if r.Table != "memories" {
			continue
		}
		var memory struct {
			ContentRef string `json:"content_ref"`
		}
		if json.Unmarshal(r.Payload, &memory) != nil || memory.ContentRef == "" {
			return ErrJournalState
		}
		if _, ok := records["content\x00"+memory.ContentRef]; !ok {
			return ErrJournalState
		}
	}
	seenEvents := map[string]bool{}
	seenSources := map[string]bool{}
	unapplied := false
	for _, event := range b.Events {
		if !event.Applied {
			unapplied = true
		} else if unapplied {
			return ErrJournalState
		}
		if event.SourceScope != b.SourceScope || event.Ingress.Owner != req.Owner || event.Ingress.Generation != req.Generation || validateIngress(event.Ingress) != nil || !json.Valid(event.Ingress.Payload) || seenEvents[event.Ingress.EventID] {
			return ErrJournalState
		}
		seenEvents[event.Ingress.EventID] = true
		r, ok := records["ingress_observations\x00"+event.SourceKey]
		if !ok || r.PayloadHash != event.SourceHash {
			return ErrJournalState
		}
		var observed struct {
			Envelope struct {
				Tenant    string `json:"tenant"`
				AppID     string `json:"app_id"`
				EventID   string `json:"event_id"`
				ChatID    string `json:"chat_id"`
				ChatType  string `json:"chat_type"`
				MessageID string `json:"message_id"`
				SenderIDs struct {
					OpenID string `json:"open_id"`
				} `json:"sender_ids"`
			} `json:"envelope"`
		}
		if json.Unmarshal(r.Payload, &observed) != nil || observed.Envelope.Tenant != req.Owner.TenantID || observed.Envelope.AppID != req.Owner.AppID || observed.Envelope.EventID != event.Ingress.EventID || observed.Envelope.ChatID != req.Owner.ChatID || observed.Envelope.ChatType != "p2p" || observed.Envelope.SenderIDs.OpenID != req.Owner.UserID || observed.Envelope.MessageID == "" {
			return ErrJournalState
		}
		messageFound := false
		for _, id := range event.Ingress.Sources {
			if id == observed.Envelope.MessageID {
				messageFound = true
				break
			}
		}
		if !messageFound {
			return ErrJournalState
		}
		var action control.Action
		if json.Unmarshal(event.Ingress.Payload, &action) != nil || action.GatewayID != req.Owner.GatewayID || action.SurfaceSessionID != req.Owner.SurfaceID || action.ChatID != req.Owner.ChatID || action.ActorUserID != req.Owner.UserID || action.MessageID != observed.Envelope.MessageID {
			return ErrJournalState
		}
		for _, source := range event.Ingress.Sources {
			if seenSources[source] {
				return ErrJournalConflict
			}
			seenSources[source] = true
		}
	}
	root, err := os.OpenRoot(i.assetRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	assets := map[string]MigrationImportedAsset{}
	for _, asset := range b.Assets {
		if asset.ContentID == "" || asset.SourceMessageID == "" || !seenSources[asset.SourceMessageID] || !validMigrationHash(asset.SHA256) || asset.Size < 0 || !filepath.IsAbs(asset.Path) {
			return ErrJournalState
		}
		r, ok := records["content\x00"+asset.ContentID]
		if !ok || r.DataHash != asset.SHA256 || int64(len(r.Data)) != asset.Size {
			return ErrJournalState
		}
		relative, e := filepath.Rel(i.assetRoot, asset.Path)
		if e != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return ErrJournalState
		}
		info, e := root.Lstat(relative)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != asset.Size {
			return ErrJournalState
		}
		file, e := root.Open(relative)
		if e != nil {
			return e
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if fmt.Sprintf("%x", h.Sum(nil)) != asset.SHA256 {
			return ErrJournalState
		}
		assets[asset.Path] = asset
	}
	for _, image := range b.Checkpoint.StagedImages {
		if image == nil {
			return ErrJournalState
		}
		asset, ok := assets[image.LocalPath]
		if !ok || asset.SourceMessageID != image.SourceMessageID || asset.MediaType != image.MIMEType {
			return ErrJournalState
		}
	}
	for _, item := range b.Checkpoint.QueueItems {
		if item == nil {
			return ErrJournalState
		}
		for _, input := range item.Inputs {
			if input.Type == agentproto.InputLocalImage {
				asset, ok := assets[input.Path]
				if !ok || asset.MediaType != input.MIMEType {
					return ErrJournalState
				}
			}
		}
	}
	return nil
}
