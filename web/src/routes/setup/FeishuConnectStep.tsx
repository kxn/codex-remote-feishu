import type { FeishuAppSummary, FeishuOnboardingCompleteResponse, FeishuOnboardingSession } from "../../lib/types";
import { FeishuAppFields } from "../shared/FeishuAppFields";
import type { FeishuConnectMode, FeishuConnectStage, SetupDraft } from "./types";

type FeishuConnectStepProps = {
  surface?: "setup" | "admin";
  apps: FeishuAppSummary[];
  activeApp: FeishuAppSummary | null;
  draft: SetupDraft;
  connectStage: FeishuConnectStage;
  connectMode: FeishuConnectMode | null;
  onboardingSession: FeishuOnboardingSession | null;
  onboardingCompletion: FeishuOnboardingCompleteResponse | null;
  onboardingNeedsManualRetry: boolean;
  busyAction: string;
  onNameChange: (value: string) => void;
  onAppIDChange: (value: string) => void;
  onAppSecretChange: (value: string) => void;
  onConnectModeChange: (value: FeishuConnectMode) => void;
  onContinueModeSelection: () => void;
  onVerifyManual: () => void;
  onBackToModeSelection: () => void;
  onRefreshOnboarding: () => void;
  onRestartOnboarding: () => void;
  onSwitchToExistingFlow: () => void;
  onRetryOnboardingComplete: () => void;
  onContinueOnboardingNotice: () => void;
};

