import Foundation

struct InstallerProbeResult: Codable {
    let ok: Bool
    let mode: String
    let statePath: String?
    let configPath: String?
    let currentVersion: String?
    let currentTrack: String?
    let installerVersion: String?
    let sameVersion: Bool?
    let currentInstallBinDir: String?
    let suggestedInstallBinDir: String?
    let installLocationEditable: Bool?
    let serviceManager: String?
    let error: String?
}

struct PackagedInstallResultValue {
    var ok: Bool = false
    var mode: String = ""
    var statePath: String = ""
    var configPath: String = ""
    var installedBinary: String = ""
    var serviceManager: String = ""
    var currentVersion: String = ""
    var currentTrack: String = ""
    var currentSlot: String = ""
    var adminURL: String = ""
    var setupURL: String = ""
    var setupRequired: Bool = false
    var logPath: String = ""
    var error: String = ""
}

enum InstallerRuntimeError: LocalizedError {
    case missingResource(String)
    case unsupportedArchitecture(String)
    case invalidProbe(String)
    case launchFailure(String)
    case resultFileMissing(String)

    var errorDescription: String? {
        switch self {
        case .missingResource(let value):
            return "缺少安装器资源：\(value)"
        case .unsupportedArchitecture(let value):
            return "当前 macOS 架构暂不支持：\(value)"
        case .invalidProbe(let value):
            return "安装探测失败：\(value)"
        case .launchFailure(let value):
            return "启动安装进程失败：\(value)"
        case .resultFileMissing(let value):
            return "安装结果文件不存在：\(value)"
        }
    }
}

struct InstallerMetadata {
    let version: String
    let track: String
}

struct InstallerLaunchPlan {
    let probe: InstallerProbeResult
    let title: String
    let summary: String
    let primaryActionTitle: String
    let installerVersion: String
    let defaultInstallDir: String
    let installLocationEditable: Bool
}

struct InstallerExecutionSummary {
    let result: PackagedInstallResultValue
    let stdout: String
    let stderr: String
}

struct InstallerExecutionRequest {
    let probe: InstallerProbeResult
    let installBinDir: String
}

struct InstallerFailureState {
    let message: String
    let detail: String
}

enum ScreenState {
    case loading
    case ready
    case installing
    case success(PackagedInstallResultValue)
    case failure(InstallerFailureState)
}
