package daemon

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// Installs a fixture-only trigger before user traffic or native startup. The
// shortened deadline is written by PrepareEffectDispatch's own transaction,
// so no second SQLite writer races an active checkpoint transaction.
func installFixtureReadDeadline(t *testing.T, app *App, kind string, offset time.Duration) {
	t.Helper()
	switch kind {
	case "mcp.oauth_login.start", "thread.goal.get", "thread.read":
	default:
		t.Fatalf("unsupported fixture read kind %q", kind)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	query := fmt.Sprintf(`CREATE TRIGGER fixture_read_deadline AFTER INSERT ON private_read_observations WHEN EXISTS (SELECT 1 FROM private_effect e WHERE e.id=substr(NEW.dispatch_id,1,length(NEW.dispatch_id)-7) AND json_extract(e.body,'$.event.Command.kind')='%s') AND (SELECT count(*) FROM private_read_observations r JOIN private_effect e ON e.id=substr(r.dispatch_id,1,length(r.dispatch_id)-7) WHERE json_extract(e.body,'$.event.Command.kind')='%s')=1 BEGIN UPDATE private_read_observations SET expires_ns=CAST(strftime('%%s','now') AS INTEGER)*1000000000+(%d) WHERE dispatch_id=NEW.dispatch_id; END`, kind, kind, offset.Nanoseconds())
	if _, err = db.Exec(query); err != nil {
		t.Fatal(err)
	}
}