export function FeishuConnectStep({
  surface = "setup",
  apps,
  activeApp,
  draft,
  connectStage,
  connectMode,
  onboardingSession,
  onboardingCompletion,
  onboardingNeedsManualRetry,
  busyAction,
  onNameChange,
  onAppIDChange,
  onAppSecretChange,
  onConnectModeChange,
  onContinueModeSelection,
  onVerifyManual,
  onBackToModeSelection,
  onRefreshOnboarding,
  onRestartOnboarding,
  onSwitchToExistingFlow,
  onRetryOnboardingComplete,
  onContinueOnboardingNotice,
}: FeishuConnectStepProps) {
  const isSetupSurface = surface === "setup";
  const verifyActionLabel = isSetupSurface ? "验证并继续" : "保存并验证";
  const onboardingGuide = onboardingCompletion?.guide;
  const extraSetupNotice = isSetupSurface && apps.length > 1
    ? [{ tone: "warn" as const, message: "这次安装只处理一个飞书应用。更多应用的新增、切换和管理，请到本地管理页操作。" }]
    : [];

  function applyCredentialField(field: "appId" | "appSecret", value: string) {
    const pair = splitAppPair(value);
    if (pair) {
      onAppIDChange(pair.appId);
      onAppSecretChange(pair.appSecret);
      return;
    }
    if (field === "appId") {
      onAppIDChange(value);
      return;
    }
    onAppSecretChange(value);
  }

  if (activeApp?.readOnly) {
    return (
      <div className="wizard-step-layout two-column">
        <FeishuAppFields
          className="wizard-form-stack"
          notices={[
            ...extraSetupNotice,
            { tone: "warn" as const, message: isSetupSurface ? "这个飞书应用的信息由当前运行配置提供。这里可以测试连接，但不能改它的内容。" : "这个飞书应用的信息由当前运行配置提供。管理页里可以测试连接，但不能改它的内容。" },
          ]}
          values={draft}
          readOnly
          hasSecret={activeApp?.hasSecret}
          nameLabel="显示名称"
          namePlaceholder="Main Bot"
          onNameChange={onNameChange}
          onAppIDChange={(value) => applyCredentialField("appId", value)}
          onAppSecretChange={(value) => applyCredentialField("appSecret", value)}
        />

        <div className="wizard-info-stack">
          <div className="manifest-block">
            <h4>这个飞书应用当前不能在这里修改</h4>
            <p>{isSetupSurface ? "它已经由当前运行配置接入好了。这里可以先测试是否连通，然后继续后面的安装。" : "它已经由当前运行配置接入好了。管理页里可以测试是否连通，但不能在这里修改。"}</p>
          </div>
          <div className="wizard-inline-actions">
            <button className="primary-button" type="button" onClick={onVerifyManual} disabled={busyAction !== ""}>
              {isSetupSurface ? "测试并继续" : "测试连接"}
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (connectStage === "mode_select") {
    return (
      <div className="wizard-step-layout">
        {activeApp ? (
          <div className="wizard-status-card good">
            <strong>当前正在处理：{activeApp.name || activeApp.appId || "当前飞书应用"}</strong>
            <p>{activeApp.readOnly ? "这个飞书应用当前只能做连接测试。" : "如果继续使用已有飞书应用，这次会处理它。"}</p>
          </div>
        ) : null}
        <div className="manifest-block">
          <h4>这次要处理哪个飞书应用？</h4>
          <p>先决定是新建一个飞书应用，还是接入你已经在用的那个。后面的安装步骤不会受这里影响。</p>
        </div>
        <div className="choice-card-list" role="radiogroup" aria-label="飞书应用接入方式">
          <label className={`choice-card${connectMode === "new" ? " selected" : ""}`}>
            <input type="radio" name="feishu-connect-mode" checked={connectMode === "new"} onChange={() => onConnectModeChange("new")} />
            <div>
              <strong>新建飞书应用</strong>
              <p>推荐。页面会给你一个二维码，扫码后自动创建飞书应用，并把需要的信息带回来。</p>
            </div>
          </label>
          <label className={`choice-card${connectMode === "existing" ? " selected" : ""}`}>
            <input type="radio" name="feishu-connect-mode" checked={connectMode === "existing"} onChange={() => onConnectModeChange("existing")} />
            <div>
              <strong>接入已有飞书应用</strong>
              <p>如果你已经有飞书应用，直接填写对应信息继续。</p>
            </div>
          </label>
        </div>
        {extraSetupNotice.map((notice) => (
          <div key={notice.message} className={`notice-banner ${notice.tone}`}>{notice.message}</div>
        ))}
        <div className="wizard-inline-actions">
          <button className="primary-button" type="button" onClick={onContinueModeSelection} disabled={busyAction !== "" || connectMode === null}>
            下一步
          </button>
        </div>
      </div>
    );
  }

  if (connectStage === "existing_manual") {
    return (
      <div className="wizard-step-layout two-column">
        <FeishuAppFields
          className="wizard-form-stack"
          notices={[
            ...extraSetupNotice,
            { tone: "good" as const, message: "支持直接粘贴 `cli_xxx:secret_xxx`，页面会自动拆成 App ID / App Secret。" },
          ]}
          values={draft}
          readOnly={false}
          hasSecret={activeApp?.hasSecret}
          showNameField={false}
          nameLabel="显示名称"
          namePlaceholder="Main Bot"
          appIDHint="改成另一个 App ID 等于切换到另一个机器人身份，旧飞书会话不会自动迁移。显示名称会自动尝试从飞书拉取，拿不到时回退成 App ID。"
          secretPlaceholderWithExisting="留空表示保留现有 App Secret"
          onNameChange={onNameChange}
          onAppIDChange={(value) => applyCredentialField("appId", value)}
          onAppSecretChange={(value) => applyCredentialField("appSecret", value)}
        />

        <div className="wizard-info-stack">
          {activeApp ? (
            <div className="wizard-status-card good">
              <strong>当前目标：{activeApp.name || activeApp.appId || "当前飞书应用"}</strong>
              <p>{activeApp.readOnly ? "当前只能做连接验证。" : "如果你不改 App ID，这次会继续处理当前这个飞书应用。"}</p>
            </div>
          ) : null}
          <div className="manifest-block">
            <h4>已有飞书应用怎么接</h4>
            <ul className="wizard-bullet-list">
              <li>打开飞书开发者后台。</li>
              <li>进入你的飞书应用，打开“凭证与基础信息”。</li>
              <li>复制 App ID 和 App Secret，然后回来验证并继续。</li>
            </ul>
          </div>
          <div className="wizard-link-list">
            <a href="https://open.feishu.cn/app?lang=zh-CN" target="_blank" rel="noreferrer">
              打开飞书开发者后台
            </a>
          </div>
          <div className="wizard-inline-actions">
            <button className="ghost-button" type="button" onClick={onBackToModeSelection} disabled={busyAction !== ""}>
              返回选择
            </button>
            <button className="primary-button" type="button" onClick={onVerifyManual} disabled={busyAction !== ""}>
              {verifyActionLabel}
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (connectStage === "new_qr_notice") {
    const appName = onboardingCompletion?.app.name || onboardingCompletion?.session.displayName || onboardingSession?.displayName || "新飞书应用";
    const appID = onboardingCompletion?.app.appId || onboardingCompletion?.session.appId || onboardingSession?.appId || "-";
    const remainingActions = onboardingGuide?.remainingManualActions?.length
      ? onboardingGuide.remainingManualActions
      : [
          "如果需要把 Markdown 预览上传到飞书云盘，还需要额外申请 `drive:drive` 权限。",
          "这个权限通常需要管理员审批；不需要 Markdown 预览时可以先跳过。",
        ];

    return (
      <div className="wizard-step-layout two-column">
        <div className="wizard-form-stack">
          <div className="wizard-status-card good">
            <strong>{appName}</strong>
            <p>App ID: {appID}</p>
          </div>
          <div className="manifest-block">
            <h4>{onboardingGuide?.autoConfiguredSummary || "扫码创建已经完成"}</h4>
            <p>飞书应用已经接好了。下一步会继续确认现在能不能开始正常使用。</p>
          </div>
        </div>

        <div className="wizard-info-stack">
          <div className="manifest-block">
            <h4>还剩哪些可选动作</h4>
            <ul className="wizard-bullet-list">
              {remainingActions.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          </div>
          <div className="wizard-inline-actions">
            <button className="primary-button" type="button" onClick={onContinueOnboardingNotice} disabled={busyAction !== ""}>
              下一步
            </button>
          </div>
        </div>
      </div>
    );
  }

  const expiresAtText = onboardingSession?.expiresAt ? new Date(onboardingSession.expiresAt).toLocaleString() : "";
  const pollIntervalSeconds = Math.max(2, onboardingSession?.pollIntervalSeconds ?? 5);

  return (
    <div className="wizard-step-layout two-column">
      <div className="wizard-form-stack">
        <div className="wizard-qr-card">
          <h4>扫码创建飞书应用</h4>
          <p>{isSetupSurface ? "请使用飞书手机客户端扫码完成创建。处理完成后，页面会自动继续下一步。" : "请使用飞书手机客户端扫码完成创建。处理完成后，页面会自动保存并继续下一步。"}</p>
          {onboardingSession?.qrCodeDataUrl ? (
            <img className="wizard-qr-image" src={onboardingSession.qrCodeDataUrl} alt="飞书应用创建二维码" />
          ) : (
            <div className="notice-banner warn">二维码还没准备好，请重新开始扫码。</div>
          )}
          {expiresAtText ? <p className="form-hint">当前二维码过期时间：{expiresAtText}</p> : null}
          {onboardingSession?.verificationUrl ? (
            <div className="wizard-link-list">
              <a href={onboardingSession.verificationUrl} target="_blank" rel="noreferrer">
                无法扫码时，在浏览器打开
              </a>
            </div>
          ) : null}
        </div>
      </div>

      <div className="wizard-info-stack">
        {onboardingSession?.status === "pending" || !onboardingSession ? (
          <>
            <div className="manifest-block">
              <h4>正在等待扫码</h4>
              <p>扫完以后，这里会自动显示新应用信息，并继续处理。</p>
              <p>页面会每 {pollIntervalSeconds} 秒自动检查一次；如果你怀疑卡住了，也可以手动刷新。</p>
            </div>
            <div className="wizard-inline-actions">
              <button className="secondary-button" type="button" onClick={onRefreshOnboarding} disabled={busyAction !== ""}>
                刷新二维码状态
              </button>
              <button className="ghost-button" type="button" onClick={onSwitchToExistingFlow} disabled={busyAction !== ""}>
                改用手动填写
              </button>
              <button className="ghost-button" type="button" onClick={onBackToModeSelection} disabled={busyAction !== ""}>
                返回选择
              </button>
            </div>
          </>
        ) : null}

        {onboardingSession?.status === "ready" ? (
          <>
            <div className="wizard-status-card good">
              <strong>{onboardingSession.displayName || onboardingSession.appId || "新飞书应用"}</strong>
              <p>App ID: {onboardingSession.appId || "-"}</p>
            </div>
            <div className="manifest-block">
              <h4>{onboardingNeedsManualRetry ? "连接测试还没有通过" : "凭据已经拿到，正在完成连接测试"}</h4>
              <p>{onboardingNeedsManualRetry ? "飞书应用已经创建好了。你可以直接重试，或者改成手动填写。" : "如果长时间没有自动继续，可以手动重试。"}</p>
            </div>
            <div className="wizard-inline-actions">
              <button className="primary-button" type="button" onClick={onRetryOnboardingComplete} disabled={busyAction !== ""}>
                {onboardingNeedsManualRetry ? "重新测试连接" : "继续连接测试"}
              </button>
              <button className="ghost-button" type="button" onClick={onSwitchToExistingFlow} disabled={busyAction !== ""}>
                改用手动填写
              </button>
              <button className="ghost-button" type="button" onClick={onBackToModeSelection} disabled={busyAction !== ""}>
                返回选择
              </button>
            </div>
          </>
        ) : null}

        {onboardingSession?.status === "expired" || onboardingSession?.status === "failed" ? (
          <>
            <div className="notice-banner warn">{onboardingSession.errorMessage || "这次扫码没有完成，请重新开始。"}</div>
            <div className="wizard-inline-actions">
              <button className="primary-button" type="button" onClick={onRestartOnboarding} disabled={busyAction !== ""}>
                重新开始扫码
              </button>
              <button className="secondary-button" type="button" onClick={onSwitchToExistingFlow} disabled={busyAction !== ""}>
                改用手动填写
              </button>
              <button className="ghost-button" type="button" onClick={onBackToModeSelection} disabled={busyAction !== ""}>
                返回选择
              </button>
            </div>
          </>
        ) : null}
      </div>
    </div>
  );
}

function splitAppPair(value: string): { appId: string; appSecret: string } | null {
  const trimmed = value.trim();
  const separator = trimmed.indexOf(":");
  if (separator <= 0 || separator >= trimmed.length - 1) {
    return null;
  }
  const appId = trimmed.slice(0, separator).trim();
  const appSecret = trimmed.slice(separator + 1).trim();
  if (appId === "" || appSecret === "") {
    return null;
  }
  return { appId, appSecret };
}
