import type { Dispatch, SetStateAction } from "react";
import { StatusBadge } from "../../components/ui";
import type {
  AutostartDetectResponse,
  FeishuAppSummary,
  FeishuManifest,
  FeishuOnboardingCompleteResponse,
  FeishuOnboardingSession,
  RuntimeRequirementsDetectResponse,
  VSCodeDetectResponse,
} from "../../lib/types";
import type { FeishuConnectMode, FeishuConnectStage, SetupDraft, StepID } from "./types";
import { feishuAppConsoleURL } from "./helpers";
import type { VSCodeUsageScenario } from "../shared/helpers";
import { FeishuConnectStep } from "./FeishuConnectStep";

type SetupStepContentProps = {
  currentStep: StepID;
  apps: FeishuAppSummary[];
  activeApp: FeishuAppSummary | null;
  manifest: FeishuManifest;
  draft: SetupDraft;
  connectStage: FeishuConnectStage;
  connectMode: FeishuConnectMode | null;
  onboardingSession: FeishuOnboardingSession | null;
  onboardingCompletion: FeishuOnboardingCompleteResponse | null;
  onboardingNeedsManualRetry: boolean;
  scopesJSON: string;
  permissionsConfirmed: boolean;
  eventsConfirmed: boolean;
  longConnectionConfirmed: boolean;
  menusConfirmed: boolean;
  runtimeRequirements: RuntimeRequirementsDetectResponse | null;
  runtimeRequirementsError: string;
  autostart: AutostartDetectResponse | null;
  autostartError: string;
  autostartSummary: string;
  vscodeScenario: VSCodeUsageScenario | null;
  vscodeSummary: string;
  vscode: VSCodeDetectResponse | null;
  vscodeError: string;
  onDraftChange: Dispatch<SetStateAction<SetupDraft>>;
  onConnectModeChange: (value: FeishuConnectMode) => void;
  onContinueModeSelection: () => void;
  onVerifyManual: () => void;
  onBackToConnectModeSelection: () => void;
  onRefreshOnboarding: () => void;
  onRestartOnboarding: () => void;
  onSwitchToExistingFlow: () => void;
  onRetryOnboardingComplete: () => void;
  onContinueOnboardingNotice: () => void;
  onPermissionsConfirmedChange: (value: boolean) => void;
  onEventsConfirmedChange: (value: boolean) => void;
  onLongConnectionConfirmedChange: (value: boolean) => void;
  onMenusConfirmedChange: (value: boolean) => void;
  onVSCodeScenarioChange: (value: VSCodeUsageScenario) => void;
  onCopyScopes: () => void;
  busyAction: string;
};

type SetupStepPrimaryActionProps = {
  currentStep: StepID;
  busyAction: string;
  autostart: AutostartDetectResponse | null;
  canContinueVSCode: boolean;
  vscodePrimaryLabel: string;
  onStart: () => void;
  onTestAndContinue: () => void;
  onConfirmPermissions: () => void;
  onConfirmEvents: () => void;
  onConfirmLongConnection: () => void;
  onConfirmMenus: () => void;
  onCheckPublish: () => void;
  onCheckRuntimeRequirements: () => void;
  onContinueAutostart: () => void;
  onContinueVSCode: () => void;
  onFinishSetup: () => void;
};

type SetupStepSecondaryActionProps = {
  currentStep: StepID;
  busyAction: string;
  onCopyScopes: () => void;
  onSkipAutostart: () => void;
  onDeferVSCode: () => void;
};

