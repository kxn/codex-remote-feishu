import type { Dispatch, SetStateAction } from "react";
import type { FeishuAppSummary, FeishuManifest, VSCodeDetectResponse } from "../../lib/types";
import { FeishuAppFields } from "../shared/FeishuAppFields";
import type { SetupDraft, StepID } from "./types";
import { feishuAppConsoleURL } from "./helpers";
import type { VSCodeUsageScenario } from "../shared/helpers";

type SetupStepContentProps = {
  currentStep: StepID;
  apps: FeishuAppSummary[];
  activeApp: FeishuAppSummary | null;
  manifest: FeishuManifest;
  draft: SetupDraft;
  scopesJSON: string;
  permissionsConfirmed: boolean;
  eventsConfirmed: boolean;
  longConnectionConfirmed: boolean;
  menusConfirmed: boolean;
  vscodeScenario: VSCodeUsageScenario | null;
  vscodeSummary: string;
  vscode: VSCodeDetectResponse | null;
  vscodeError: string;
  onDraftChange: Dispatch<SetStateAction<SetupDraft>>;
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
  canContinueVSCode: boolean;
  vscodePrimaryLabel: string;
  onStart: () => void;
  onTestAndContinue: () => void;
  onConfirmPermissions: () => void;
  onConfirmEvents: () => void;
  onConfirmLongConnection: () => void;
  onConfirmMenus: () => void;
  onCheckPublish: () => void;
  onContinueVSCode: () => void;
  onFinishSetup: () => void;
};

type SetupStepSecondaryActionProps = {
  currentStep: StepID;
  busyAction: string;
  onCopyScopes: () => void;
  onDeferVSCode: () => void;
};

