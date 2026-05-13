import Cocoa

final class InstallerBridge {
    private let fileManager = FileManager.default
    private var extractedPayloadURLs: [String: URL] = [:]

    func loadMetadata() throws -> InstallerMetadata {
        let version = try readResourceText(named: "installer-version", extension: "txt")
        let trackText = try? readResourceText(named: "installer-track", extension: "txt")
        let track = trackText.flatMap { normalizedTrack(from: $0) } ?? inferTrack(from: version)
        return InstallerMetadata(version: version, track: track)
    }

    func probe() throws -> InstallerLaunchPlan {
        let metadata = try loadMetadata()
        let probeBinary = try selectedPayloadBinaryURL()
        let stdout = try runSync(arguments: [
            "packaged-install-probe",
            "-current-version", metadata.version,
            "-format", "json",
        ], binaryURL: probeBinary)
        let data = Data(stdout.utf8)
        let probe = try JSONDecoder().decode(InstallerProbeResult.self, from: data)
        if !probe.ok {
            throw InstallerRuntimeError.invalidProbe(probe.error ?? "unknown probe error")
        }

        let defaultInstallDir = (probe.currentInstallBinDir?.isEmpty == false ? probe.currentInstallBinDir : probe.suggestedInstallBinDir) ?? ""
        let editable = probe.installLocationEditable ?? false
        return InstallerLaunchPlan(
            probe: probe,
            title: screenTitle(for: probe),
            summary: screenSummary(for: probe),
            primaryActionTitle: primaryActionTitle(for: probe),
            installerVersion: metadata.version,
            defaultInstallDir: defaultInstallDir,
            installLocationEditable: editable
        )
    }

    func runInstall(
        request: InstallerExecutionRequest,
        onOutput: @escaping (String) -> Void,
        completion: @escaping (Result<InstallerExecutionSummary, Error>) -> Void
    ) {
        do {
            let metadata = try loadMetadata()
            let binaryURL = try selectedPayloadBinaryURL()
            let resultFileURL = fileManager.temporaryDirectory
                .appendingPathComponent("codex-remote-installer-\(UUID().uuidString)")
                .appendingPathExtension("ini")

            var arguments = [
                "packaged-install",
                "-binary", binaryURL.path,
                "-current-version", metadata.version,
                "-current-track", metadata.track,
                "-service-manager", "launchd_user",
                "-format", "text",
                "-result-file", resultFileURL.path,
            ]
            if request.probe.mode == "repair", let statePath = request.probe.statePath, !statePath.isEmpty {
                arguments += ["-state-path", statePath]
            } else if !request.installBinDir.isEmpty {
                arguments += ["-install-bin-dir", request.installBinDir]
            }

            let process = Process()
            process.executableURL = binaryURL
            process.arguments = arguments

            let stdoutPipe = Pipe()
            let stderrPipe = Pipe()
            process.standardOutput = stdoutPipe
            process.standardError = stderrPipe

            let stdoutBuffer = OutputBuffer(onOutput: onOutput)
            let stderrBuffer = OutputBuffer(onOutput: onOutput)
            let stdoutHandle = stdoutPipe.fileHandleForReading
            let stderrHandle = stderrPipe.fileHandleForReading
            stdoutHandle.readabilityHandler = { handle in
                let data = handle.availableData
                stdoutBuffer.consume(data)
            }
            stderrHandle.readabilityHandler = { handle in
                let data = handle.availableData
                stderrBuffer.consume(data)
            }

            process.terminationHandler = { [weak self] _ in
                guard let self else {
                    return
                }
                stdoutHandle.readabilityHandler = nil
                stderrHandle.readabilityHandler = nil
                stdoutBuffer.consume(stdoutHandle.readDataToEndOfFile())
                stderrBuffer.consume(stderrHandle.readDataToEndOfFile())

                DispatchQueue.main.async {
                    defer {
                        try? self.fileManager.removeItem(at: resultFileURL)
                    }
                    do {
                        let result = try self.parseResultFile(at: resultFileURL)
                        let summary = InstallerExecutionSummary(
                            result: result,
                            stdout: stdoutBuffer.snapshot(),
                            stderr: stderrBuffer.snapshot()
                        )
                        completion(.success(summary))
                    } catch {
                        completion(.failure(error))
                    }
                }
            }

            try process.run()
        } catch {
            completion(.failure(error))
        }
    }