export function SetupStepContent({
  currentStep,
  apps,
  activeApp,
  manifest,
  draft,
  connectStage,
  connectMode,
  onboardingSession,
  onboardingCompletion,
  onboardingNeedsManualRetry,
  scopesJSON,
  permissionsConfirmed,
  eventsConfirmed,
  longConnectionConfirmed,
  menusConfirmed,
  runtimeRequirements,
  runtimeRequirementsError,
  autostart,
  autostartError,
  autostartSummary,
  vscodeScenario,
  vscodeSummary,
  vscode,
  vscodeError,
  onDraftChange,
  onConnectModeChange,
  onContinueModeSelection,
  onVerifyManual,
  onBackToConnectModeSelection,
  onRefreshOnboarding,
  onRestartOnboarding,
  onSwitchToExistingFlow,
  onRetryOnboardingComplete,
  onContinueOnboardingNotice,
  onPermissionsConfirmedChange,
  onEventsConfirmedChange,
  onLongConnectionConfirmedChange,
  onMenusConfirmedChange,
  onVSCodeScenarioChange,
  onCopyScopes,
  busyAction,
}: SetupStepContentProps) {
  const vscodeBundleDetected = Boolean(vscode?.latestBundleEntrypoint || vscode?.recordedBundleEntrypoint || vscode?.candidateBundleEntrypoints?.length);

  switch (currentStep) {
    case "start":
      return (
        <div className="wizard-step-layout">
          <div className="wizard-callout">
            <h4>开始设置 Codex Remote</h4>
            <p>这是一套分步向导。你现在只需要先把一个能正常工作的飞书应用接上，后面的步骤会一页一页继续做。</p>
            <ul className="wizard-bullet-list">
              <li>先创建并连接飞书应用。</li>
              <li>再完成权限、事件、回调长连接、菜单和发布。</li>
              <li>发布后先做一次运行环境检查，再按需处理自动启动和 VS Code。</li>
            </ul>
          </div>
        </div>
      );
    case "connect":
      return (
        <FeishuConnectStep
          apps={apps}
          activeApp={activeApp}
          draft={draft}
          connectStage={connectStage}
          connectMode={connectMode}
          onboardingSession={onboardingSession}
          onboardingCompletion={onboardingCompletion}
          onboardingNeedsManualRetry={onboardingNeedsManualRetry}
          busyAction={busyAction}
          onNameChange={(value) => onDraftChange((current) => ({ ...current, name: value }))}
          onAppIDChange={(value) => onDraftChange((current) => ({ ...current, appId: value }))}
          onAppSecretChange={(value) => onDraftChange((current) => ({ ...current, appSecret: value }))}
          onConnectModeChange={onConnectModeChange}
          onContinueModeSelection={onContinueModeSelection}
          onVerifyManual={onVerifyManual}
          onBackToModeSelection={onBackToConnectModeSelection}
          onRefreshOnboarding={onRefreshOnboarding}
          onRestartOnboarding={onRestartOnboarding}
          onSwitchToExistingFlow={onSwitchToExistingFlow}
          onRetryOnboardingComplete={onRetryOnboardingComplete}
          onContinueOnboardingNotice={onContinueOnboardingNotice}
        />
      );
    case "permissions":
      return (
        <div className="wizard-step-layout">
          <div className="wizard-link-row">
            <a href={feishuAppConsoleURL(activeApp?.appId)} target="_blank" rel="noreferrer">
              打开当前应用后台
            </a>
            <span>打开后点击左侧“权限管理”。</span>
          </div>
          <div className="manifest-block">
            <h4>权限导入说明</h4>
            <ul className="wizard-bullet-list">
              <li>先点击“复制权限配置”。</li>
              <li>去飞书后台打开“批量导入/导出权限”。</li>
              <li>把下面这段 JSON 粘贴进去，然后点击“保存并申请开通”。</li>
              <li>保存完成后回到这里，再点“继续”。</li>
            </ul>
          </div>
          <textarea className="code-textarea" readOnly value={scopesJSON} />
          <div className="button-row">
            <button className="secondary-button" type="button" onClick={onCopyScopes} disabled={busyAction !== ""}>
              复制权限配置
            </button>
          </div>
          <label className="checkbox-card">
            <input type="checkbox" checked={permissionsConfirmed} onChange={(event) => onPermissionsConfirmedChange(event.target.checked)} />
            <div>
              <strong>我已经在飞书后台完成权限导入</strong>
              <p>飞书后台这个入口叫“批量导入/导出权限”。</p>
            </div>
          </label>
        </div>
      );
    case "events":
      return (
        <div className="wizard-step-layout">
          <div className="wizard-link-row">
            <a href={feishuAppConsoleURL(activeApp?.appId)} target="_blank" rel="noreferrer">
              打开当前应用后台
            </a>
            <span>打开后点击左侧“事件与回调”。</span>
          </div>
          <div className="manifest-block">
            <h4>先保存事件订阅方式</h4>
            <ul className="wizard-bullet-list">
              <li>在“事件与回调”页点击“订阅方式”。</li>
              <li>默认就是“长连接”，直接点击“保存”。</li>
            </ul>
          </div>
          <div className="manifest-block">
            <h4>按下面的事件列表完成订阅</h4>
            <p>保存订阅方式后，再把下面这些事件全部订阅进去并保存。卡片回调不在这里，完成后再去下一页配置回调订阅方式。</p>
          </div>
          <ul className="token-list">
            {manifest.events.map((item) => (
              <li key={item.event}>
                <code>{item.event}</code>
                <span>{item.purpose || "需要手工订阅"}</span>
              </li>
            ))}
          </ul>
          <label className="checkbox-card">
            <input type="checkbox" checked={eventsConfirmed} onChange={(event) => onEventsConfirmedChange(event.target.checked)} />
            <div>
              <strong>我已经完成事件订阅</strong>
              <p>事件列表要和页面展示一致，订阅方式也要保存为长连接。</p>
            </div>
          </label>
        </div>
      );
    case "longConnection":
      return (
        <div className="wizard-step-layout">
          <div className="wizard-link-row">
            <a href={feishuAppConsoleURL(activeApp?.appId)} target="_blank" rel="noreferrer">
              打开当前应用后台
            </a>
            <span>打开后点击左侧“事件与回调”。</span>
          </div>
          <div className="manifest-block">
            <h4>回调配置这一步怎么做</h4>
            <ul className="wizard-bullet-list">
              <li>在同一个“事件与回调”页面里找到“回调配置”。</li>
              <li>点击“回调订阅方式”。</li>
              <li>选择“长连接”，然后点击“保存”。</li>
              <li>这里不需要填写 HTTP 回调 URL。</li>
              <li>再把下面这些回调项按页面说明配置完成。</li>
            </ul>
          </div>
          <div className="manifest-block">
            <h4>当前需要的回调项</h4>
            <p>这些回调项走回调 / 长连接配置语义，不和上一页的普通事件订阅混在一起。</p>
          </div>
          <ul className="token-list">
            {manifest.callbacks.map((item) => (
              <li key={item.callback}>
                <code>{item.callback}</code>
                <span>{item.purpose || "需要手工配置回调。"}</span>
              </li>
            ))}
          </ul>
          <div className="manifest-block">
            <h4>这一步为什么重要</h4>
            <ul className="wizard-bullet-list">
              <li>approval request 等卡片按钮要靠回调长连接进入服务。</li>
              <li>如果这里没配好，用户点卡片会没有反应。</li>
            </ul>
          </div>
          <label className="checkbox-card">
            <input type="checkbox" checked={longConnectionConfirmed} onChange={(event) => onLongConnectionConfirmedChange(event.target.checked)} />
            <div>
              <strong>我已经完成回调长连接配置</strong>
              <p>确认回调订阅方式已经保存为长连接，不填写 HTTP 回调 URL。</p>
            </div>
          </label>
        </div>
      );
    case "menus":
      return (
        <div className="wizard-step-layout">
          <div className="wizard-link-row">
            <a href={feishuAppConsoleURL(activeApp?.appId)} target="_blank" rel="noreferrer">
              打开当前应用后台
            </a>
            <span>打开后点击左侧“机器人”，进入自定义菜单区域。</span>
          </div>
          <div className="manifest-block">
            <h4>这些菜单 key 会真正生效</h4>
            <p>菜单的 key 必须和下面保持一致，否则用户点击后当前服务收不到正确事件。</p>
          </div>
          <ul className="token-list">
            {manifest.menus.map((item) => (
              <li key={item.key}>
                <code>{item.key}</code>
                <strong>{item.name}</strong>
                <span>{item.description || "当前实现会处理这个菜单事件。"}</span>
              </li>
            ))}
          </ul>
          <label className="checkbox-card">
            <input type="checkbox" checked={menusConfirmed} onChange={(event) => onMenusConfirmedChange(event.target.checked)} />
            <div>
              <strong>我已经完成菜单配置</strong>
              <p>请再次确认所有 key 和页面展示完全一致。</p>
            </div>
          </label>
        </div>
      );
    case "publish":
      return (
        <div className="wizard-step-layout">
          <div className="wizard-link-row">
            <a href={feishuAppConsoleURL(activeApp?.appId)} target="_blank" rel="noreferrer">
              打开当前应用后台
            </a>
            <span>打开后点击左侧“版本管理与发布”。</span>
          </div>
          <div className="manifest-block">
            <h4>这一步必须真的发版</h4>
            <ul className="wizard-bullet-list">
              <li>前面的权限、事件、回调长连接、菜单都只是配置准备。</li>
              <li>只有在飞书后台真正发版后，这些变更才会生效。</li>
              <li>发版完成以后，再回来点击“检查并继续”。</li>
            </ul>
          </div>
        </div>
      );
    case "runtimeRequirements":
      return (
        <div className="wizard-step-layout">
          {runtimeRequirementsError ? <div className="notice-banner warn">运行环境检查暂时不可用：{runtimeRequirementsError}</div> : null}
          {!runtimeRequirements && !runtimeRequirementsError ? <div className="notice-banner warn">当前还没拿到运行环境检查结果，请先刷新状态后再继续。</div> : null}
          {runtimeRequirements ? (
            <>
              <div className={`notice-banner ${runtimeRequirements.ready ? (runtimeRequirements.checks.some((check) => check.status === "warn") ? "warn" : "good") : "danger"}`}>
                {runtimeRequirements.summary}
              </div>
              <div className="manifest-block">
                <h4>这一步在检查什么</h4>
                <ul className="wizard-bullet-list">
                  <li>当前 daemon 有没有可用的 headless 启动器。</li>
                  <li>wrapper 实际将要启动哪个真实 <code>codex</code>。</li>
                  <li>当前服务环境里能不能把它解析成可执行文件。</li>
                  <li>是否存在明显配置风险，例如回指自身或只靠 PATH 解析。</li>
                </ul>
              </div>
              <div className="wizard-summary-grid">
                <div className="wizard-summary-card">
                  <strong>当前 codex-remote</strong>
                  <p>{runtimeRequirements.currentBinary || "未检测到"}</p>
                </div>
                <div className="wizard-summary-card">
                  <strong>配置的真实 Codex</strong>
                  <p>{runtimeRequirements.codexRealBinary || "未配置"}</p>
                </div>
                <div className="wizard-summary-card">
                  <strong>配置来源</strong>
                  <p>{runtimeRequirementSourceLabel(runtimeRequirements.codexRealBinarySource)}</p>
                </div>
                <div className="wizard-summary-card">
                  <strong>实际解析结果</strong>
                  <p>{runtimeRequirements.resolvedCodexRealBinary || "当前不可解析"}</p>
                </div>
              </div>
              <div className="checkbox-card-list">
                {runtimeRequirements.checks.map((check) => (
                  <div key={check.id} className="checkbox-card">
                    <StatusBadge value={runtimeRequirementStatusLabel(check.status)} tone={runtimeRequirementStatusTone(check.status)} />
                    <div>
                      <strong>{check.title}</strong>
                      <p>{check.summary}</p>
                      {check.detail ? (
                        <p>
                          <code>{check.detail}</code>
                        </p>
                      ) : null}
                    </div>
                  </div>
                ))}
              </div>
              {runtimeRequirements.notes?.length ? (
                <div className="manifest-block">
                  <h4>当前边界</h4>
                  <ul className="wizard-bullet-list">
                    {runtimeRequirements.notes.map((note) => (
                      <li key={note}>{note}</li>
                    ))}
                  </ul>
                </div>
              ) : null}
            </>
          ) : null}
        </div>
      );
    case "autostart":
      return (
        <div className="wizard-step-layout">
          {autostartError ? <div className="notice-banner warn">自动启动状态暂时不可用：{autostartError}</div> : null}
          {!autostart && !autostartError ? <div className="notice-banner warn">当前还没拿到自动启动检测结果，请先刷新状态后再继续。</div> : null}
          {autostart ? (
            <>
              {autostart.supported ? (
                <>
                  <div className="manifest-block">
                    <h4>当前平台支持自动启动</h4>
                    <p>Linux 侧当前已接入的是 <code>systemd --user</code> 这条路径。启用后，会在当前用户登录后自动拉起 codex-remote。</p>
                  </div>
                  <div className="manifest-block">
                    <h4>当前状态</h4>
                    <p>{autostartSummary}</p>
                    <ul className="wizard-bullet-list">
                      <li>这一步只处理当前登录用户的自动启动。</li>
                      <li>当前 issue 不会把未登录恢复建模成第二种 service manager。</li>
                    </ul>
                  </div>
                  {autostart.warning ? <div className="notice-banner warn">自动启动检测提示：{autostart.warning}</div> : null}
                  {autostart.lingerHint ? <div className="notice-banner neutral">{autostart.lingerHint}</div> : null}
                </>
              ) : (
                <div className="manifest-block">
                  <h4>当前平台暂不支持自动启动</h4>
                  <p>当前 setup 只接入 Linux 的 <code>systemd --user</code> 路径。macOS 和 Windows 先只展示状态，不提供可操作入口。</p>
                </div>
              )}
              <details className="wizard-tech-detail">
                <summary>查看技术详情</summary>
                <div className="wizard-tech-grid">
                  <div>
                    <strong>Platform</strong>
                    <p>{autostart.platform}</p>
                  </div>
                  <div>
                    <strong>Manager</strong>
                    <p>{autostart.manager || "not available"}</p>
                  </div>
                  <div>
                    <strong>Current Manager</strong>
                    <p>{autostart.currentManager || "detached"}</p>
                  </div>
                  <div>
                    <strong>Status</strong>
                    <p>{autostart.status}</p>
                  </div>
                  <div>
                    <strong>State Path</strong>
                    <p>{autostart.installStatePath || "not recorded"}</p>
                  </div>
                  <div>
                    <strong>Unit Path</strong>
                    <p>{autostart.serviceUnitPath || "not configured"}</p>
                  </div>
                </div>
              </details>
            </>
          ) : null}
        </div>
      );
    case "vscode":
      return (
        <div className="wizard-step-layout">
          <div className="manifest-block">
            <h4>不使用 VS Code 可以直接跳过</h4>
            <p>这一步只在你准备使用 VS Code 里的 Codex 时才需要处理。不用 VS Code 的话，可以直接点底部的“跳过 VS Code”。</p>
          </div>
          {vscode?.sshSession ? (
            <>
              <div className="manifest-block">
                <h4>检测到当前是远程 SSH 机器</h4>
                <p>你现在是在被 VS Code Remote SSH 连接的机器上完成设置。这个场景下，需要直接接管这台机器上的 VS Code 扩展入口。</p>
              </div>
              <div className="manifest-block">
                <h4>推荐操作</h4>
                <p>我们会把这台机器上的 VS Code 扩展入口接到 codex-remote。这不会去写 host 机器的 settings.json。</p>
                <ul className="wizard-bullet-list">
                  <li>适合当前远程 VS Code 场景。</li>
                  <li>后续如果扩展升级，回到管理页重新安装扩展入口即可。</li>
                </ul>
              </div>
              {!vscodeBundleDetected ? (
                <div className="notice-banner warn">还没检测到这台机器上的 VS Code 扩展。请先在这台机器上打开一次 VS Code Remote 窗口，并确保 Codex 扩展已经安装，然后再回来继续。</div>
              ) : null}
            </>
          ) : (
            <>
              <div className="manifest-block">
                <h4>你以后主要怎么使用 VS Code 里的 Codex？</h4>
                <p>先确认当前这台机器是否需要接入。只要这台机器要用 VS Code，就统一只处理扩展入口，不再写 settings.json。</p>
              </div>
              <div className="choice-card-list" role="radiogroup" aria-label="VS Code 使用场景">
                <label className={`choice-card${vscodeScenario === "current_machine" ? " selected" : ""}`}>
                  <input type="radio" name="vscode-usage-scenario" checked={vscodeScenario === "current_machine"} onChange={() => onVSCodeScenarioChange("current_machine")} />
                  <div>
                    <strong>要在当前这台机器上使用</strong>
                    <p>无论是本地 VS Code，还是这台机器被 Remote SSH 连接，都统一只处理扩展入口。</p>
                  </div>
                </label>
                <label className={`choice-card${vscodeScenario === "remote_only" ? " selected" : ""}`}>
                  <input type="radio" name="vscode-usage-scenario" checked={vscodeScenario === "remote_only"} onChange={() => onVSCodeScenarioChange("remote_only")} />
                  <div>
                    <strong>主要去别的 SSH 机器上使用</strong>
                    <p>当前机器先不做 VS Code 接入，避免 host 设置影响远端。</p>
                  </div>
                </label>
              </div>
              {vscodeScenario === "current_machine" ? (
                <div className="manifest-block">
                  <h4>当前策略：只处理扩展入口</h4>
                  <p>这条路径不会写本机 settings.json，因此不会再把 host 机器上的客户端 override 带进远端会话。</p>
                  <p>如果扩展升级导致入口失效，回来重新安装扩展入口即可。</p>
                </div>
              ) : null}
              {vscodeScenario === "remote_only" ? (
                <div className="manifest-block">
                  <h4>当前这台机器先不用接入</h4>
                  <p>如果你主要是在别的 SSH 机器上使用 VS Code Codex，真正需要安装和接入的是目标远程机器，而不是当前这台本机。</p>
                  <ul className="wizard-bullet-list">
                    <li>当前机器不写 settings.json。</li>
                    <li>避免 host 设置影响以后连接到远程机器。</li>
                    <li>去目标机器安装 codex-remote 后，再在目标机器上完成这一步。</li>
                  </ul>
                </div>
              ) : null}
              {vscodeScenario === "current_machine" && !vscodeBundleDetected ? (
                <div className="notice-banner warn">还没检测到这台机器上的 VS Code 扩展安装。请先在这台机器上打开一次 VS Code，并确保 Codex 扩展已经安装，然后再回来继续。</div>
              ) : null}
            </>
          )}
          {vscodeError ? <div className="notice-banner warn">VS Code 检测暂时不可用：{vscodeError}</div> : null}
          {!vscode && !vscodeError ? <div className="notice-banner warn">当前还没拿到 VS Code 检测结果，请先刷新状态后再继续。</div> : null}
          {vscode ? (
            <details className="wizard-tech-detail">
              <summary>查看技术详情</summary>
              <div className="wizard-tech-grid">
                <div>
                  <strong>Recommended</strong>
                  <p>{vscode.recommendedMode}</p>
                </div>
                <div>
                  <strong>Current Mode</strong>
                  <p>{vscode.currentMode}</p>
                </div>
                <div>
                  <strong>Settings</strong>
                  <p>{vscode.settings.path || "unavailable"}</p>
                </div>
                <div>
                  <strong>Latest Bundle</strong>
                  <p>{vscode.latestBundleEntrypoint || "not detected"}</p>
                </div>
                <div>
                  <strong>Recorded Bundle</strong>
                  <p>{vscode.recordedBundleEntrypoint || "not recorded"}</p>
                </div>
                <div>
                  <strong>Needs Reinstall</strong>
                  <p>{vscode.needsShimReinstall ? "yes" : "no"}</p>
                </div>
              </div>
            </details>
          ) : null}
        </div>
      );
    case "finish":
      return (
        <div className="wizard-step-layout">
          <div className="manifest-block">
            <h4>现在你可以开始第一次对话</h4>
            <ul className="wizard-bullet-list">
              <li>推荐先在飞书里打开“开发者小助手”。</li>
              <li>找到刚完成发布或审批通过的应用。</li>
              <li>点击“打开应用”后，先给机器人发一条测试消息完成第一次私聊。</li>
              <li>如果你的工作台已经能看到该应用，也可以直接从工作台进入。</li>
            </ul>
          </div>
          <div className="wizard-summary-grid">
            <div className="wizard-summary-card">
              <strong>飞书应用</strong>
              <p>{activeApp?.name || activeApp?.id || "未命名应用"}</p>
            </div>
            <div className="wizard-summary-card">
              <strong>平台配置</strong>
              <p>权限、事件、回调长连接、菜单、发布均已完成。</p>
            </div>
            <div className="wizard-summary-card">
              <strong>自动启动</strong>
              <p>{autostartSummary}</p>
            </div>
            <div className="wizard-summary-card">
              <strong>VS Code</strong>
              <p>{vscodeSummary}</p>
            </div>
          </div>
        </div>
      );
    default:
      return null;
  }
}

