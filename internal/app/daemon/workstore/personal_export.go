package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// PersonalExport is a point-in-time source snapshot, not a write fence,
// migration receipt, or authorization to switch execution ownership.
type PersonalExport struct {
	FormatVersion       int                         `json:"format_version"`
	SourceSchemaVersion int                         `json:"source_schema_version"`
	ScopeID             string                      `json:"scope_id"`
	PrincipalID         string                      `json:"principal_id"`
	RoleID              string                      `json:"role_id"`
	CapturedAt          time.Time                   `json:"captured_at"`
	ReadOnly            bool                        `json:"read_only"`
	WritersFenced       bool                        `json:"writers_fenced"`
	SHA256              string                      `json:"sha256"`
	Records             []PersonalExportRecord      `json:"records"`
	Unresolved          []PersonalExportReference   `json:"unresolved_references"`
	TablePolicy         []PersonalExportTablePolicy `json:"table_policy"`
}
type PersonalExportTablePolicy struct {
	Table       string `json:"table"`
	Disposition string `json:"disposition"`
	Reason      string `json:"reason"`
}
type PersonalExportLimits struct {
	MaxRows  int
	MaxBytes int64
}
type PersonalExportRecord struct {
	Table       string          `json:"table"`
	Key         string          `json:"key"`
	SourceOrder int64           `json:"source_order"`
	Revision    int64           `json:"revision,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Data        []byte          `json:"data,omitempty"`
	SHA256      string          `json:"sha256"`
}
type PersonalExportReference struct {
	Table  string `json:"table"`
	Key    string `json:"key"`
	Field  string `json:"field"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}
type personalExportQuery struct{ table, query string }

// Page returns independent copies from the already-frozen capture. It never
// re-queries a potentially changed source database.
func (s PersonalExport) Page(offset, limit int) ([]PersonalExportRecord, int, error) {
	if err := s.Verify(); err != nil {
		return nil, 0, err
	}
	if offset < 0 || offset > len(s.Records) || limit < 1 || limit > 1000 {
		return nil, 0, fmt.Errorf("invalid export page")
	}
	end := min(offset+limit, len(s.Records))
	out := make([]PersonalExportRecord, 0, end-offset)
	for _, r := range s.Records[offset:end] {
		r.Payload = append(json.RawMessage(nil), r.Payload...)
		r.Data = append([]byte(nil), r.Data...)
		out = append(out, r)
	}
	next := 0
	if end < len(s.Records) {
		next = end
	}
	return out, next, nil
}

// Verify detects accidental mutation of any exported row, bytes or metadata.
func (s PersonalExport) Verify() error {
	expected := s.SHA256
	s.SHA256 = ""
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if expected == "" || exportHash(raw) != expected {
		return fmt.Errorf("personal export integrity mismatch")
	}
	return nil
}
func exportHash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// CapturePersonalExport must be called inside Store.Within. It does no external
// IO, never changes source state, and fails rather than returning partial data
// when limits or a required same-scope content reference cannot be satisfied.
func (t *Tx) CapturePersonalExport(scopeID string, limits PersonalExportLimits) (PersonalExport, error) {
	var out PersonalExport
	if limits.MaxRows < 1 || limits.MaxBytes < 1 {
		return out, fmt.Errorf("positive export limits required")
	}
	scope, err := t.GetScope(scopeID)
	if err != nil {
		return out, err
	}
	if scope.Kind != work.ScopePersonal {
		return out, ErrScopeDenied
	}
	if _, err = t.GetPrincipal(scope.PrincipalID); err != nil {
		return out, err
	}
	if _, err = t.GetRoleBotBinding(scope.RoleID); err != nil {
		return out, err
	}
	var schema int
	if err = t.tx.QueryRowContext(t.ctx, `PRAGMA user_version`).Scan(&schema); err != nil {
		return out, err
	}
	out = PersonalExport{FormatVersion: 1, SourceSchemaVersion: schema, ScopeID: scope.ID, PrincipalID: scope.PrincipalID, RoleID: scope.RoleID, CapturedAt: time.Now().UTC(), ReadOnly: true, Records: []PersonalExportRecord{}, Unresolved: []PersonalExportReference{}}
	if out.TablePolicy, err = t.personalExportTablePolicy(); err != nil {
		return PersonalExport{}, err
	}
	out.Unresolved = append(out.Unresolved, PersonalExportReference{Table: "role_settings_references", Key: scope.RoleID, Field: "effective_settings", Reason: "external_settings_snapshot_required_hash_revision_match"}, PersonalExportReference{Table: "profile_model_catalogs", Key: scope.RoleID, Field: "namespace_key", Reason: "external_config_account_identity_required_to_resolve_catalog"}, PersonalExportReference{Table: "work_legacy_observations", Key: scope.ID, Field: "instance_ownership", Reason: "legacy_instance_evidence_requires_scope_proof"})
	var used int64
	refs := map[string][]personalExportRef{}
	for _, q := range personalExportQueries {
		if err = t.capturePersonalRows(&out, q, scopeID, limits, &used, refs); err != nil {
			return PersonalExport{}, err
		}
	}
	identityObserved := false
	for _, record := range out.Records {
		if record.Table == "external_identities" {
			identityObserved = true
			break
		}
	}
	if !identityObserved {
		out.Unresolved = append(out.Unresolved, PersonalExportReference{Table: "external_identities", Key: scope.PrincipalID, Field: "p2p_observation", Reason: "authenticated_personal_identity_not_observed"})
	}
	if err = t.capturePersonalContent(&out, scopeID, limits, &used, refs); err != nil {
		return PersonalExport{}, err
	}
	sort.Slice(out.Unresolved, func(i, j int) bool {
		a, b := out.Unresolved[i], out.Unresolved[j]
		return strings.Join([]string{a.Table, a.Key, a.Field, a.ID}, "\x00") < strings.Join([]string{b.Table, b.Key, b.Field, b.ID}, "\x00")
	})
	if len(out.Records)+len(out.Unresolved) > limits.MaxRows {
		return PersonalExport{}, fmt.Errorf("personal export exceeds configured row limit")
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return PersonalExport{}, err
	}
	// SHA-256 is encoded as 64 bytes in the final JSON, beyond the empty
	// placeholder used to calculate the self-excluding digest.
	if int64(len(raw))+64 > limits.MaxBytes {
		return PersonalExport{}, fmt.Errorf("personal export exceeds configured byte limit")
	}
	out.SHA256 = exportHash(raw)
	return out, nil
}
