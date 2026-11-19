package codexstate

import (
	"database/sql"
	"strings"
)

// ExplicitThreadNames reads only requested identities, independent of activity
// order. Bounded batches avoid SQLite parameter limits for large loaded lists.
func (c *SQLiteThreadCatalog) ExplicitThreadNames(threadIDs []string) (map[string]string, error) {
	if len(threadIDs) == 0 {
		return nil, nil
	}
	var names map[string]string
	err := c.readWithRetry("query explicit thread names", func(db *sql.DB) error {
		column, err := desktopNameColumn(db)
		if err != nil {
			return err
		}
		current := map[string]string{}
		if column == "NULL" {
			names = current
			return nil
		}
		for start := 0; start < len(threadIDs); start += 200 {
			end := min(start+200, len(threadIDs))
			args := make([]any, 0, end-start+1)
			for _, id := range threadIDs[start:end] {
				args = append(args, id)
			}
			args = append(args, cronRepoRunPathPattern)
			rows, err := db.Query(`SELECT id, name FROM threads
WHERE id IN (`+strings.TrimSuffix(strings.Repeat("?,", end-start), ",")+`)
  AND archived = 0 AND source IN ('cli', 'vscode')
  AND COALESCE(agent_role, '') = ''
  AND cwd NOT LIKE '%/_tmp-codex-thread-latency-%'
  AND cwd NOT LIKE '%/_tmp-codex-appserver-%'
  AND cwd NOT LIKE ?`, args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id string
				var name sql.NullString
				if err := rows.Scan(&id, &name); err != nil {
					_ = rows.Close()
					return err
				}
				if name := strings.TrimSpace(name.String); name != "" {
					current[id] = name
				}
			}
			err = rows.Err()
			closeErr := rows.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		names = current
		return nil
	})
	return names, err
}