export function SetupStepPrimaryAction({
  currentStep,
  busyAction,
  autostart,
  canContinueVSCode,
  vscodePrimaryLabel,
  onStart,
  onTestAndContinue,
  onConfirmPermissions,
  onConfirmEvents,
  onConfirmLongConnection,
  onConfirmMenus,
  onCheckPublish,
  onCheckRuntimeRequirements,
  onContinueAutostart,
  onContinueVSCode,
  onFinishSetup,
}: SetupStepPrimaryActionProps) {
  switch (currentStep) {
    case "start":
      return (
        <button className="primary-button" type="button" onClick={onStart} disabled={busyAction !== ""}>
          开始
        </button>
      );
    case "connect":
      return null;
    case "permissions":
      return (
        <button className="primary-button" type="button" onClick={onConfirmPermissions} disabled={busyAction !== ""}>
          继续
        </button>
      );
    case "events":
      return (
        <button className="primary-button" type="button" onClick={onConfirmEvents} disabled={busyAction !== ""}>
          继续
        </button>
      );
    case "longConnection":
      return (
        <button className="primary-button" type="button" onClick={onConfirmLongConnection} disabled={busyAction !== ""}>
          继续
        </button>
      );
    case "menus":
      return (
        <button className="primary-button" type="button" onClick={onConfirmMenus} disabled={busyAction !== ""}>
          继续
        </button>
      );
    case "publish":
      return (
        <button className="primary-button" type="button" onClick={onCheckPublish} disabled={busyAction !== ""}>
          检查并继续
        </button>
      );
    case "runtimeRequirements":
      return (
        <button className="primary-button" type="button" onClick={onCheckRuntimeRequirements} disabled={busyAction !== ""}>
          {busyAction === "runtime-requirements-detect" ? "正在检查..." : "检查并继续"}
        </button>
      );
    case "autostart":
      return (
        <button className="primary-button" type="button" onClick={onContinueAutostart} disabled={busyAction !== ""}>
          {busyAction === "autostart-apply"
            ? "正在启用..."
            : !autostart?.supported || autostart.status === "enabled"
              ? "继续"
              : "启用自动启动"}
        </button>
      );
    case "vscode":
      return (
        <button className="primary-button" type="button" onClick={onContinueVSCode} disabled={busyAction !== "" || !canContinueVSCode}>
          {vscodePrimaryLabel}
        </button>
      );
    case "finish":
      return (
        <button className="primary-button" type="button" onClick={onFinishSetup} disabled={busyAction !== ""}>
          完成并进入本地管理页
        </button>
      );
    default:
      return null;
  }
}

