package workstore

var RuntimeSchemaV1 = []string{
	`CREATE TABLE runtime_gate_sequences (worker_id TEXT PRIMARY KEY REFERENCES workers(id),revision INTEGER NOT NULL)`,
	`CREATE TABLE runtime_model_catalogs (command_id TEXT PRIMARY KEY REFERENCES native_commands(command_id),incarnation TEXT NOT NULL REFERENCES carrier_registrations(incarnation),sequence INTEGER NOT NULL,catalog_key TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)))`,
	`CREATE INDEX runtime_catalog_key ON runtime_model_catalogs(catalog_key,sequence)`,
	`CREATE TABLE runtime_carrier_processes (incarnation TEXT PRIMARY KEY REFERENCES carrier_registrations(incarnation),instance_id TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)))`,
	`CREATE TABLE runtime_controller_occupancy (resource_key TEXT PRIMARY KEY,worker_id TEXT NOT NULL REFERENCES workers(id),location_generation INTEGER NOT NULL,controller_generation INTEGER NOT NULL)`,
	`CREATE TABLE carrier_registrations (incarnation TEXT PRIMARY KEY NOT NULL,worker_id TEXT NOT NULL REFERENCES workers(id),instance_id TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),connected INTEGER NOT NULL DEFAULT 0)`,
	`CREATE INDEX carrier_instance ON carrier_registrations(instance_id)`,
	`CREATE TABLE native_commands (command_id TEXT PRIMARY KEY NOT NULL REFERENCES operations(id),incarnation TEXT NOT NULL REFERENCES carrier_registrations(incarnation),attempt_id TEXT REFERENCES attempts(id),payload TEXT NOT NULL CHECK(json_valid(payload)))`,
	`CREATE TABLE runtime_quarantine (incarnation TEXT NOT NULL,sequence INTEGER NOT NULL,session_id TEXT NOT NULL,turn_id TEXT NOT NULL,reason TEXT NOT NULL,resolved INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(incarnation,sequence),FOREIGN KEY(incarnation,sequence) REFERENCES runtime_receipts(carrier_incarnation,sequence))`,
	`CREATE TABLE runtime_gates (worker_id TEXT PRIMARY KEY NOT NULL REFERENCES workers(id),reason TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)))`,
	`CREATE TABLE runtime_continuations (incarnation TEXT NOT NULL,session_id TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),PRIMARY KEY(incarnation,session_id))`,
	`CREATE INDEX native_attempt ON native_turns(attempt_id)`,
}
