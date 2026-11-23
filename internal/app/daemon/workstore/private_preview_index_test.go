package workstore

import (
	"strings"
	"testing"
)

func TestPrivatePreviewQueriesUsePartialIndex(t *testing.T) {
	store, _ := openTestStore(t)
	requireTx(t, store, func(tx *Tx) error {
		for _, query := range []string{
			`EXPLAIN QUERY PLAN SELECT id FROM private_execution_display WHERE json_extract(payload,'$.PreviewTerminal') IS NOT NULL ORDER BY json_extract(payload,'$.Binding.attemptId'),id LIMIT 32`,
			`EXPLAIN QUERY PLAN SELECT 1 FROM private_execution_display WHERE json_extract(payload,'$.PreviewTerminal') IS NOT NULL AND json_extract(payload,'$.Binding.attemptId')='attempt' LIMIT 1`,
		} {
			rows, err := tx.tx.QueryContext(tx.ctx, query)
			if err != nil {
				return err
			}
			used := false
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					rows.Close()
					return err
				}
				used = used || strings.Contains(detail, "private_execution_display_pending_preview")
			}
			if err := rows.Close(); err != nil {
				return err
			}
			if !used {
				t.Errorf("preview query did not use partial index: %s", query)
			}
		}
		return nil
	})
}
