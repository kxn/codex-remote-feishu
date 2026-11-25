package daemon

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Preserve only the isolated fixture's database files, never account config.
func preserveReviewEvidenceOnFailure(t *testing.T, app *App) { preserveReviewEvidence(t, app, false) }
func preserveReviewEvidence(t *testing.T, app *App, force bool) {
	t.Helper()
	if !force && !t.Failed() {
		return
	}
	root := app.headlessRuntime.Paths.StateDir
	dst, err := os.MkdirTemp("/tmp", "private-review-evidence-")
	if err != nil {
		t.Logf("evidence directory: %v", err)
		return
	}
	count := 0
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return nil
		}
		target := filepath.Join(dst, rel)
		if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
			t.Logf("evidence mkdir: %v", e)
			return nil
		}
		db, e := sql.Open("sqlite", "file:"+path+"?mode=ro")
		if e != nil {
			t.Logf("evidence open: %v", e)
			return nil
		}
		defer db.Close()
		if _, e = db.Exec(`VACUUM INTO ?`, target); e != nil {
			t.Logf("evidence snapshot %s: %v", rel, e)
			return nil
		}
		_ = os.Chmod(target, 0600)
		count++
		return nil
	})
	t.Logf("review fixture evidence directory=%s databases=%d", dst, count)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dst, "private-chat.db")+"?mode=ro")
	if err != nil {
		return
	}
	defer db.Close()
	rows, err := db.Query(`SELECT sequence,coalesce(json_extract(payload,'$.kind'),''),coalesce(json_extract(payload,'$.action'),''),coalesce(json_extract(payload,'$.status'),''),coalesce(json_extract(payload,'$.threadId'),''),coalesce(json_extract(payload,'$.turnId'),''),coalesce(json_extract(payload,'$.metadata.reviewThreadID'),''),command_id,applied FROM native_events WHERE json_extract(payload,'$.action')='review.start' OR applied=0 ORDER BY sequence LIMIT 50`)
	if err != nil {
		t.Logf("evidence query: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		var kind, action, status, thread, turn, child, command string
		var applied int
		if rows.Scan(&seq, &kind, &action, &status, &thread, &turn, &child, &command, &applied) == nil {
			t.Log(fmt.Sprintf("review native seq=%d kind=%s action=%s status=%s thread=%s turn=%s child=%s command=%s applied=%d", seq, kind, action, status, thread, turn, child, command, applied))
		}
	}
}

func controlledReviewShutdown(t *testing.T, g graphEnv) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(g.app.headlessRuntime.Paths.StateDir, "private-carriers.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	err = db.QueryRow(`SELECT json_extract(record,'$.process.pid') FROM carriers WHERE state='running' LIMIT 1`).Scan(&pid)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "controlled review shutdown wrapper_pid=%d state=%s\n", pid, g.app.headlessRuntime.Paths.StateDir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = g.app.shutdownApplications(ctx); err != nil {
		t.Errorf("controlled review shutdown: %v", err)
		preserveReviewEvidenceOnFailure(t, g.app)
		release := fmt.Sprintf("/tmp/private-review-shutdown-release-%d", pid)
		fmt.Fprintf(os.Stderr, "controlled review shutdown failed; wrapper_pid=%d release=%s\n", pid, release)
		deadline := time.Now().Add(45 * time.Second)
		for time.Now().Before(deadline) {
			if _, e := os.Stat(release); e == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.FailNow()
	}
}

// Record only process topology and redacted argv from this isolated fixture.
func captureReviewProcessTree(t *testing.T, pid int, path string) {
	t.Helper()
	if runtime.GOOS != "linux" || path == "" {
		return
	}
	type proc struct {
		pid, ppid, pgid int
		comm            string
		argv            []string
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Logf("process topology unavailable: %v", err)
		return
	}
	procs := map[int]proc{}
	for _, e := range entries {
		id, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() {
			continue
		}
		base := filepath.Join("/proc", e.Name())
		raw, err := os.ReadFile(filepath.Join(base, "stat"))
		if err != nil {
			continue
		}
		line := string(raw)
		left, right := strings.IndexByte(line, '('), strings.LastIndexByte(line, ')')
		if left < 0 || right < left {
			continue
		}
		fields := strings.Fields(line[right+1:])
		if len(fields) < 3 {
			continue
		}
		ppid, e1 := strconv.Atoi(fields[1])
		pgid, e2 := strconv.Atoi(fields[2])
		if e1 != nil || e2 != nil {
			continue
		}
		p := proc{pid: id, ppid: ppid, pgid: pgid, comm: line[left+1 : right]}
		if cmd, e := os.ReadFile(filepath.Join(base, "cmdline")); e == nil {
			for i, a := range strings.Split(string(cmd), "\x00") {
				if a == "" {
					continue
				}
				if i == 0 {
					p.argv = append(p.argv, filepath.Base(a))
					continue
				}
				if strings.HasPrefix(a, "-") {
					name, _, _ := strings.Cut(a, "=")
					p.argv = append(p.argv, name)
				} else if a == "app-server" || a == "serve" || a == "stdio" {
					p.argv = append(p.argv, a)
				} else {
					p.argv = append(p.argv, "[redacted]")
				}
			}
		}
		procs[id] = p
	}
	root, ok := procs[pid]
	if !ok {
		t.Logf("isolated process topology root unavailable pid=%d", pid)
		return
	}
	selected := map[int]bool{pid: true}
	changed := true
	for changed {
		changed = false
		for id, p := range procs {
			if !selected[id] && (selected[p.ppid] || p.pgid == root.pgid) {
				selected[id] = true
				changed = true
			}
		}
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Logf("process topology artifact: %v", err)
		return
	}
	defer out.Close()
	for id := range selected {
		p := procs[id]
		fmt.Fprintf(out, "pid=%d ppid=%d pgid=%d comm=%q argv=%q\n", p.pid, p.ppid, p.pgid, p.comm, p.argv)
	}
	t.Logf("isolated process topology artifact=%s", path)
}
