package ingressowner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

type lockEntry struct {
	mu    sync.Mutex
	users int
}
type Router struct {
	db                        *sql.DB
	lock                      *os.File
	mu                        sync.Mutex
	keys                      map[Key]*lockEntry
	owners                    map[Owner]*lockEntry
	migrationVerifier         MigrationVerifier
	targetReadyVerifier       TargetReadyVerifier
	groupWitnessVerifier      GroupOwnerWitnessVerifier
	scopeVerifier             OwnerScopeBindingVerifier
	coverageVerifier          GatewayCoverageVerifier
	legacyScopeVerifier       LegacyScopeVerifier
	historicalStagingVerifier HistoricalStagingVerifier
	historicalSourceVerifier  HistoricalSourceVerifier
	routerID                  string
	verifier                  ReceiptVerifier
	bindingVerifier           BindingVerifier
}

func Open(path string, verifier ReceiptVerifier, bindingVerifier BindingVerifier) (*Router, error) {
	if path == "" || path == ":memory:" || !filepath.IsAbs(path) {
		return nil, ErrInvalid
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	path = filepath.Join(parent, filepath.Base(path))
	if info, e := os.Lstat(path); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalid
	} else if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	lock, err := lockRouter(path)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		unlockRouter(lock)
		return nil, err
	}
	err = file.Chmod(0600)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		unlockRouter(lock)
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		unlockRouter(lock)
		return nil, err
	}
	db.SetMaxOpenConns(1)
	r := &Router{db: db, lock: lock, keys: map[Key]*lockEntry{}, owners: map[Owner]*lockEntry{}, verifier: verifier, bindingVerifier: bindingVerifier}
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 0 && version != 1 && version != 2 && version != 3 {
		r.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unsupported ingress owner schema")
	}
	if version == 0 {
		var n int
		if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&n); err != nil || n != 0 {
			r.Close()
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("unversioned ingress database not empty")
		}
	}
	var mode string
	if err = db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&mode); err != nil || mode != "wal" {
		r.Close()
		return nil, fmt.Errorf("ingress journal requires WAL")
	}
	_, err = db.Exec(`PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON; BEGIN IMMEDIATE;
 CREATE TABLE IF NOT EXISTS events(seq INTEGER PRIMARY KEY AUTOINCREMENT,tenant TEXT NOT NULL,app_id TEXT NOT NULL,event_id TEXT NOT NULL,facts BLOB NOT NULL,source BLOB NOT NULL,owner BLOB,route BLOB,state TEXT NOT NULL CHECK(state IN ('unresolved','pending','delivering','accepted','expired')),response BLOB,receipt BLOB,revision INTEGER NOT NULL,UNIQUE(tenant,app_id,event_id));
 CREATE TABLE IF NOT EXISTS messages(tenant TEXT NOT NULL,app_id TEXT NOT NULL,chat_id TEXT NOT NULL,message_id TEXT NOT NULL,payload BLOB NOT NULL,PRIMARY KEY(tenant,app_id,chat_id,message_id));
 CREATE TABLE IF NOT EXISTS card_tokens(tenant TEXT NOT NULL,app_id TEXT NOT NULL,chat_id TEXT NOT NULL,message_id TEXT NOT NULL,card_kind TEXT NOT NULL,card_ref TEXT NOT NULL,version INTEGER NOT NULL,PRIMARY KEY(tenant,app_id,chat_id,message_id,card_kind,card_ref));
 CREATE TABLE IF NOT EXISTS router_metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS gateway_coverage(tenant TEXT NOT NULL,app_id TEXT NOT NULL,gateway TEXT NOT NULL,binding_generation INTEGER NOT NULL,body BLOB NOT NULL,proof BLOB NOT NULL,digest TEXT NOT NULL,PRIMARY KEY(tenant,app_id,gateway,binding_generation));
 CREATE TABLE IF NOT EXISTS gateway_coverage_lineage(tenant TEXT NOT NULL,app_id TEXT NOT NULL,gateway TEXT NOT NULL,binding_generation INTEGER NOT NULL,previous_digest TEXT NOT NULL,next_digest TEXT NOT NULL,previous_body BLOB NOT NULL,next_body BLOB NOT NULL,PRIMARY KEY(tenant,app_id,gateway,binding_generation,previous_digest),UNIQUE(tenant,app_id,gateway,binding_generation,next_digest));
 CREATE TABLE IF NOT EXISTS legacy_scope_barriers(scope_id TEXT PRIMARY KEY,role_id TEXT NOT NULL,app_id TEXT NOT NULL,gateway TEXT NOT NULL,binding_generation INTEGER NOT NULL,reason TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS group_owners(tenant TEXT NOT NULL,app_id TEXT NOT NULL,gateway TEXT NOT NULL,binding_generation INTEGER NOT NULL,chat_id TEXT NOT NULL,owner BLOB NOT NULL,PRIMARY KEY(tenant,app_id,gateway,binding_generation,chat_id));
 CREATE TABLE IF NOT EXISTS imported_scope_sources(tenant TEXT NOT NULL,app_id TEXT NOT NULL,event_id TEXT NOT NULL,gateway TEXT NOT NULL,binding_generation INTEGER NOT NULL,chat_id TEXT NOT NULL,open_id TEXT NOT NULL,owner BLOB NOT NULL,PRIMARY KEY(tenant,app_id,event_id),UNIQUE(tenant,app_id,gateway,binding_generation,chat_id,open_id));
 CREATE TABLE IF NOT EXISTS owner_scope_bindings(owner BLOB PRIMARY KEY,binding BLOB NOT NULL,proof BLOB NOT NULL,digest TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS historical_sources(id INTEGER PRIMARY KEY AUTOINCREMENT,tenant TEXT NOT NULL,app_id TEXT NOT NULL,event_id TEXT NOT NULL,event BLOB NOT NULL,owner BLOB NOT NULL,proof BLOB NOT NULL,UNIQUE(tenant,app_id,event_id));
 CREATE TABLE IF NOT EXISTS historical_staged_actions(id INTEGER PRIMARY KEY REFERENCES historical_sources(id),owner BLOB NOT NULL,body BLOB NOT NULL,body_hash TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS staged_private_actions(seq INTEGER PRIMARY KEY REFERENCES events(seq),owner BLOB NOT NULL,body BLOB NOT NULL,body_hash TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS delivery_attempts(seq INTEGER PRIMARY KEY REFERENCES events(seq),state TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS owner_migrations(owner BLOB PRIMARY KEY,migration_id TEXT NOT NULL UNIQUE,state TEXT NOT NULL,revision INTEGER NOT NULL,watermark INTEGER NOT NULL DEFAULT -1,target BLOB,import_receipt BLOB);
 CREATE TABLE IF NOT EXISTS migration_target_ready(owner BLOB PRIMARY KEY, migration_id TEXT NOT NULL, proof BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS group_owner_witnesses(tenant TEXT NOT NULL,app_id TEXT NOT NULL,gateway TEXT NOT NULL,binding_generation INTEGER NOT NULL,chat_id TEXT NOT NULL,witness BLOB NOT NULL,proof BLOB NOT NULL,PRIMARY KEY(tenant,app_id,gateway,binding_generation,chat_id));
 CREATE TABLE IF NOT EXISTS delivery_targets(seq INTEGER PRIMARY KEY REFERENCES events(seq),migration_id TEXT NOT NULL,target BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS private_callback_views(id INTEGER PRIMARY KEY AUTOINCREMENT,source_seq INTEGER NOT NULL UNIQUE,tenant TEXT NOT NULL,app_id TEXT NOT NULL,chat_id TEXT NOT NULL,message_id TEXT NOT NULL,base_version INTEGER NOT NULL,owner BLOB NOT NULL,token TEXT NOT NULL UNIQUE,response_hash TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS private_callback_view_message ON private_callback_views(tenant,app_id,chat_id,message_id,base_version,id);
 CREATE TABLE IF NOT EXISTS private_views(owner BLOB NOT NULL,effect_id TEXT NOT NULL,ordinal INTEGER NOT NULL,render_hash TEXT NOT NULL,token TEXT NOT NULL UNIQUE,PRIMARY KEY(owner,effect_id,ordinal));
 CREATE TABLE IF NOT EXISTS private_bindings(tenant TEXT NOT NULL,app_id TEXT NOT NULL,gateway TEXT NOT NULL,generation INTEGER NOT NULL,id_type TEXT NOT NULL,operator_id TEXT NOT NULL,payload BLOB NOT NULL,PRIMARY KEY(tenant,app_id,gateway,generation,id_type,operator_id));
 UPDATE events SET state='pending',revision=revision+1 WHERE state='delivering'; PRAGMA user_version=3; COMMIT;`)
	if err != nil {
		db.Exec(`ROLLBACK`)
		r.Close()
		return nil, err
	}
	nonce := make([]byte, 24)
	if _, err = rand.Read(nonce); err != nil {
		r.Close()
		return nil, err
	}
	if _, err = r.db.Exec(`INSERT INTO router_metadata(key,value) VALUES('router_id',?) ON CONFLICT(key) DO NOTHING`, hex.EncodeToString(nonce)); err != nil {
		r.Close()
		return nil, err
	}
	if err = r.db.QueryRow(`SELECT value FROM router_metadata WHERE key='router_id'`).Scan(&r.routerID); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}
