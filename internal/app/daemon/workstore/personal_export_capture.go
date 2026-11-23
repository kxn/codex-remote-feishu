package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type personalExportRef struct {
	table, key, field string
	required, expand  bool
}

var personalContentFields = map[string]bool{
	"body_ref": true, "content_ref": true, "request_ref": true, "payload_ref": true, "prompt_ref": true, "form_ref": true, "resolved_evidence_ref": true, "result_ref": false, "outcome_ref": true, "processing_outcome_ref": true, "receipt_ref": false, "source_ref": false, "evidence_ref": false, "recalled_evidence_ref": false, "destination_text_ref": false, "verification_ref": false,
}

func collectPersonalRefs(value any, table, key string, refs map[string][]personalExportRef) {
	switch v := value.(type) {
	case map[string]any:
		for field, item := range v {
			normalized := strings.ToLower(field)
			if required, ok := personalContentFields[normalized]; ok {
				if id, ok := item.(string); ok && id != "" {
					ref := personalExportRef{table: table, key: key, field: field, required: required, expand: normalized == "request_ref" || normalized == "payload_ref" || (table == "team_ui_views" && normalized == "body_ref") || (table == "migration_stop_evidence" && normalized == "content_ref")}
					found := false
					for _, old := range refs[id] {
						if old == ref {
							found = true
							break
						}
					}
					if !found {
						refs[id] = append(refs[id], ref)
					}
				}
			}
			collectPersonalRefs(item, table, key, refs)
		}
	case []any:
		for _, item := range v {
			collectPersonalRefs(item, table, key, refs)
		}
	}
}
func addPersonalRecord(out *PersonalExport, record PersonalExportRecord, limits PersonalExportLimits, used *int64) error {
	size := int64(len(record.Payload) + len(record.Data) + len(record.Table) + len(record.Key))
	if len(out.Records) >= limits.MaxRows || size > limits.MaxBytes-*used {
		return fmt.Errorf("personal export exceeds configured row/byte limit")
	}
	*used += size
	data := record.Payload
	if record.Table == "content" {
		data = record.Data
	}
	record.SHA256 = exportHash(data)
	out.Records = append(out.Records, record)
	return nil
}
func (t *Tx) capturePersonalRows(out *PersonalExport, q personalExportQuery, scope string, limits PersonalExportLimits, used *int64, refs map[string][]personalExportRef) error {
	// The CASE prevents a single malicious payload from being materialized when
	// it already exceeds the caller's entire byte budget.
	query := `SELECT seq,k,rev,length(payload),CASE WHEN length(payload)<=?2 THEN payload ELSE NULL END FROM (` + q.query + `) AS source(seq,k,rev,payload) ORDER BY seq`
	// SQLite does not allow naming subquery columns using an alias column list;
	// query definitions expose stable names through a wrapping CTE instead.
	query = `WITH source(seq,k,rev,payload) AS (` + q.query + `) SELECT seq,k,rev,length(payload),CASE WHEN length(payload)<=?2 THEN payload ELSE NULL END FROM source ORDER BY seq`
	rows, err := t.tx.QueryContext(t.ctx, query, scope, limits.MaxBytes)
	if err != nil {
		return fmt.Errorf("personal export %s: %w", q.table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var order, rev, size int64
		var key string
		var payload sql.NullString
		if err = rows.Scan(&order, &key, &rev, &size, &payload); err != nil {
			return err
		}
		if !payload.Valid || size > limits.MaxBytes-*used {
			return fmt.Errorf("personal export exceeds configured byte limit")
		}
		raw := json.RawMessage(payload.String)
		if !json.Valid(raw) {
			return fmt.Errorf("personal export %s has invalid payload", q.table)
		}
		record := PersonalExportRecord{Table: q.table, Key: key, SourceOrder: order, Revision: rev, Payload: raw}
		if err = addPersonalRecord(out, record, limits, used); err != nil {
			return err
		}
		var object any
		if err = json.Unmarshal(raw, &object); err != nil {
			return err
		}
		collectPersonalRefs(object, q.table, key, refs)
	}
	return rows.Err()
}
func (t *Tx) capturePersonalContent(out *PersonalExport, scope string, limits PersonalExportLimits, used *int64, refs map[string][]personalExportRef) error {
	visited := map[string]bool{}
	expanded := map[string]bool{}
	missing := map[string]string{}
	for {
		ids := make([]string, 0)
		for id, uses := range refs {
			needsExpand := false
			for _, u := range uses {
				needsExpand = needsExpand || u.expand
			}
			if !visited[id] || needsExpand && !expanded[id] && missing[id] == "" {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			break
		}
		sort.Strings(ids)
		for _, id := range ids {
			uses := refs[id]
			needsExpand := false
			for _, u := range uses {
				needsExpand = needsExpand || u.expand
			}
			var actual, media, hash, created string
			var length int64
			err := t.tx.QueryRowContext(t.ctx, `SELECT scope_id,media_type,sha256,created_at,length(data) FROM content WHERE id=?`, id).Scan(&actual, &media, &hash, &created, &length)
			if errors.Is(err, sql.ErrNoRows) {
				visited[id] = true
				missing[id] = "external_or_unavailable"
				continue
			}
			if err != nil {
				return err
			}
			if actual != scope {
				visited[id] = true
				missing[id] = "outside_personal_scope"
				continue
			}
			if length > limits.MaxBytes-*used && !visited[id] {
				return fmt.Errorf("personal export exceeds configured byte limit")
			}
			content, err := t.GetContent(scope, id)
			if err != nil {
				return err
			}
			if !visited[id] {
				meta, err := json.Marshal(struct {
					ScopeID       string `json:"scope_id"`
					MediaType     string `json:"media_type"`
					ContentSHA256 string `json:"content_sha256"`
					CreatedAt     string `json:"created_at"`
				}{scope, media, hash, created})
				if err != nil {
					return err
				}
				if err = addPersonalRecord(out, PersonalExportRecord{Table: "content", Key: id, Payload: meta, Data: content.Data}, limits, used); err != nil {
					return err
				}
				visited[id] = true
			}
			if needsExpand && !expanded[id] {
				expanded[id] = true
				if strings.Contains(media, "json") {
					var value any
					if json.Unmarshal(content.Data, &value) == nil {
						collectPersonalRefs(value, "content", id, refs)
					}
				}
			}
		}
	}
	for id, reason := range missing {
		for _, ref := range refs[id] {
			if ref.required {
				return fmt.Errorf("personal export missing owned content reference %s/%s", ref.table, ref.field)
			}
			out.Unresolved = append(out.Unresolved, PersonalExportReference{Table: ref.table, Key: ref.key, Field: ref.field, ID: id, Reason: reason})
		}
	}
	return nil
}
