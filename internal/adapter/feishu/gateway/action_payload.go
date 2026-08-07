package gateway

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
	cardActionKindUpgradeOwnerFlow            = frontstagecontract.CardActionKindUpgradeOwnerFlow
	cardActionKindVSCodeMigrateOwnerFlow      = frontstagecontract.CardActionKindVSCodeMigrateOwnerFlow
	cardActionKindPlanProposal                = frontstagecontract.CardActionKindPlanProposal
	cardActionKindPageSubmit                  = frontstagecontract.CardActionKindPageSubmit
	cardActionKindPageLocalSubmit             = frontstagecontract.CardActionKindPageLocalSubmit
	cardActionKindSubmitRequestForm           = frontstagecontract.CardActionKindSubmitRequestForm
	cardActionKindPathPickerEnter             = frontstagecontract.CardActionKindPathPickerEnter
	cardActionKindPathPickerUp                = frontstagecontract.CardActionKindPathPickerUp
	cardActionKindPathPickerSelect            = frontstagecontract.CardActionKindPathPickerSelect
	cardActionKindPathPickerPage              = frontstagecontract.CardActionKindPathPickerPage
	cardActionKindPathPickerConfirm           = frontstagecontract.CardActionKindPathPickerConfirm
	cardActionKindPathPickerCancel            = frontstagecontract.CardActionKindPathPickerCancel
	cardActionKindTargetPickerSelectWorkspace = frontstagecontract.CardActionKindTargetPickerSelectWorkspace
	cardActionKindTargetPickerSelectSession   = frontstagecontract.CardActionKindTargetPickerSelectSession
	cardActionKindTargetPickerPage            = frontstagecontract.CardActionKindTargetPickerPage
	cardActionKindTargetPickerOpenPathPicker  = frontstagecontract.CardActionKindTargetPickerOpenPathPicker
	cardActionKindTargetPickerCancel          = frontstagecontract.CardActionKindTargetPickerCancel
	cardActionKindTargetPickerConfirm         = frontstagecontract.CardActionKindTargetPickerConfirm
	cardActionKindHistoryPage                 = frontstagecontract.CardActionKindHistoryPage
	cardActionKindHistoryDetail               = frontstagecontract.CardActionKindHistoryDetail
)

func actionPayloadKind(value map[string]any) string {
	return frontstagecontract.ActionPayloadKind(value)
}
