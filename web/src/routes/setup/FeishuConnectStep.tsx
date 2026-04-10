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
    ? [{ tone: "warn" as const, message: "当前 setup 只继续处理一个应用。更多应用的新增、切换和运行管理请到本地管理页进行。" }]
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
            { tone: "warn" as const, message: isSetupSurface ? "当前应用由运行时环境变量接管，setup 页面会直接对它做连接测试，但不会修改本地配置。" : "当前应用由运行时环境变量接管，管理页只能做连接测试，不能修改本地配置。" },
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
            <h4>当前是只读接入</h4>
            <p>{isSetupSurface ? "这个飞书应用来自当前运行时环境变量。setup 只能做连接测试，不能修改本地配置。" : "这个飞书应用来自当前运行时环境变量。管理页只能做连接测试，不能修改本地配置。"}</p>
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
        <div className="manifest-block">
          <h4>你想怎么接入飞书应用？</h4>
          <p>先选一种方式。后面的权限、事件、菜单和发布流程保持不变。</p>
        </div>
        <div className="choice-card-list" role="radiogroup" aria-label="飞书应用接入方式">
          <label className={`choice-card${connectMode === "new" ? " selected" : ""}`}>
            <input type="radio" name="feishu-connect-mode" checked={connectMode === "new"} onChange={() => onConnectModeChange("new")} />
            <div>
              <strong>新建飞书应用</strong>
              <p>推荐。页面会给你一个二维码，扫码后自动创建应用并带回 App ID / App Secret。</p>
            </div>
          </label>
          <label className={`choice-card${connectMode === "existing" ? " selected" : ""}`}>
            <input type="radio" name="feishu-connect-mode" checked={connectMode === "existing"} onChange={() => onConnectModeChange("existing")} />
            <div>
              <strong>接入已有应用</strong>
              <p>如果你已经在飞书开放平台有应用，直接填写 App ID 和 App Secret。</p>
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
          <div className="manifest-block">
            <h4>已有应用怎么接</h4>
            <ul className="wizard-bullet-list">
              <li>打开飞书开发者后台。</li>
              <li>进入你的应用，打开“凭证与基础信息”。</li>
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
            <p>机器人基础接入已经处理完成。下面给你一个明确的收口说明，避免流程看起来像是直接跳回去了。</p>
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
          <p>{isSetupSurface ? "请使用飞书手机客户端扫码完成应用创建与授权。拿到凭据后，页面会自动继续连接测试。" : "请使用飞书手机客户端扫码完成应用创建与授权。拿到凭据后，页面会自动保存并继续连接测试。"}</p>
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
              <p>扫完以后，这里会自动显示新应用的名称和 App ID，并继续做连接测试。</p>
              <p>页面会每 {pollIntervalSeconds} 秒自动检查一次扫码结果；如果你怀疑卡住了，也可以手动刷新。</p>
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
              <p>{onboardingNeedsManualRetry ? "飞书应用已经创建好了。你可以直接重试连接测试，或者改成手动填写。" : "如果长时间没有自动继续，可以手动重试连接测试。"}</p>
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