export function SetupStepContent({
  currentStep,
  apps,
  activeApp,
  manifest,
  draft,
  scopesJSON,
  permissionsConfirmed,
  eventsConfirmed,
  longConnectionConfirmed,
  menusConfirmed,
  vscodeScenario,
  vscodeSummary,
  vscode,
  vscodeError,
  onDraftChange,
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
              <li>最后按需配置 VS Code 集成。</li>
            </ul>
          </div>
        </div>
      );
    case "connect":
      return (
        <div className="wizard-step-layout two-column">
          <FeishuAppFields
            className="wizard-form-stack"
            notices={[
              ...(apps.length > 1 ? [{ tone: "warn" as const, message: "当前 setup 只继续处理一个应用。更多应用的新增、切换和运行管理请到本地管理页进行。" }] : []),
              ...(activeApp?.readOnly
                ? [{ tone: "warn" as const, message: "当前应用由运行时环境变量接管，setup 页面会直接对它做连接测试，但不会修改本地配置。" }]
                : []),
            ]}
            values={draft}
            readOnly={Boolean(activeApp?.readOnly)}
            hasSecret={activeApp?.hasSecret}
            nameLabel="显示名称"
            namePlaceholder="Main Bot"
            secretPlaceholderWithExisting="留空表示保留现有 App Secret"
            onNameChange={(value) => onDraftChange((current) => ({ ...current, name: value }))}
            onAppIDChange={(value) => onDraftChange((current) => ({ ...current, appId: value }))}
            onAppSecretChange={(value) => onDraftChange((current) => ({ ...current, appSecret: value }))}
          />

          <div className="wizard-info-stack">
            <div className="manifest-block">
              <h4>先去飞书后台做什么</h4>
              <div className="wizard-link-list">
                <a href="https://open.feishu.cn/app?lang=zh-CN" target="_blank" rel="noreferrer">
                  打开飞书开发者后台
                </a>
              </div>
              <ul className="wizard-bullet-list">
                <li>进入后创建企业自建应用。</li>
                <li>必须给应用添加机器人能力，否则后续消息、菜单和事件都不会生效。</li>
                <li>推荐路径：左侧“应用能力”或“添加应用能力”里添加“机器人”。</li>
              </ul>
            </div>
            <div className="manifest-block">
              <h4>App ID / App Secret 在哪里</h4>
              <ul className="wizard-bullet-list">
                <li>进入应用后，打开左侧“凭证与基础信息”。</li>
                <li>在“应用凭证”区域复制 App ID。</li>
                <li>同一块区域可以复制 App Secret。</li>
              </ul>
            </div>
          </div>
        </div>
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
    case "vscode":
      return (
        <div className="wizard-step-layout">
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
                <p>先选你的使用场景。页面会根据这个选择，只给当前这台机器提供一条更安全的接入路径。</p>
              </div>
              <div className="choice-card-list" role="radiogroup" aria-label="VS Code 使用场景">
                <label className={`choice-card${vscodeScenario === "local_only" ? " selected" : ""}`}>
                  <input type="radio" name="vscode-usage-scenario" checked={vscodeScenario === "local_only"} onChange={() => onVSCodeScenarioChange("local_only")} />
                  <div>
                    <strong>只在这台机器本地使用</strong>
                    <p>适合桌面 VS Code。会写入这台机器的 settings.json。</p>
                  </div>
                </label>
                <label className={`choice-card${vscodeScenario === "remote_only" ? " selected" : ""}`}>
                  <input type="radio" name="vscode-usage-scenario" checked={vscodeScenario === "remote_only"} onChange={() => onVSCodeScenarioChange("remote_only")} />
                  <div>
                    <strong>主要去别的 SSH 机器上使用</strong>
                    <p>当前机器先不做 VS Code 接入，避免 host 设置影响远端。</p>
                  </div>
                </label>
                <label className={`choice-card${vscodeScenario === "local_and_remote" ? " selected" : ""}`}>
                  <input type="radio" name="vscode-usage-scenario" checked={vscodeScenario === "local_and_remote"} onChange={() => onVSCodeScenarioChange("local_and_remote")} />
                  <div>
                    <strong>这台机器本地要用，也会 SSH 到别的机器</strong>
                    <p>当前机器只处理扩展入口，不写 settings.json。</p>
                  </div>
                </label>
              </div>
              {vscodeScenario === "local_only" ? (
                <div className="manifest-block">
                  <h4>推荐：写入这台机器的 settings.json</h4>
                  <p>这是最适合本机桌面 VS Code 的接入方式。我们会把这台机器上的 Codex 执行入口指向 codex-remote。</p>
                  <p>如果你以后改成主要通过 Remote SSH 去别的机器上使用，需要回到管理页重新调整策略。</p>
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
              {vscodeScenario === "local_and_remote" ? (
                <>
                  <div className="manifest-block">
                    <h4>推荐：只处理这台机器的扩展入口</h4>
                    <p>这个场景下，不建议写这台机器的 settings.json。我们只接管当前机器的 VS Code 扩展入口，避免 host 设置影响以后连接到远程机器。</p>
                    <p>以后每台真正要通过 Remote SSH 使用的目标机器，仍然要各自安装并完成一次接入。</p>
                  </div>
                  {!vscodeBundleDetected ? (
                    <div className="notice-banner warn">还没检测到这台机器上的 VS Code 扩展安装。请先在这台机器上打开一次 VS Code，并确保 Codex 扩展已经安装，然后再回来继续。</div>
                  ) : null}
                </>
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
  canContinueVSCode,
  vscodePrimaryLabel,
  onStart,
  onTestAndContinue,
  onConfirmPermissions,
  onConfirmEvents,
  onConfirmLongConnection,
  onConfirmMenus,
  onCheckPublish,
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
      return (
        <button className="primary-button" type="button" onClick={onTestAndContinue} disabled={busyAction !== ""}>
          测试并继续
        </button>
      );
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

export function SetupStepSecondaryAction({ currentStep, busyAction, onCopyScopes, onDeferVSCode }: SetupStepSecondaryActionProps) {
  if (currentStep === "vscode") {
    return (
      <button className="secondary-button" type="button" onClick={onDeferVSCode} disabled={busyAction !== ""}>
        稍后在管理页处理
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
