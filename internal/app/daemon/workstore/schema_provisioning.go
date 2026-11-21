package workstore

// ProvisioningSchemaV1 is installed alongside the work schema, before any saga
// runs. Step evidence survives process restarts independently of projections.
var ProvisioningSchemaV1 = []string{
	`CREATE TABLE provision_steps (id TEXT PRIMARY KEY NOT NULL,revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)),scope_id TEXT GENERATED ALWAYS AS (json_extract(payload,'$.scope_id')) STORED NOT NULL REFERENCES scopes(id),operation_id TEXT GENERATED ALWAYS AS (json_extract(payload,'$.operation_id')) STORED NOT NULL REFERENCES operations(id),step_id TEXT GENERATED ALWAYS AS (json_extract(payload,'$.step_id')) STORED NOT NULL,state TEXT GENERATED ALWAYS AS (json_extract(payload,'$.state')) STORED NOT NULL,UNIQUE(operation_id,step_id))`,
	`CREATE INDEX provision_steps_state ON provision_steps(operation_id,state)`,
	`CREATE TABLE project_homes (id TEXT PRIMARY KEY NOT NULL REFERENCES projects(id),revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)),scope_id TEXT GENERATED ALWAYS AS (json_extract(payload,'$.scope_id')) STORED NOT NULL REFERENCES scopes(id),operation_id TEXT GENERATED ALWAYS AS (json_extract(payload,'$.operation_id')) STORED NOT NULL REFERENCES operations(id))`,
}