    func openURL(_ rawValue: String) {
        guard let url = URL(string: rawValue) else {
            return
        }
        NSWorkspace.shared.open(url)
    }

    private func selectedPayloadBinaryURL() throws -> URL {
        let machine = try machineArchitecture()
        let resourceName: String
        switch machine {
        case "arm64":
            resourceName = "codex-remote-darwin-arm64"
        case "x86_64":
            resourceName = "codex-remote-darwin-amd64"
        default:
            throw InstallerRuntimeError.unsupportedArchitecture(machine)
        }

        if let directURL = Bundle.main.resourceURL?.appendingPathComponent("payload/\(resourceName)"),
           fileManager.isExecutableFile(atPath: directURL.path) {
            return directURL
        }

        if let cachedURL = extractedPayloadURLs[resourceName],
           fileManager.isExecutableFile(atPath: cachedURL.path) {
            return cachedURL
        }

        guard let archiveURL = Bundle.main.resourceURL?.appendingPathComponent("payload/\(resourceName).tar.gz"),
              fileManager.fileExists(atPath: archiveURL.path) else {
            throw InstallerRuntimeError.missingResource(resourceName)
        }

        let extractedURL = try extractPayloadBinary(from: archiveURL, resourceName: resourceName)
        extractedPayloadURLs[resourceName] = extractedURL
        return extractedURL
    }

    private func machineArchitecture() throws -> String {
        var uts = utsname()
        guard uname(&uts) == 0 else {
            throw InstallerRuntimeError.unsupportedArchitecture("unknown")
        }
        var machine = uts.machine
        let machineSize = MemoryLayout.size(ofValue: machine)
        return withUnsafePointer(to: &machine) { pointer in
            pointer.withMemoryRebound(to: CChar.self, capacity: machineSize) { rebound in
                String(cString: rebound)
            }
        }
    }

    private func runSync(arguments: [String], binaryURL: URL) throws -> String {
        let process = Process()
        process.executableURL = binaryURL
        process.arguments = arguments
        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe
        try process.run()
        process.waitUntilExit()

        let stdoutData = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
        let stderrData = stderrPipe.fileHandleForReading.readDataToEndOfFile()
        let stdout = String(data: stdoutData, encoding: .utf8) ?? ""
        let stderr = String(data: stderrData, encoding: .utf8) ?? ""
        if process.terminationStatus != 0 {
            let detail = stderr.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? stdout : stderr
            throw InstallerRuntimeError.launchFailure(detail.trimmingCharacters(in: .whitespacesAndNewlines))
        }
        return stdout
    }

