package daemon

// legacyWorkRetired is the boundary for consumers of pre-team execution and
// presentation state. Production selects history-only import before loading old
// files; configuring the durable runtime also closes legacy writers in tests or
// embedded hosts. Historical data remains available for migration observation.
func (a *App) legacyWorkRetired() bool {
	return a.headlessRuntime.LegacyHistoryOnly || a.teamRuntime != nil
}
