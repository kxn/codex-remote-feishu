import Foundation

enum InstallerResultPageKind: Equatable {
    case freshInstallSetup
    case freshInstallComplete
    case repairComplete
    case failure
}

enum InstallerResultPageActionKind: Equatable {
    case continueWebSetup
    case openAdminUI
    case openLogs
    case finish
}

struct InstallerResultPageAction: Equatable {
    let kind: InstallerResultPageActionKind
    let title: String
    let target: String?
}

struct InstallerResultPageInfoItem: Equatable {
    let label: String
    let value: String
}

struct InstallerResultPageModel: Equatable {
    let kind: InstallerResultPageKind
    let stepText: String
    let title: String
    let summary: String
    let detail: String
    let primaryAction: InstallerResultPageAction
    let auxiliaryActions: [InstallerResultPageAction]
    let infoItems: [InstallerResultPageInfoItem]

    static func fromSuccess(probe: InstallerProbeResult, result: PackagedInstallResultValue) -> InstallerResultPageModel {
        let isRepair = probe.mode == "repair"
        let startupMode = installerFriendlyStartupModeLabel(result.startupMode, serviceManager: result.serviceManager)
        let version = result.currentVersion.trimmingCharacters(in: .whitespacesAndNewlines)
        let needsSetup = !isRepair && result.setupRequired && !result.setupURL.isEmpty

        var infoItems: [InstallerResultPageInfoItem] = []
        if !version.isEmpty {
            infoItems.append(InstallerResultPageInfoItem(label: "已安装版本", value: version))
        }
        if !startupMode.isEmpty {
            infoItems.append(InstallerResultPageInfoItem(label: "当前启动方式", value: startupMode))
        }
        if !result.logPath.isEmpty {
            infoItems.append(InstallerResultPageInfoItem(label: "日志路径", value: result.logPath))
        }

        if needsSetup {
            return InstallerResultPageModel(
                kind: .freshInstallSetup,
                stepText: "Finished",
                title: "安装完成",
                summary: "后台服务已就绪。继续进入 WebSetup 完成首次配置。",
                detail: "安装器不会自动打开浏览器；只有在你点击继续后，才会 handoff 到 WebSetup。",
                primaryAction: InstallerResultPageAction(
                    kind: .continueWebSetup,
                    title: "继续 WebSetup",
                    target: result.setupURL
                ),
                auxiliaryActions: result.logPath.isEmpty ? [] : [
                    InstallerResultPageAction(
                        kind: .openLogs,
                        title: "打开日志",
                        target: result.logPath
                    ),
                ],
                infoItems: infoItems
            )
        }

        let summary: String
        if isRepair {
            if probe.sameVersion ?? false {
                summary = "已完成重装修复，后台服务已重新启动。"
            } else {
                summary = "已完成升级，后台服务已重新启动。"
            }
        } else {
            summary = "后台服务已启动。你现在可以结束安装器，或打开管理页继续使用。"
        }

        var auxiliaryActions: [InstallerResultPageAction] = []
        if !result.adminURL.isEmpty {
            auxiliaryActions.append(
                InstallerResultPageAction(
                    kind: .openAdminUI,
                    title: "打开管理页",
                    target: result.adminURL
                )
            )
        }
        if !result.logPath.isEmpty {
            auxiliaryActions.append(
                InstallerResultPageAction(
                    kind: .openLogs,
                    title: "打开日志",
                    target: result.logPath
                )
            )
        }

        return InstallerResultPageModel(
            kind: isRepair ? .repairComplete : .freshInstallComplete,
            stepText: "Finished",
            title: "安装完成",
            summary: summary,
            detail: "安装器已完成基础安装和服务启动。后续产品配置都在 WebSetup 或管理页中继续进行。",
            primaryAction: InstallerResultPageAction(
                kind: .finish,
                title: "完成",
                target: nil
            ),
            auxiliaryActions: auxiliaryActions,
            infoItems: infoItems
        )
    }

    static func fromFailure(message: String, detail: String, logPath: String) -> InstallerResultPageModel {
        var infoItems: [InstallerResultPageInfoItem] = []
        var auxiliaryActions: [InstallerResultPageAction] = []
        if !logPath.isEmpty {
            infoItems.append(InstallerResultPageInfoItem(label: "日志路径", value: logPath))
            auxiliaryActions.append(
                InstallerResultPageAction(
                    kind: .openLogs,
                    title: "打开日志",
                    target: logPath
                )
            )
        }

        return InstallerResultPageModel(
            kind: .failure,
            stepText: "Error",
            title: "安装失败",
            summary: message,
            detail: detail,
            primaryAction: InstallerResultPageAction(
                kind: .finish,
                title: "完成",
                target: nil
            ),
            auxiliaryActions: auxiliaryActions,
            infoItems: infoItems
        )
    }
}

func installerFriendlyStartupModeLabel(_ startupMode: String?, serviceManager: String?) -> String {
    let normalizedMode = (startupMode ?? "").trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    switch normalizedMode {
    case "manual":
        return "手动/按需启动"
    case "login_autostart":
        return "登录后自动启动"
    default:
        break
    }

    let normalizedManager = (serviceManager ?? "").trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    switch normalizedManager {
    case "detached":
        return "手动/按需启动"
    case "launchd_user", "task_scheduler_logon", "systemd_user":
        return "登录后自动启动"
    default:
        return ""
    }
}