    private func extractPayloadBinary(from archiveURL: URL, resourceName: String) throws -> URL {
        let extractionRoot = fileManager.temporaryDirectory
            .appendingPathComponent("codex-remote-installer-\(resourceName)-\(UUID().uuidString)", isDirectory: true)
        try fileManager.createDirectory(at: extractionRoot, withIntermediateDirectories: true)

        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/tar")
        process.arguments = ["-xzf", archiveURL.path, "-C", extractionRoot.path]
        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe
        try process.run()
        process.waitUntilExit()

        let stdout = String(data: stdoutPipe.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
        let stderr = String(data: stderrPipe.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
        if process.terminationStatus != 0 {
            let detail = stderr.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? stdout : stderr
            throw InstallerRuntimeError.launchFailure(detail.trimmingCharacters(in: .whitespacesAndNewlines))
        }

        guard let enumerator = fileManager.enumerator(
            at: extractionRoot,
            includingPropertiesForKeys: [.isRegularFileKey],
            options: [.skipsHiddenFiles]
        ) else {
            throw InstallerRuntimeError.missingResource(resourceName)
        }

        for case let candidateURL as URL in enumerator {
            guard candidateURL.lastPathComponent == "codex-remote" else {
                continue
            }
            try fileManager.setAttributes([.posixPermissions: 0o755], ofItemAtPath: candidateURL.path)
            return candidateURL
        }

        throw InstallerRuntimeError.missingResource(resourceName)
    }

    private func parseResultFile(at url: URL) throws -> PackagedInstallResultValue {
        guard fileManager.fileExists(atPath: url.path) else {
            throw InstallerRuntimeError.resultFileMissing(url.path)
        }
        let content = try String(contentsOf: url, encoding: .utf8)
        var result = PackagedInstallResultValue()
        for rawLine in content.components(separatedBy: .newlines) {
            let line = rawLine.trimmingCharacters(in: .whitespacesAndNewlines)
            if line.isEmpty || line.hasPrefix("[") {
                continue
            }
            let parts = line.split(separator: "=", maxSplits: 1, omittingEmptySubsequences: false)
            guard parts.count == 2 else {
                continue
            }
            let key = String(parts[0])
            let value = String(parts[1])
            switch key {
            case "ok":
                result.ok = value == "true"
            case "mode":
                result.mode = value
            case "statePath":
                result.statePath = value
            case "configPath":
                result.configPath = value
            case "installedBinary":
                result.installedBinary = value
            case "serviceManager":
                result.serviceManager = value
            case "currentVersion":
                result.currentVersion = value
            case "currentTrack":
                result.currentTrack = value
            case "currentSlot":
                result.currentSlot = value
            case "adminURL":
                result.adminURL = value
            case "setupURL":
                result.setupURL = value
            case "setupRequired":
                result.setupRequired = value == "true"
            case "logPath":
                result.logPath = value
            case "error":
                result.error = value
            default:
                continue
            }
        }
        return result
    }

    private func readResourceText(named name: String, extension ext: String) throws -> String {
        guard let url = Bundle.main.url(forResource: name, withExtension: ext) else {
            throw InstallerRuntimeError.missingResource("\(name).\(ext)")
        }
        return try String(contentsOf: url, encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func inferTrack(from version: String) -> String {
        let trimmed = version.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.contains("-alpha.") {
            return "alpha"
        }
        if trimmed.contains("-beta.") {
            return "beta"
        }
        return "production"
    }

    private func normalizedTrack(from rawValue: String) -> String? {
        switch rawValue.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() {
        case "production", "beta", "alpha":
            return rawValue.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        default:
            return nil
        }
    }

    private func screenTitle(for probe: InstallerProbeResult) -> String {
        if probe.mode == "repair" {
            return "修复或升级当前安装"
        }
        return "安装 Codex Remote"
    }

    private func primaryActionTitle(for probe: InstallerProbeResult) -> String {
        if probe.mode == "repair" {
            return (probe.sameVersion ?? false) ? "开始重装修复" : "开始升级"
        }
        return "开始安装"
    }

    private func screenSummary(for probe: InstallerProbeResult) -> String {
        if probe.mode == "repair" {
            if probe.sameVersion ?? false {
                return "检测到当前用户环境中已经安装了相同版本。本次会重装修复当前安装，并重新启动后台服务。"
            }
            let currentVersion = probe.currentVersion ?? "当前版本"
            let installerVersion = probe.installerVersion ?? "新版本"
            return "检测到已有安装。本次会把 \(currentVersion) 升级为 \(installerVersion)，并复用现有配置与服务语义。"
        }
        return "这会把 Codex Remote 安装到当前用户环境，不会写入 system-wide 目录，也不会要求 root。"
    }
}

private final class OutputBuffer {
    private let queue = DispatchQueue(label: "com.kxn.codex-remote.installer.output-buffer")
    private var output: String = ""
    private let onOutput: (String) -> Void

    init(onOutput: @escaping (String) -> Void) {
        self.onOutput = onOutput
    }

    func consume(_ data: Data) {
        guard !data.isEmpty, let text = String(data: data, encoding: .utf8), !text.isEmpty else {
            return
        }
        queue.sync {
            output.append(text)
        }
        DispatchQueue.main.async {
            self.onOutput(text)
        }
    }

    func snapshot() -> String {
        queue.sync {
            output
        }
    }
}
