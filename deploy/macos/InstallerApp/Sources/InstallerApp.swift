import Cocoa

@main
enum InstallerLauncher {
    static func main() {
        let app = NSApplication.shared
        let delegate = InstallerAppDelegate()
        app.delegate = delegate
        app.setActivationPolicy(.regular)
        app.activate(ignoringOtherApps: true)
        app.run()
    }
}

final class InstallerAppDelegate: NSObject, NSApplicationDelegate {
    private var window: NSWindow?

    func applicationDidFinishLaunching(_ notification: Notification) {
        let controller = InstallerViewController()
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 760, height: 620),
            styleMask: [.titled, .closable, .miniaturizable],
            backing: .buffered,
            defer: false
        )
        window.title = "Install Codex Remote"
        window.center()
        window.isReleasedWhenClosed = false
        window.contentViewController = controller
        window.makeKeyAndOrderFront(nil)
        self.window = window
        NSApp.activate(ignoringOtherApps: true)
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        true
    }
}

final class InstallerViewController: NSViewController {
    private let bridge = InstallerBridge()

    private var plan: InstallerLaunchPlan?
    private var screenState: ScreenState = .loading

    private let stepLabel = NSTextField(labelWithString: "")
    private let titleLabel = NSTextField(labelWithString: "")
    private let summaryLabel = NSTextField(wrappingLabelWithString: "")
    private let detailLabel = NSTextField(wrappingLabelWithString: "")
    private let locationTitleLabel = NSTextField(labelWithString: "安装位置")
    private let locationField = NSTextField(string: "")
    private let locationHintLabel = NSTextField(wrappingLabelWithString: "")
    private lazy var browseButton: NSButton = {
        let button = NSButton(title: "选择...", target: self, action: #selector(selectLocation))
        button.bezelStyle = .rounded
        return button
    }()
    private let progressIndicator = NSProgressIndicator()
    private let logTextView = NSTextView()
    private let logScrollView = NSScrollView()
    private lazy var primaryButton: NSButton = {
        let button = NSButton(title: "继续", target: self, action: #selector(primaryAction))
        button.bezelStyle = .rounded
        button.keyEquivalent = "\r"
        return button
    }()
    private lazy var secondaryButton: NSButton = {
        let button = NSButton(title: "取消", target: self, action: #selector(secondaryAction))
        button.bezelStyle = .rounded
        return button
    }()

    override func loadView() {
        view = NSView()
        view.wantsLayer = true
        view.layer?.backgroundColor = NSColor.windowBackgroundColor.cgColor
        setupUI()
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        startProbe()
    }

    @objc private func primaryAction() {
        switch screenState {
        case .ready:
            beginInstall()
        case .result(let model):
            performPrimaryResultAction(model.primaryAction)
        case .loading, .installing:
            break
        }
    }

    @objc private func secondaryAction() {
        switch screenState {
        case .installing:
            break
        default:
            NSApp.terminate(nil)
        }
    }

    @objc private func selectLocation() {
        guard let plan, plan.installLocationEditable else {
            return
        }
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.prompt = "选择安装目录"
        if panel.runModal() == .OK, let url = panel.url {
            locationField.stringValue = url.path
        }
    }

    private func startProbe() {
        screenState = .loading
        render()
        DispatchQueue.global(qos: .userInitiated).async {
            do {
                let plan = try self.bridge.probe()
                DispatchQueue.main.async {
                    self.plan = plan
                    self.locationField.stringValue = plan.defaultInstallDir
                    self.screenState = .ready
                    self.render()
                }
            } catch {
                DispatchQueue.main.async {
                    self.plan = nil
                    self.screenState = .result(InstallerResultPageModel.fromFailure(
                        message: error.localizedDescription,
                        detail: "安装器暂时无法完成当前环境探测。请确认嵌入 payload 可执行、版本资源存在，并查看下方过程日志。",
                        logPath: ""
                    ))
                    self.render()
                }
            }
        }
    }

    private func beginInstall() {
        guard let plan else {
            return
        }
        screenState = .installing
        clearLog()
        render()
        appendLog("Starting install with mode: \(plan.probe.mode)\n")
        let request = InstallerExecutionRequest(
            probe: plan.probe,
            installBinDir: locationField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        )
        bridge.runInstall(
            request: request,
            onOutput: { [weak self] text in
                self?.appendLog(text)
            },
            completion: { [weak self] result in
                guard let self else { return }
                switch result {
                case .success(let summary):
                    if summary.result.ok {
                        self.screenState = .result(InstallerResultPageModel.fromSuccess(
                            probe: plan.probe,
                            result: summary.result
                        ))
                    } else {
                        self.screenState = .result(self.failureResultModel(from: summary))
                    }
                    self.render()
                case .failure(let error):
                    self.screenState = .result(InstallerResultPageModel.fromFailure(
                        message: error.localizedDescription,
                        detail: "安装器没有拿到有效结果文件。请检查嵌入 payload 的启动错误，或查看下方过程日志。",
                        logPath: ""
                    ))
                    self.render()
                }
            }
        )
    }

    private func setupUI() {
        let contentStack = NSStackView()
        contentStack.translatesAutoresizingMaskIntoConstraints = false
        contentStack.orientation = .vertical
        contentStack.alignment = .leading
        contentStack.spacing = 16

        stepLabel.font = .systemFont(ofSize: 12, weight: .medium)
        stepLabel.textColor = .secondaryLabelColor

        titleLabel.font = .systemFont(ofSize: 28, weight: .semibold)
        titleLabel.lineBreakMode = .byWordWrapping

        summaryLabel.font = .systemFont(ofSize: 14, weight: .regular)
        summaryLabel.textColor = .secondaryLabelColor
        summaryLabel.maximumNumberOfLines = 0

        detailLabel.font = .systemFont(ofSize: 13, weight: .regular)
        detailLabel.textColor = .secondaryLabelColor
        detailLabel.maximumNumberOfLines = 0

        locationTitleLabel.font = .systemFont(ofSize: 13, weight: .semibold)
        locationField.isEditable = false
        locationHintLabel.font = .systemFont(ofSize: 12, weight: .regular)
        locationHintLabel.textColor = .secondaryLabelColor
        locationHintLabel.maximumNumberOfLines = 0

        let locationRow = NSStackView(views: [locationField, browseButton])
        locationRow.orientation = .horizontal
        locationRow.spacing = 10
        locationRow.translatesAutoresizingMaskIntoConstraints = false
        locationField.translatesAutoresizingMaskIntoConstraints = false
        browseButton.translatesAutoresizingMaskIntoConstraints = false

        progressIndicator.style = .spinning
        progressIndicator.controlSize = .regular
        progressIndicator.isDisplayedWhenStopped = false

        logTextView.isEditable = false
        logTextView.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
        logTextView.backgroundColor = NSColor.textBackgroundColor
        logScrollView.documentView = logTextView
        logScrollView.hasVerticalScroller = true
        logScrollView.borderType = .bezelBorder
        logScrollView.translatesAutoresizingMaskIntoConstraints = false
        logScrollView.heightAnchor.constraint(equalToConstant: 220).isActive = true

        let buttonRow = NSStackView(views: [secondaryButton, primaryButton])
        buttonRow.orientation = .horizontal
        buttonRow.spacing = 10
        buttonRow.alignment = .centerY

        for viewItem in [
            stepLabel,
            titleLabel,
            summaryLabel,
            locationTitleLabel,
            locationRow,
            locationHintLabel,
            progressIndicator,
            detailLabel,
            logScrollView,
            buttonRow,
        ] {
            contentStack.addArrangedSubview(viewItem)
        }

        view.addSubview(contentStack)
        NSLayoutConstraint.activate([
            contentStack.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 28),
            contentStack.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -28),
            contentStack.topAnchor.constraint(equalTo: view.topAnchor, constant: 28),
            contentStack.bottomAnchor.constraint(lessThanOrEqualTo: view.bottomAnchor, constant: -24),
            locationField.widthAnchor.constraint(greaterThanOrEqualToConstant: 420),
        ])
    }

    private func render() {
        switch screenState {
        case .loading:
            stepLabel.stringValue = "Preparing"
            titleLabel.stringValue = "正在检查当前安装状态"
            summaryLabel.stringValue = "安装器会先探测当前用户环境中的安装状态，再决定是首次安装还是修复 / 升级。"
            detailLabel.stringValue = ""
            locationTitleLabel.isHidden = true
            locationField.isHidden = true
            browseButton.isHidden = true
            locationHintLabel.isHidden = true
            progressIndicator.startAnimation(nil)
            logScrollView.isHidden = true
            primaryButton.isEnabled = false
            primaryButton.title = "继续"
            secondaryButton.title = "取消"
            secondaryButton.isEnabled = true
        case .ready:
            guard let plan else { return }
            stepLabel.stringValue = "Review"
            titleLabel.stringValue = plan.title
            summaryLabel.stringValue = plan.summary
            detailLabel.stringValue = detailText(for: plan)
            locationTitleLabel.isHidden = false
            locationField.isHidden = false
            browseButton.isHidden = !plan.installLocationEditable
            locationHintLabel.isHidden = false
            locationField.isEditable = plan.installLocationEditable
            locationField.isEnabled = true
            locationHintLabel.stringValue = plan.installLocationEditable
                ? "首次安装时可选择目标目录。现有安装的修复 / 升级会锁定当前 live binary 目录。"
                : "当前检测到已有安装。本次会复用当前安装目录，不能在修复 / 升级时切换 live binary 位置。"
            progressIndicator.stopAnimation(nil)
            logScrollView.isHidden = true
            primaryButton.isEnabled = true
            primaryButton.title = plan.primaryActionTitle
            secondaryButton.title = "取消"
            secondaryButton.isEnabled = true
        case .installing:
            stepLabel.stringValue = "Installing"
            titleLabel.stringValue = "正在安装"
            summaryLabel.stringValue = "安装器正在调用嵌入的 Codex Remote payload，并根据当前安装状态执行首装、升级或重装修复。"
            detailLabel.stringValue = "如果你正在修复已有安装，当前目录和服务状态会被自动复用。"
            locationTitleLabel.isHidden = true
            locationField.isHidden = true
            browseButton.isHidden = true
            locationHintLabel.isHidden = true
            progressIndicator.startAnimation(nil)
            logScrollView.isHidden = false
            primaryButton.isEnabled = false
            primaryButton.title = "安装中..."
            secondaryButton.title = "安装中..."
            secondaryButton.isEnabled = false
        case .result(let model):
            stepLabel.stringValue = model.stepText
            titleLabel.stringValue = model.title
            summaryLabel.stringValue = model.summary
            detailLabel.stringValue = resultDetailText(for: model)
            locationTitleLabel.isHidden = true
            locationField.isHidden = true
            browseButton.isHidden = true
            locationHintLabel.isHidden = true
            progressIndicator.stopAnimation(nil)
            logScrollView.isHidden = false
            primaryButton.isEnabled = true
            primaryButton.title = model.primaryAction.title
            secondaryButton.title = "关闭"
            secondaryButton.isEnabled = true
        }
    }

    private func detailText(for plan: InstallerLaunchPlan) -> String {
        var lines: [String] = []
        if let currentVersion = plan.probe.currentVersion, !currentVersion.isEmpty {
            lines.append("当前已安装版本：\(currentVersion)")
        }
        lines.append("安装器版本：\(plan.installerVersion)")
        let startupMode = installerFriendlyStartupModeLabel(plan.probe.startupMode, serviceManager: plan.probe.serviceManager)
        if !startupMode.isEmpty {
            let prefix = plan.probe.mode == "repair" ? "当前启动方式" : "安装后启动方式"
            lines.append("\(prefix)：\(startupMode)")
        }
        return lines.joined(separator: "\n")
    }

    private func resultDetailText(for model: InstallerResultPageModel) -> String {
        var lines: [String] = []
        if !model.detail.isEmpty {
            lines.append(model.detail)
        }
        lines.append(contentsOf: model.infoItems.map { "\($0.label)：\($0.value)" })
        return lines.joined(separator: "\n")
    }

    private func failureResultModel(from summary: InstallerExecutionSummary) -> InstallerResultPageModel {
        InstallerResultPageModel.fromFailure(
            message: summary.result.error.isEmpty ? "安装失败" : summary.result.error,
            detail: "你可以查看下方日志后重试。若问题持续存在，优先关注 result-file 返回的错误和 daemon 日志路径。",
            logPath: summary.result.logPath
        )
    }

    private func performPrimaryResultAction(_ action: InstallerResultPageAction) {
        switch action.kind {
        case .continueWebSetup, .openAdminUI:
            guard let target = action.target else {
                NSApp.terminate(nil)
                return
            }
            bridge.openURL(target)
            NSApp.terminate(nil)
        case .openLogs, .finish:
            NSApp.terminate(nil)
        }
    }

    private func appendLog(_ text: String) {
        guard !text.isEmpty else { return }
        let attributed = NSAttributedString(string: text)
        logTextView.textStorage?.append(attributed)
        logTextView.scrollToEndOfDocument(nil)
    }

    private func clearLog() {
        logTextView.string = ""
    }
}
