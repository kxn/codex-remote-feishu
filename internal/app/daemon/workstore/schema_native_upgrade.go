package workstore

const NativeUpgradeSchemaV1 = `CREATE TABLE IF NOT EXISTS native_upgrades(id TEXT PRIMARY KEY,host_id TEXT NOT NULL,payload TEXT NOT NULL); CREATE UNIQUE INDEX IF NOT EXISTS native_upgrade_active_host ON native_upgrades(host_id) WHERE json_extract(payload,'$.stage')<>'completed';`
