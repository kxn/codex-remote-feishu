package daemon

import (
	"time"

	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const (
	cronStateSchemaVersion    = cronrt.StateSchemaVersion
	cronDefaultTimeoutMinute  = cronrt.DefaultTimeoutMinute
	cronDefaultConcurrencyCap = cronrt.DefaultConcurrencyCap
	cronScheduleScanEvery     = cronrt.ScheduleScanEvery
	cronExitGrace             = cronrt.ExitGrace
	cronBitableBootstrapTTL   = cronrt.BitableBootstrapTTL
	cronBitableWorkspaceTTL   = cronrt.BitableWorkspaceTTL
	cronBitablePermissionTTL  = cronrt.BitablePermissionTTL
	cronReloadWorkspaceTTL    = cronrt.ReloadWorkspaceTTL
	cronReloadTasksTTL        = cronrt.ReloadTasksTTL
	cronWritebackRunsTTL      = cronrt.WritebackRunsTTL
	cronWritebackTasksTTL     = cronrt.WritebackTasksTTL
	cronInstancePrefix        = cronrt.InstancePrefix
	cronRunsTableName         = cronrt.RunsTableName
	cronTasksTableName        = cronrt.TasksTableName
	cronWorkspacesTableName   = cronrt.WorkspacesTableName
	cronMetaTableName         = cronrt.MetaTableName

	cronScheduleTypeDaily    = cronrt.ScheduleTypeDaily
	cronScheduleTypeInterval = cronrt.ScheduleTypeInterval

	cronTaskSourceTypeField     = cronrt.TaskSourceTypeField
	cronTaskWorkspaceField      = cronrt.TaskWorkspaceField
	cronTaskGitRepoInputField   = cronrt.TaskGitRepoInputField
	cronTaskConcurrencyField    = cronrt.TaskConcurrencyField
	cronTaskSourceWorkspaceText = cronrt.TaskSourceWorkspaceText
	cronTaskSourceGitRepoText   = cronrt.TaskSourceGitRepoText
)

type cronStateFile = cronrt.StateFile
type cronBitableState = cronrt.BitableState
type cronTableIDs = cronrt.TableIDs
type cronJobState = cronrt.JobState
type cronRunState = cronrt.RunState
type cronItemBuffer = cronrt.ItemBuffer
type cronExitTarget = cronrt.ExitTarget
type cronWritebackTarget = cronrt.WritebackTarget

type cronJobSourceType = cronrt.JobSourceType

const (
	cronJobSourceWorkspace = cronrt.JobSourceWorkspace
	cronJobSourceGitRepo   = cronrt.JobSourceGitRepo
)

type cronGatewayIdentity = cronrt.GatewayIdentity
type cronOwnerStatus = cronrt.OwnerStatus
type cronOwnerBinding = cronrt.OwnerBinding
type cronOwnerResolution = cronrt.OwnerResolution
type cronOwnerView = cronrt.OwnerView
type cronOwnerResolveOptions = cronrt.OwnerResolveOptions

const (
	cronOwnerStatusNone        = cronrt.OwnerStatusNone
	cronOwnerStatusHealthy     = cronrt.OwnerStatusHealthy
	cronOwnerStatusBootstrap   = cronrt.OwnerStatusBootstrap
	cronOwnerStatusUnavailable = cronrt.OwnerStatusUnavailable
	cronOwnerStatusMismatch    = cronrt.OwnerStatusMismatch
	cronOwnerStatusUnresolved  = cronrt.OwnerStatusUnresolved
)

type cronFieldSpec = cronrt.FieldSpec
type cronWorkspaceRow = cronrt.WorkspaceRow

type cronIntervalChoice = cronrt.IntervalChoice

var cronIntervalChoices = cronrt.IntervalChoices

type cronCommandMode = cronrt.CommandMode

const (
	cronCommandMenu   = cronrt.CommandModeMenu
	cronCommandStatus = cronrt.CommandModeStatus
	cronCommandList   = cronrt.CommandModeList
	cronCommandRun    = cronrt.CommandModeRun
	cronCommandEdit   = cronrt.CommandModeEdit
	cronCommandRepair = cronrt.CommandModeRepair
	cronCommandReload = cronrt.CommandModeReload
)

type parsedCronCommand = cronrt.ParsedCommand

type cronReloadTaskChange = cronrt.ReloadTaskChange

const (
	cronReloadTaskChangeAdded = cronrt.ReloadTaskChangeAdded
	cronReloadTaskChangeKept  = cronrt.ReloadTaskChangeKept
)

type cronReloadTaskItem = cronrt.ReloadTaskItem
type cronReloadError = cronrt.ReloadError
type cronReloadResult = cronrt.ReloadResult

func fallbackCronInstanceID(values ...string) string {
	return cronrt.FallbackInstanceID(values...)
}

func cloneCronState(stateValue *cronStateFile) *cronStateFile {
	return cronrt.CloneState(stateValue)
}

func cronAppTitle(instanceLabel string) string {
	return cronrt.AppTitle(instanceLabel)
}

func normalizeCronJobSourceType(raw string) cronJobSourceType {
	return cronrt.NormalizeJobSourceType(raw)
}

func cronJobSourceTypeLabel(sourceType cronJobSourceType) string {
	return cronrt.JobSourceTypeLabel(sourceType)
}

func cronInferJobSourceType(rawLabel, gitInput string, workspaceLinks []string) cronJobSourceType {
	return cronrt.InferJobSourceType(rawLabel, gitInput, workspaceLinks)
}

func cronNormalizeJobState(job cronJobState) cronJobState {
	return cronrt.NormalizeJobState(job)
}

func cronJobDisplaySource(job cronJobState) string {
	return cronrt.JobDisplaySource(job)
}

func cronJobConcurrencyText(limit int) string {
	return cronrt.JobConcurrencyText(limit)
}

func cronOwnerBindingBackfill(current *cronOwnerBinding, identity cronGatewayIdentity) (*cronOwnerBinding, bool) {
	return cronrt.OwnerBindingBackfill(current, identity)
}

func cronOwnerBindingFromState(stateValue *cronStateFile) *cronOwnerBinding {
	return cronrt.OwnerBindingFromState(stateValue)
}

func applyCronOwnerBinding(stateValue *cronStateFile, owner *cronOwnerBinding) {
	cronrt.ApplyOwnerBinding(stateValue, owner)
}

func cronOwnerActionError(action string, resolution cronOwnerResolution) error {
	return cronrt.OwnerActionError(action, resolution)
}

func cronTaskFieldSpecs(workspacesTableID string) []cronFieldSpec {
	return cronrt.TaskFieldSpecs(workspacesTableID)
}

func cronFieldTypeMatches(spec cronFieldSpec, current int) bool {
	return cronrt.FieldTypeMatches(spec, current)
}

func cronPermissionSatisfies(current, desired string) bool {
	return cronrt.PermissionSatisfies(current, desired)
}

func cronPermissionRank(value string) int {
	return cronrt.PermissionRank(value)
}

func cronSelectFieldProperty(labels []string) *larkbitable.AppTableFieldProperty {
	return cronrt.SelectFieldProperty(labels)
}

func cronDateTimeFieldProperty() *larkbitable.AppTableFieldProperty {
	return cronrt.DateTimeFieldProperty()
}

func cronIntervalLabels() []string {
	return cronrt.IntervalLabels()
}

func parseCronCommandText(text string) (parsedCronCommand, error) {
	return cronrt.ParseCommandText(text)
}

func cronDailyTimeFromFields(fields map[string]any) (int, int, bool) {
	return cronrt.DailyTimeFromFields(fields)
}

func cronUsageEvents(surfaceID, formDefault, message string) []control.UIEvent {
	return cronrt.UsageEvents(surfaceID, formDefault, message)
}

func cronBindingSummaryLines(stateValue *cronStateFile, configReady bool) []string {
	return cronrt.BindingSummaryLines(stateValue, configReady)
}

func cronConfigSummaryLine(stateValue *cronStateFile, configReady bool) string {
	return cronrt.ConfigSummaryLine(stateValue, configReady)
}

func cronRunsSummaryLine(stateValue *cronStateFile, configReady bool) string {
	return cronrt.RunsSummaryLine(stateValue, configReady)
}

func cronExternalLinkSection(stateValue *cronStateFile, configReady bool) (control.CommandCatalogSection, bool) {
	return cronrt.ExternalLinkSection(stateValue, configReady)
}

func cronExternalLinkButtons(stateValue *cronStateFile, configReady bool) []control.CommandCatalogButton {
	return cronrt.ExternalLinkButtons(stateValue, configReady)
}

func cronConfigLinkButton(stateValue *cronStateFile, configReady bool) (control.CommandCatalogButton, bool) {
	return cronrt.ConfigLinkButton(stateValue, configReady)
}

func cronRunsLinkButton(stateValue *cronStateFile, configReady bool) (control.CommandCatalogButton, bool) {
	return cronrt.RunsLinkButton(stateValue, configReady)
}

func cronBitableTableURL(appURL, tableID string) string {
	return cronrt.BitableTableURL(appURL, tableID)
}

func cronPrimaryMenuCommand(stateValue *cronStateFile, ownerView cronOwnerView) string {
	return cronrt.PrimaryMenuCommand(stateValue, ownerView)
}

func cronPrimaryDetailCommand(stateValue *cronStateFile, ownerView cronOwnerView) string {
	return cronrt.PrimaryDetailCommand(stateValue, ownerView)
}

func cronPrimaryEditCommand(stateValue *cronStateFile, ownerView cronOwnerView) string {
	return cronrt.PrimaryEditCommand(stateValue, ownerView)
}

func cronPrimaryButtonStyle(primaryCommand, commandText string) string {
	return cronrt.PrimaryButtonStyle(primaryCommand, commandText)
}

func cronRepairShouldBePrimary(stateValue *cronStateFile, ownerView cronOwnerView) bool {
	return cronrt.RepairShouldBePrimary(stateValue, ownerView)
}

func cronCanEdit(stateValue *cronStateFile) bool {
	return cronrt.CanEdit(stateValue)
}

func cronCanReload(stateValue *cronStateFile, ownerView cronOwnerView) bool {
	return cronrt.CanReload(stateValue, ownerView)
}

func cronOwnerAllowsLoadedJobs(status cronOwnerStatus) bool {
	return cronrt.OwnerAllowsLoadedJobs(status)
}

func cronLoadedJobCountLine(stateValue *cronStateFile, ownerView cronOwnerView) string {
	return cronrt.LoadedJobCountLine(stateValue, ownerView)
}

func cronSortedJobs(jobs []cronJobState) []cronJobState {
	return cronrt.SortedJobs(jobs)
}

func cronLoadedJobEntries(jobs []cronJobState, timeZone string) []control.CommandCatalogEntry {
	return cronrt.LoadedJobEntries(jobs, timeZone)
}

func cronRunCommandText(jobRecordID string) string {
	return cronrt.RunCommandText(jobRecordID)
}

func cronNoticeEvent(surfaceID, code, text string) control.UIEvent {
	return cronrt.NoticeEvent(surfaceID, code, text)
}

func buildCronRootPageView(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string, configReady bool, formDefault, statusKind, statusText string) control.FeishuPageView {
	return cronrt.BuildRootPageView(stateValue, ownerView, extraSummary, configReady, formDefault, statusKind, statusText)
}

func buildCronStatusPageView(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string, configReady bool) control.FeishuPageView {
	return cronrt.BuildStatusPageView(stateValue, ownerView, extraSummary, configReady)
}

func buildCronListPageView(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string) control.FeishuPageView {
	return cronrt.BuildListPageView(stateValue, ownerView, extraSummary)
}

func buildCronEditPageView(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string, configReady bool) control.FeishuPageView {
	return cronrt.BuildEditPageView(stateValue, ownerView, extraSummary, configReady)
}

func cronReloadTaskItemFromJob(job cronJobState) cronReloadTaskItem {
	return cronrt.ReloadTaskItemFromJob(job)
}

func cronReloadTaskPreviewFromRecord(record *larkbitable.AppTableRecord, workspacesByRecord map[string]cronWorkspaceRow, now time.Time, timeZone string) cronReloadTaskItem {
	return cronrt.ReloadTaskPreviewFromRecord(record, workspacesByRecord, now, timeZone)
}

func cronNewReloadError(record *larkbitable.AppTableRecord, tableName string, rowNumber int, taskName, fieldName, message string) *cronReloadError {
	return cronrt.NewReloadError(record, tableName, rowNumber, taskName, fieldName, message)
}

func cronJobFromReloadRecord(record *larkbitable.AppTableRecord, workspacesByRecord map[string]cronWorkspaceRow, now time.Time, timeZone, tableName string, rowNumber int) (cronJobState, bool, *cronReloadError) {
	return cronrt.JobFromReloadRecord(record, workspacesByRecord, now, timeZone, tableName, rowNumber)
}

func cronBuildReloadResult(records []*larkbitable.AppTableRecord, workspacesByRecord map[string]cronWorkspaceRow, now time.Time, previousJobs []cronJobState, timeZone string) cronReloadResult {
	return cronrt.BuildReloadResult(records, workspacesByRecord, now, previousJobs, timeZone)
}

func cronReloadTaskNoticeLine(item cronReloadTaskItem, plannedLabel, timeZone string) string {
	return cronrt.ReloadTaskNoticeLine(item, plannedLabel, timeZone)
}

func cronReloadTaskScheduleText(item cronReloadTaskItem) string {
	return cronrt.ReloadTaskScheduleText(item)
}

func cronReloadTaskNextRunText(item cronReloadTaskItem, label, timeZone string) string {
	return cronrt.ReloadTaskNextRunText(item, label, timeZone)
}

func cronReloadErrorNoticeLine(item cronReloadError) string {
	return cronrt.ReloadErrorNoticeLine(item)
}

func cronReloadTableLabel(name string) string {
	return cronrt.ReloadTableLabel(name)
}

func cronDefaultTimeoutMinutes(raw int) int {
	return cronrt.DefaultTimeoutMinutes(raw)
}

func cronDefaultMaxConcurrency(raw int) int {
	return cronrt.DefaultMaxConcurrency(raw)
}

func cronStateHasBinding(stateValue *cronStateFile) bool {
	return cronrt.StateHasBinding(stateValue)
}

func cronNextRunAt(job cronJobState, now time.Time) time.Time {
	return cronrt.NextRunAt(job, now)
}

func cronNextRunAtIn(job cronJobState, now time.Time, timeZone string) time.Time {
	return cronrt.NextRunAtIn(job, now, timeZone)
}

func cronAdvanceRunAt(job cronJobState, current, now time.Time) time.Time {
	return cronrt.AdvanceRunAt(job, current, now)
}

func cronAdvanceRunAtIn(job cronJobState, current, now time.Time, timeZone string) time.Time {
	return cronrt.AdvanceRunAtIn(job, current, now, timeZone)
}

func cronSchedulerTimeIn(now time.Time, timeZone string) time.Time {
	return cronrt.SchedulerTimeIn(now, timeZone)
}

func cronInstanceIDForRun(jobRecordID string, triggeredAt time.Time) string {
	return cronrt.InstanceIDForRun(jobRecordID, triggeredAt)
}

func cronRunSummary(text string) string {
	return cronrt.RunSummary(text)
}

func cronStatusText(status string) string {
	return cronrt.StatusText(status)
}

func cronMilliseconds(value time.Time) any {
	return cronrt.Milliseconds(value)
}

func cronConfiguredTimeZone(stateValue *cronStateFile) string {
	return cronrt.ConfiguredTimeZone(stateValue)
}

func cronFormatDisplayTime(value time.Time, timeZone string) string {
	return cronrt.FormatDisplayTime(value, timeZone)
}

func cronResolveLocation(timeZone string) *time.Location {
	return cronrt.ResolveLocation(timeZone)
}

func cronSystemTimeZone() string {
	return cronrt.SystemTimeZone()
}

func cronNormalizeTimeZone(value string) string {
	return cronrt.NormalizeTimeZone(value)
}

func cronReadTimeZoneFile(path string) string {
	return cronrt.ReadTimeZoneFile(path)
}

func cronTimeZoneFromLocaltime(path string) string {
	return cronrt.TimeZoneFromLocaltime(path)
}

func cronElapsedSeconds(startedAt, completedAt time.Time) any {
	return cronrt.ElapsedSeconds(startedAt, completedAt)
}

func cronItemBufferKey(itemID string) string {
	return cronrt.ItemBufferKey(itemID)
}

func cronJobActiveKey(jobRecordID, jobName string) string {
	return cronrt.JobActiveKey(jobRecordID, jobName)
}
