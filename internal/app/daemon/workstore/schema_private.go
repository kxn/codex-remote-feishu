package workstore

var privateSchemaV1 = []string{
	`CREATE TABLE private_navigation (scope_id TEXT PRIMARY KEY NOT NULL REFERENCES scopes(id),revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)))`,
	`CREATE TABLE project_principal_access (scope_id TEXT NOT NULL REFERENCES scopes(id),principal_id TEXT NOT NULL REFERENCES principals(id),revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)),PRIMARY KEY(scope_id,principal_id))`,
	`CREATE TABLE membership_checks (id TEXT PRIMARY KEY NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)))`,
	`CREATE TABLE private_instruction_adoptions (id TEXT PRIMARY KEY NOT NULL,personal_scope_id TEXT NOT NULL REFERENCES scopes(id),project_scope_id TEXT NOT NULL REFERENCES scopes(id),source_message_id TEXT NOT NULL REFERENCES messages(id),target_task_id TEXT NOT NULL REFERENCES tasks(id),payload TEXT NOT NULL CHECK(json_valid(payload)),UNIQUE(personal_scope_id,project_scope_id,source_message_id))`,
}
