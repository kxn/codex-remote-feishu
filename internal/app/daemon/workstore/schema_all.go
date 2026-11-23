package workstore

// All components share one transaction and schema version. These groups are
// split only for source ownership and review; none opens a second database.
func allSchemaV1() []string {
	statements := []string{}
	for _, group := range [][]string{schemaV1, schemaKnowledge, RuntimeSchemaV1, ProvisioningSchemaV1, ingressSchemaV1, ManagementSchemaV1, MigrationSchemaV1, privateSchemaV1} {
		statements = append(statements, group...)
	}
	return append(statements, SchemaPrivateShellOperationsV1, SchemaPrivateReactionActionsV1, SchemaPrivateReactionsV1, SchemaProfileModelCatalogV1, SchemaPrivateExecutionDisplayV1, SchemaPublicationBudgetV1, SchemaPublicationOutboxIndexV1, SchemaExecutionPreferencesV1, SchemaSettingsReferenceV1, SchemaDesiredLocationV1, SchemaNativeRequestsV1, SchemaHumanInteractionsV1, SchemaUIViewsV1, SchemaMaintenanceV1, SchemaPlatformObservationsV1, SchemaMenuInteractionsV1, SchemaDispatchProblemsV1, SchemaReviewResultsV1, SchemaSessionGoalIntentsV1, SchemaAutomationV1, SchemaReviewFailuresV1, SchemaInputAdmissionsV1, NativeUpgradeSchemaV1, SchemaPersonalMigrationFenceV1, SchemaPersonalMigrationPreservedV1, SchemaPersonalMigrationSealV1, SchemaPersonalMigrationReadArchiveV1, SchemaPersonalMigrationPublicationHoldV1, SchemaPersonalMigrationReactionHoldV1, SchemaPersonalMigrationExecutionHoldV1, SchemaPrivateProjectIdentityV1, SchemaPrivateProjectShareV1)
}
