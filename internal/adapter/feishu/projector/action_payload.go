package projector

import frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"

const (
	cardPathPickerDirectorySelectFieldName    = frontstagecontract.CardPathPickerDirectorySelectFieldName
	cardPathPickerFileSelectFieldName         = frontstagecontract.CardPathPickerFileSelectFieldName
	cardTargetPickerWorkspaceFieldName        = frontstagecontract.CardTargetPickerWorkspaceFieldName
	cardTargetPickerSessionFieldName          = frontstagecontract.CardTargetPickerSessionFieldName
	cardSelectionThreadFieldName              = frontstagecontract.CardSelectionThreadFieldName
	cardThreadHistoryTurnFieldName            = frontstagecontract.CardThreadHistoryTurnFieldName
	cardActionPayloadDefaultCommandFieldName  = frontstagecontract.CardActionPayloadDefaultCommandFieldName
	cardActionKindAttachInstance              = frontstagecontract.CardActionKindAttachInstance
	cardActionKindAttachWorkspace             = frontstagecontract.CardActionKindAttachWorkspace
	cardActionKindUseThread                   = frontstagecontract.CardActionKindUseThread
	cardActionKindThreadSelectionPage         = frontstagecontract.CardActionKindThreadSelectionPage
	cardActionKindShowScopedThreads           = frontstagecontract.CardActionKindShowScopedThreads
	cardActionKindShowThreads                 = frontstagecontract.CardActionKindShowThreads
	cardActionKindShowAllThreads              = frontstagecontract.CardActionKindShowAllThreads
	cardActionKindShowAllThreadWorkspaces     = frontstagecontract.CardActionKindShowAllThreadWorkspaces
	cardActionKindShowRecentThreadWorkspaces  = frontstagecontract.CardActionKindShowRecentThreadWorkspaces
	cardActionKindShowWorkspaceThreads        = frontstagecontract.CardActionKindShowWorkspaceThreads
	cardActionKindShowAllWorkspaces           = frontstagecontract.CardActionKindShowAllWorkspaces
	cardActionKindShowRecentWorkspaces        = frontstagecontract.CardActionKindShowRecentWorkspaces
	cardActionKindKickThreadConfirm           = frontstagecontract.CardActionKindKickThreadConfirm
	cardActionKindKickThreadCancel            = frontstagecontract.CardActionKindKickThreadCancel
	cardActionKindRequestRespond              = frontstagecontract.CardActionKindRequestRespond
	cardActionKindRequestControl              = frontstagecontract.CardActionKindRequestControl
	cardActionKindPageAction                  = frontstagecontract.CardActionKindPageAction
	cardActionKindPageLocalAction             = frontstagecontract.CardActionKindPageLocalAction
	cardActionKindPageSubmit                  = frontstagecontract.CardActionKindPageSubmit
	cardActionKindPageLocalSubmit             = frontstagecontract.CardActionKindPageLocalSubmit
	cardActionKindSubmitRequestForm           = frontstagecontract.CardActionKindSubmitRequestForm
	cardActionKindPathPickerEnter             = frontstagecontract.CardActionKindPathPickerEnter
	cardActionKindPathPickerSelect            = frontstagecontract.CardActionKindPathPickerSelect
	cardActionKindPathPickerPage              = frontstagecontract.CardActionKindPathPickerPage
	cardActionKindPathPickerConfirm           = frontstagecontract.CardActionKindPathPickerConfirm
	cardActionKindPathPickerCancel            = frontstagecontract.CardActionKindPathPickerCancel
	cardActionKindTargetPickerSelectWorkspace = frontstagecontract.CardActionKindTargetPickerSelectWorkspace
	cardActionKindTargetPickerSelectSession   = frontstagecontract.CardActionKindTargetPickerSelectSession
	cardActionKindTargetPickerPage            = frontstagecontract.CardActionKindTargetPickerPage
	cardActionKindTargetPickerOpenPathPicker  = frontstagecontract.CardActionKindTargetPickerOpenPathPicker
	cardActionKindTargetPickerCancel          = frontstagecontract.CardActionKindTargetPickerCancel
	cardActionKindTargetPickerBack            = frontstagecontract.CardActionKindTargetPickerBack
	cardActionKindTargetPickerConfirm         = frontstagecontract.CardActionKindTargetPickerConfirm
	cardActionKindHistoryPage                 = frontstagecontract.CardActionKindHistoryPage
	cardActionKindHistoryDetail               = frontstagecontract.CardActionKindHistoryDetail
)

var (
	actionPayloadWithLifecycle         = frontstagecontract.ActionPayloadWithLifecycle
	actionPayloadNavigation            = frontstagecontract.ActionPayloadNavigation
	actionPayloadNavigationPage        = frontstagecontract.ActionPayloadNavigationPage
	actionPayloadThreadNavigation      = frontstagecontract.ActionPayloadThreadNavigation
	actionPayloadWorkspaceThreads      = frontstagecontract.ActionPayloadWorkspaceThreads
	actionPayloadAttachInstance        = frontstagecontract.ActionPayloadAttachInstance
	actionPayloadAttachWorkspace       = frontstagecontract.ActionPayloadAttachWorkspace
	actionPayloadUseThread             = frontstagecontract.ActionPayloadUseThread
	actionPayloadUseThreadField        = frontstagecontract.ActionPayloadUseThreadField
	actionPayloadThreadSelectionCursor = frontstagecontract.ActionPayloadThreadSelectionCursor
	actionPayloadKickThreadConfirm     = frontstagecontract.ActionPayloadKickThreadConfirm
	actionPayloadPageAction            = frontstagecontract.ActionPayloadPageAction
	actionPayloadPageLocalAction       = frontstagecontract.ActionPayloadPageLocalAction
	actionPayloadWithCatalog           = frontstagecontract.ActionPayloadWithCatalog
	actionPayloadPageSubmit            = frontstagecontract.ActionPayloadPageSubmit
	actionPayloadRequestRespond        = frontstagecontract.ActionPayloadRequestRespond
	actionPayloadRequestControl        = frontstagecontract.ActionPayloadRequestControl
	actionPayloadPathPicker            = frontstagecontract.ActionPayloadPathPicker
	actionPayloadPathPickerCursor      = frontstagecontract.ActionPayloadPathPickerCursor
	actionPayloadTargetPicker          = frontstagecontract.ActionPayloadTargetPicker
	actionPayloadTargetPickerCursor    = frontstagecontract.ActionPayloadTargetPickerCursor
	actionPayloadTargetPickerValue     = frontstagecontract.ActionPayloadTargetPickerValue
	actionPayloadThreadHistory         = frontstagecontract.ActionPayloadThreadHistory
)