func (r *Router) Close() error {
	e := r.db.Close()
	u := unlockRouter(r.lock)
	if e != nil {
		return e
	}
	return u
}
func (r *Router) acquire(k Key) func() {
	r.mu.Lock()
	e := r.keys[k]
	if e == nil {
		e = &lockEntry{}
		r.keys[k] = e
	}
	e.users++
	r.mu.Unlock()
	e.mu.Lock()
	return func() {
		e.mu.Unlock()
		r.mu.Lock()
		e.users--
		if e.users == 0 {
			delete(r.keys, k)
		}
		r.mu.Unlock()
	}
}
func facts(e Event) ([]byte, error) { e.Source = Source{}; return json.Marshal(e) }
func (r *Router) get(ctx context.Context, k Key) (Record, error) {
	var result Record
	var data, source, owner, route, response, receipt []byte
	err := r.db.QueryRowContext(ctx, `SELECT seq,facts,source,owner,route,state,response,receipt,revision FROM events WHERE tenant=? AND app_id=? AND event_id=?`, k.Tenant, k.AppID, k.EventID).Scan(&result.Sequence, &data, &source, &owner, &route, &result.State, &response, &receipt, &result.Revision)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(data, &result.Event); err != nil {
		return result, err
	}
	if err = json.Unmarshal(source, &result.Event.Source); err != nil {
		return result, err
	}
	if len(owner) > 0 {
		var o Owner
		if err = json.Unmarshal(owner, &o); err != nil {
			return result, err
		}
		result.Owner = &o
	}
	if len(route) > 0 {
		var snapshot RouteSnapshot
		if err = json.Unmarshal(route, &snapshot); err != nil {
			return result, err
		}
		result.Route = &snapshot
	}
	if len(receipt) > 0 {
		var saved AdmissionReceipt
		if err = json.Unmarshal(receipt, &saved); err != nil {
			return result, err
		}
		result.Receipt = &saved
	}
	result.Response = response
	var targetRaw []byte
	e := r.db.QueryRowContext(ctx, `SELECT target FROM delivery_targets WHERE seq=?`, result.Sequence).Scan(&targetRaw)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return result, e
	}
	if e == nil {
		var target Owner
		if err = json.Unmarshal(targetRaw, &target); err != nil {
			return result, err
		}
		result.DeliveryTarget = &target
	}
	return result, nil
}
func (r *Router) Get(ctx context.Context, k Key) (Record, error) { return r.get(ctx, k) }

func (r *Router) acquireOwner(owner Owner) func() {
	r.mu.Lock()
	e := r.owners[owner]
	if e == nil {
		e = &lockEntry{}
		r.owners[owner] = e
	}
	e.users++
	r.mu.Unlock()
	e.mu.Lock()
	return func() {
		e.mu.Unlock()
		r.mu.Lock()
		e.users--
		if e.users == 0 {
			delete(r.owners, owner)
		}
		r.mu.Unlock()
	}
}

// EventFactHash excludes local receipt/source metadata and hashes the stable SDK facts.
func EventFactHash(e Event) (string, error) {
	raw, err := facts(e)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
