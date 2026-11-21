package workstore

var ingressSchemaV1 = []string{
	`CREATE TABLE bootstrap_leader_selections (tenant TEXT NOT NULL,chat_id TEXT NOT NULL,binding_generation INTEGER NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),PRIMARY KEY(tenant,chat_id,binding_generation))`,
	`CREATE TABLE raw_ingress (sequence INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT UNIQUE NOT NULL,revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)),state TEXT GENERATED ALWAYS AS (json_extract(payload,'$.state')) STORED NOT NULL)`,
	`CREATE INDEX ingress_pending ON raw_ingress(state,sequence)`,
	`CREATE INDEX ingress_establishment ON raw_ingress(json_extract(payload,'$.envelope.tenant'),json_extract(payload,'$.envelope.chat_id'),json_extract(payload,'$.route.binding_generation'),sequence)`,
	`CREATE TABLE ingress_observations (ingress_id TEXT NOT NULL REFERENCES raw_ingress(id),app_id TEXT NOT NULL,generation INTEGER NOT NULL,event_id TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),PRIMARY KEY(app_id,generation,event_id))`,
	`CREATE TABLE ingress_selection_notices (ingress_id TEXT PRIMARY KEY NOT NULL REFERENCES raw_ingress(id),revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)))`,
	`CREATE TABLE external_identities (tenant TEXT NOT NULL,domain TEXT NOT NULL,id_type TEXT NOT NULL,external_id TEXT NOT NULL,generation INTEGER NOT NULL CHECK(generation>0),principal_id TEXT NOT NULL REFERENCES principals(id),evidence_ref TEXT NOT NULL,PRIMARY KEY(tenant,domain,id_type,external_id,generation))`,
	`CREATE TABLE private_commands (id TEXT PRIMARY KEY NOT NULL,revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)),scope_id TEXT GENERATED ALWAYS AS (json_extract(payload,'$.scope_id')) STORED NOT NULL REFERENCES scopes(id),state TEXT GENERATED ALWAYS AS (json_extract(payload,'$.state')) STORED NOT NULL)`,
	`CREATE TABLE task_conversations (scope_id TEXT NOT NULL REFERENCES scopes(id),task_id TEXT NOT NULL REFERENCES tasks(id),revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)),PRIMARY KEY(scope_id,task_id))`,
	`CREATE TABLE task_topic_reservations (scope_id TEXT NOT NULL REFERENCES scopes(id),task_id TEXT NOT NULL REFERENCES tasks(id),operation_id TEXT NOT NULL REFERENCES operations(id),PRIMARY KEY(scope_id,task_id))`,
	`CREATE TABLE platform_message_mappings (tenant TEXT NOT NULL,chat_id TEXT NOT NULL,message_id TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),PRIMARY KEY(tenant,chat_id,message_id))`,
}
