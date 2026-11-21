package workstore

var MigrationSchemaV1 = []string{
	`CREATE TABLE work_legacy_observations (id INTEGER PRIMARY KEY AUTOINCREMENT,instance_id TEXT NOT NULL,kind TEXT NOT NULL,payload_hash TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),observed_at TEXT NOT NULL,UNIQUE(instance_id,kind,payload_hash))`,
	`CREATE TABLE work_authority (id TEXT PRIMARY KEY CHECK(id='runtime'),owner TEXT NOT NULL,generation INTEGER NOT NULL,migration_id TEXT NOT NULL,revision INTEGER NOT NULL)`,
	`CREATE TABLE work_migrations (id TEXT PRIMARY KEY,source_fingerprint TEXT NOT NULL,phase TEXT NOT NULL,revision INTEGER NOT NULL,authority_generation INTEGER NOT NULL,snapshot TEXT NOT NULL CHECK(json_valid(snapshot)),fence TEXT CHECK(fence IS NULL OR json_valid(fence)))`,
	`CREATE TABLE work_migration_records (migration_id TEXT NOT NULL REFERENCES work_migrations(id),record_key TEXT NOT NULL,unit_key TEXT NOT NULL,kind TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),state TEXT NOT NULL,reason TEXT NOT NULL DEFAULT '',scope_id TEXT NOT NULL DEFAULT '',context_id TEXT NOT NULL DEFAULT '',owner TEXT NOT NULL DEFAULT '',revision INTEGER NOT NULL,PRIMARY KEY(migration_id,record_key))`,
	`CREATE INDEX work_migration_pending ON work_migration_records(migration_id,state,record_key)`,
	`CREATE TABLE work_migration_units (migration_id TEXT NOT NULL,unit_key TEXT NOT NULL,principal_id TEXT NOT NULL,role_id TEXT NOT NULL,scope_id TEXT NOT NULL REFERENCES scopes(id),context_id TEXT NOT NULL REFERENCES contexts(id),PRIMARY KEY(migration_id,unit_key))`,
	`CREATE TABLE work_migration_native_owners (native_key TEXT PRIMARY KEY,migration_id TEXT NOT NULL,unit_key TEXT NOT NULL,principal_id TEXT NOT NULL,role_id TEXT NOT NULL,worker_id TEXT NOT NULL REFERENCES workers(id))`,
	`CREATE TABLE work_migration_resource_fences (resource_key TEXT NOT NULL,migration_id TEXT NOT NULL,record_key TEXT NOT NULL,reason TEXT NOT NULL,evidence TEXT NOT NULL CHECK(json_valid(evidence)),released INTEGER NOT NULL DEFAULT 0,resolution TEXT CHECK(resolution IS NULL OR json_valid(resolution)),PRIMARY KEY(resource_key,migration_id,record_key))`,
}
