package workstore

var ManagementSchemaV1 = []string{
	`CREATE TABLE context_preferences (context_id TEXT PRIMARY KEY NOT NULL REFERENCES contexts(id),scope_id TEXT NOT NULL REFERENCES scopes(id),revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)))`,
	`CREATE TABLE project_role_descriptions (project_id TEXT NOT NULL REFERENCES projects(id),role_id TEXT NOT NULL REFERENCES roles(id),revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)),PRIMARY KEY(project_id,role_id))`,
	`CREATE TABLE team_ownership (team_id TEXT PRIMARY KEY NOT NULL REFERENCES teams(id),principal_id TEXT NOT NULL REFERENCES principals(id),revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)))`,
}