export function SetupStepSecondaryAction({ currentStep, busyAction, onCopyScopes, onSkipAutostart, onDeferVSCode }: SetupStepSecondaryActionProps) {
  if (currentStep === "connect") {
    return null;
  }
  if (currentStep === "autostart") {
    return (
      <button className="secondary-button" type="button" onClick={onSkipAutostart} disabled={busyAction !== ""}>
        跳过这一步
      </button>
    );
  }
  if (currentStep === "vscode") {
    return (
      <button className="secondary-button" type="button" onClick={onDeferVSCode} disabled={busyAction !== ""}>
        跳过 VS Code
      </button>
    );
  }
  if (currentStep === "permissions") {
    return (
      <button className="secondary-button" type="button" onClick={onCopyScopes} disabled={busyAction !== ""}>
        复制权限配置
      </button>
    );
  }
  return null;
}

function runtimeRequirementStatusTone(status: string): "neutral" | "good" | "warn" | "danger" {
  switch (status) {
    case "pass":
      return "good";
    case "warn":
      return "warn";
    case "fail":
      return "danger";
    default:
      return "neutral";
  }
}

function runtimeRequirementStatusLabel(status: string): string {
  switch (status) {
    case "pass":
      return "通过";
    case "warn":
      return "注意";
    case "fail":
      return "阻断";
    default:
      return "信息";
  }
}

function runtimeRequirementSourceLabel(source?: string): string {
  switch (source) {
    case "env_override":
      return "环境变量覆盖";
    case "config":
      return "配置文件";
    case "default":
      return "默认值";
    default:
      return "未说明";
  }
}
