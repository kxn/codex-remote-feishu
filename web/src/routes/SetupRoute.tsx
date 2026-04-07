import { useEffect, useMemo, useState } from "react";
import { formatError, requestJSON, requestJSONAllowHTTPError, sendJSON } from "../lib/api";
import type {
  BootstrapState,
  FeishuAppPublishCheckResponse,
  FeishuAppResponse,
  FeishuAppSummary,
  FeishuAppVerifyResponse,
  FeishuAppsResponse,
  FeishuManifestResponse,
  SetupCompleteResponse,
  VSCodeDetectResponse,
} from "../lib/types";
import { BlockingModal, ErrorState, LoadingState, Panel, StatusBadge } from "../components/ui";

const newAppID = "__new__";

type SetupDraft = {
  isNew: boolean;
  name: string;
  appId: string;
  appSecret: string;
};

type Notice = {
  tone: "good" | "warn";
  message: string;
};

type BlockingErrorState = {
  title: string;
  message: string;
  detail?: string;
} | null;

type StepID =
  | "start"
  | "connect"
  | "permissions"
  | "events"
  | "longConnection"
  | "menus"
  | "publish"
  | "vscode"
  | "finish";

type WizardStep = {
  id: StepID;
  label: string;
  summary: string;
  optional?: boolean;
};

const wizardSteps: WizardStep[] = [
  { id: "start", label: "开始", summary: "说明安装向导会做什么。" },
  { id: "connect", label: "创建并连接飞书应用", summary: "创建应用、添加机器人能力，并完成连接测试。" },
  { id: "permissions", label: "配置应用权限", summary: "复制 scopes JSON，并在“批量导入/导出权限”里保存申请。" },
  { id: "events", label: "配置事件订阅", summary: "按 manifest 订阅需要的飞书事件，并在“订阅方式”里保存长连接。" },
  { id: "longConnection", label: "配置回调订阅方式", summary: "把“回调订阅方式”设为长连接，不填写 HTTP 回调 URL。" },
  { id: "menus", label: "配置机器人菜单", summary: "按 key 创建真正会生效的机器人菜单。" },
  { id: "publish", label: "发布应用", summary: "发版后执行一次服务端验收检查。" },
  { id: "vscode", label: "VS Code（可选）", summary: "SSH 推荐 managed_shim，其他情况推荐 all。", optional: true },
  { id: "finish", label: "完成", summary: "提示首次对话路径，并进入本地管理页。" },
];

function emptyDraft(): SetupDraft {
  return {
    isNew: true,
    name: "",
    appId: "",
    appSecret: "",
  };
}

export function SetupRoute() {
  const [bootstrap, setBootstrap] = useState<BootstrapState | null>(null);
  const [apps, setApps] = useState<FeishuAppSummary[]>([]);
  const [manifest, setManifest] = useState<FeishuManifestResponse["manifest"] | null>(null);
  const [vscode, setVSCode] = useState<VSCodeDetectResponse | null>(null);
  const [vscodeError, setVSCodeError] = useState<string>("");
  const [selectedID, setSelectedID] = useState<string>(() => preferredSetupAppFromLocation());
  const [draft, setDraft] = useState<SetupDraft>(emptyDraft());
  const [setupStarted, setSetupStarted] = useState(false);
  const [permissionsConfirmed, setPermissionsConfirmed] = useState(false);
  const [eventsConfirmed, setEventsConfirmed] = useState(false);
  const [longConnectionConfirmed, setLongConnectionConfirmed] = useState(false);
  const [menusConfirmed, setMenusConfirmed] = useState(false);
  const [vscodeDeferred, setVSCodeDeferred] = useState(false);
  const [currentStepHint, setCurrentStepHint] = useState<StepID>("start");
  const [error, setError] = useState<string>("");
  const [notice, setNotice] = useState<Notice | null>(null);
  const [busyAction, setBusyAction] = useState<string>("");
  const [finishInfo, setFinishInfo] = useState<SetupCompleteResponse | null>(null);
  const [blockingError, setBlockingError] = useState<BlockingErrorState>(null);

  async function loadData(preferredID?: string) {
    const [bootstrapState, appList, manifestResponse, vscodeState] = await Promise.all([
      requestJSON<BootstrapState>("/api/setup/bootstrap-state"),
      requestJSON<FeishuAppsResponse>("/api/setup/feishu/apps"),
      requestJSON<FeishuManifestResponse>("/api/setup/feishu/manifest"),
      loadVSCodeState("/api/setup/vscode/detect"),
    ]);
    const nextSelectedID = chooseAppID(appList.apps, preferredID ?? selectedID);
    const nextActiveApp = appList.apps.find((app) => app.id === nextSelectedID) ?? null;

    setBootstrap(bootstrapState);
    setApps(appList.apps);
    setManifest(manifestResponse.manifest);
    setVSCode(vscodeState.data);
    setVSCodeError(vscodeState.error);
    setSelectedID(nextSelectedID);
    setDraft(appToDraft(nextActiveApp));
    setCurrentStepHint((current) => {
      const fallback = defaultStepFor(bootstrapState, appList.apps, nextActiveApp, vscodeState.data, vscodeDeferred, setupStarted);
      if (current === "start" && fallback !== "start") {
        return fallback;
      }
      return isStepReachable(current, bootstrapState, nextActiveApp) ? current : fallback;
    });
  }

  useEffect(() => {
    let cancelled = false;
    void loadData()
      .then(() => {
        if (!cancelled) {
          setError("");
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(formatError(err));
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const activeApp = useMemo(() => apps.find((app) => app.id === selectedID) ?? null, [apps, selectedID]);
  const scopesJSON = useMemo(() => JSON.stringify(manifest?.scopesImport ?? { scopes: { tenant: [], user: [] } }, null, 2), [manifest]);

  useEffect(() => {
    if (apps.length > 0) {
      setSetupStarted(true);
    }
  }, [apps.length]);

  useEffect(() => {
    setDraft(appToDraft(activeApp));
  }, [activeApp?.id, activeApp?.name, activeApp?.appId, activeApp?.hasSecret]);

  useEffect(() => {
    setPermissionsConfirmed(Boolean(activeApp?.wizard?.scopesExportedAt));
    setEventsConfirmed(Boolean(activeApp?.wizard?.eventsConfirmedAt));
    setLongConnectionConfirmed(Boolean(activeApp?.wizard?.callbacksConfirmedAt));
    setMenusConfirmed(Boolean(activeApp?.wizard?.menusConfirmedAt));
  }, [
    activeApp?.id,
    activeApp?.wizard?.scopesExportedAt,
    activeApp?.wizard?.eventsConfirmedAt,
    activeApp?.wizard?.callbacksConfirmedAt,
    activeApp?.wizard?.menusConfirmedAt,
  ]);

  const resolvedCurrentStep = useMemo(
    () => (isStepReachable(currentStepHint, bootstrap, activeApp) ? currentStepHint : defaultStepFor(bootstrap, apps, activeApp, vscode, vscodeDeferred, setupStarted)),
    [activeApp, apps, bootstrap, currentStepHint, setupStarted, vscode, vscodeDeferred],
  );
  const currentStepIndex = wizardSteps.findIndex((step) => step.id === resolvedCurrentStep);
  const currentStepMeta = wizardSteps[currentStepIndex >= 0 ? currentStepIndex : 0];
  const stepCompletion = {
    start: setupStarted || apps.length > 0,
    connect: Boolean(activeApp?.wizard?.connectionVerifiedAt),
    permissions: Boolean(activeApp?.wizard?.scopesExportedAt),
    events: Boolean(activeApp?.wizard?.eventsConfirmedAt),
    longConnection: Boolean(activeApp?.wizard?.callbacksConfirmedAt),
    menus: Boolean(activeApp?.wizard?.menusConfirmedAt),
    publish: Boolean(activeApp?.wizard?.publishedAt),
    vscode: vscodeDeferred || vscodeIsReady(vscode),
  };

  useEffect(() => {
    window.scrollTo({ top: 0, behavior: "auto" });
    document.documentElement.scrollTop = 0;
    document.body.scrollTop = 0;
  }, [resolvedCurrentStep]);

  async function runAction(label: string, work: () => Promise<void>) {
    setBusyAction(label);
    setNotice(null);
    try {
      await work();
    } catch (err: unknown) {
      showBlockingError("这一步还没有完成", formatError(err));
    } finally {
      setBusyAction("");
    }
  }

  function showBlockingError(title: string, message: string, detail?: string) {
    setBlockingError({ title, message, detail });
  }

  async function copyText(value: string, successMessage: string) {
    await runAction("copy-text", async () => {
      if (!navigator.clipboard?.writeText) {
        throw new Error("当前浏览器不支持复制到剪贴板。");
      }
      await navigator.clipboard.writeText(value);
      setNotice({ tone: "good", message: successMessage });
    });
  }

  async function testAndContinue() {
    const hasPersistedSecret = Boolean(activeApp?.hasSecret);
    if (activeApp?.readOnly) {
      await verifyExistingAppAndAdvance(activeApp.id);
      return;
    }
    if (draft.appId.trim() === "") {
      showBlockingError("这一步还没有完成", "请先填写 App ID。");
      return;
    }
    if (draft.appSecret.trim() === "" && !hasPersistedSecret) {
      showBlockingError("这一步还没有完成", "请先填写 App Secret。");
      return;
    }

    await runAction("connect-app", async () => {
      const payload = {
        name: blankToUndefined(draft.name),
        appId: blankToUndefined(draft.appId),
        appSecret: blankToUndefined(draft.appSecret),
        enabled: true,
      };
      const response = draft.isNew
        ? await sendJSON<FeishuAppResponse>("/api/setup/feishu/apps", "POST", payload)
        : await sendJSON<FeishuAppResponse>(`/api/setup/feishu/apps/${encodeURIComponent(selectedID)}`, "PUT", payload);
      await verifyExistingAppAndAdvance(response.app.id);
    });
  }

  async function verifyExistingAppAndAdvance(appID: string) {
    await runAction("verify-app", async () => {
      const response = await requestJSONAllowHTTPError<FeishuAppVerifyResponse>(`/api/setup/feishu/apps/${encodeURIComponent(appID)}/verify`, {
        method: "POST",
      });
      await loadData(appID);
      if (!response.ok) {
        const detail = `${response.data.result.errorCode || "verify_failed"} ${response.data.result.errorMessage || ""}`.trim();
        showBlockingError("这一步还没有完成", "飞书应用连接测试失败，请检查 App ID、App Secret，以及飞书平台里是否已经添加机器人能力。", detail);
        return;
      }
      setNotice({ tone: "good", message: "飞书应用连接成功，已进入下一步。" });
      setSetupStarted(true);
      setCurrentStepHint("permissions");
    });
  }

  async function confirmPermissionsAndContinue() {
    if (!permissionsConfirmed) {
      showBlockingError("这一步还没有完成", "请先在飞书平台完成权限导入，并勾选页面上的确认项。");
      return;
    }
    if (!activeApp) {
      showBlockingError("这一步还没有完成", "当前还没有可用的飞书应用。");
      return;
    }
    await runAction("wizard-permissions", async () => {
      await sendJSON<FeishuAppResponse>(`/api/setup/feishu/apps/${encodeURIComponent(activeApp.id)}/wizard`, "PATCH", { scopesExported: true });
      await loadData(activeApp.id);
      setNotice({ tone: "good", message: "权限导入已记录，继续下一步。" });
      setCurrentStepHint("events");
    });
  }

  async function confirmEventsAndContinue() {
    if (!eventsConfirmed) {
      showBlockingError("这一步还没有完成", "请先在飞书平台完成事件订阅，并勾选页面上的确认项。");
      return;
    }
    if (!activeApp) {
      showBlockingError("这一步还没有完成", "当前还没有可用的飞书应用。");
      return;
    }
    await runAction("wizard-events", async () => {
      await sendJSON<FeishuAppResponse>(`/api/setup/feishu/apps/${encodeURIComponent(activeApp.id)}/wizard`, "PATCH", { eventsConfirmed: true });
      await loadData(activeApp.id);
      setNotice({ tone: "good", message: "事件订阅已记录，继续下一步。" });
      setCurrentStepHint("longConnection");
    });
  }

  async function confirmLongConnectionAndContinue() {
    if (!longConnectionConfirmed) {
      showBlockingError("这一步还没有完成", "请先在飞书平台把回调订阅方式保存为长连接，并勾选页面上的确认项。");
      return;
    }
    if (!activeApp) {
      showBlockingError("这一步还没有完成", "当前还没有可用的飞书应用。");
      return;
    }
    await runAction("wizard-long-connection", async () => {
      await sendJSON<FeishuAppResponse>(`/api/setup/feishu/apps/${encodeURIComponent(activeApp.id)}/wizard`, "PATCH", { callbacksConfirmed: true });
      await loadData(activeApp.id);
      setNotice({ tone: "good", message: "回调长连接配置已记录，继续下一步。" });
      setCurrentStepHint("menus");
    });
  }

  async function confirmMenusAndContinue() {
    if (!menusConfirmed) {
      showBlockingError("这一步还没有完成", "请先在飞书平台完成机器人菜单配置，并勾选页面上的确认项。");
      return;
    }
    if (!activeApp) {
      showBlockingError("这一步还没有完成", "当前还没有可用的飞书应用。");
      return;
    }
    await runAction("wizard-menus", async () => {
      await sendJSON<FeishuAppResponse>(`/api/setup/feishu/apps/${encodeURIComponent(activeApp.id)}/wizard`, "PATCH", { menusConfirmed: true });
      await loadData(activeApp.id);
      setNotice({ tone: "good", message: "机器人菜单配置已记录，继续下一步。" });
      setCurrentStepHint("publish");
    });
  }

  async function checkPublishAndContinue() {
    if (!activeApp) {
      showBlockingError("这一步还没有完成", "当前还没有可用的飞书应用。");
      return;
    }
    await runAction("publish-check", async () => {
      const response = await requestJSONAllowHTTPError<FeishuAppPublishCheckResponse>(`/api/setup/feishu/apps/${encodeURIComponent(activeApp.id)}/publish-check`, {
        method: "POST",
      });
      await loadData(activeApp.id);
      if (!response.ok || !response.data.ready) {
        showBlockingError(
          "这一步还没有完成",
          "发布验收没有通过。请先回到飞书后台完成缺失项，再重新点击“检查并继续”。",
          (response.data.issues ?? []).join("\n"),
        );
        return;
      }
      setNotice({ tone: "good", message: "发布验收通过，继续下一步。" });
      setCurrentStepHint("vscode");
    });
  }

  async function applyRecommendedVSCode() {
    await runAction("vscode-apply", async () => {
      const response = await sendJSON<VSCodeDetectResponse>("/api/setup/vscode/apply", "POST", {
        mode: vscode?.recommendedMode || "all",
      });
      setVSCode(response);
      setVSCodeError("");
      setVSCodeDeferred(false);
      setNotice({ tone: "good", message: `VS Code 推荐模式已应用：${response.recommendedMode}。` });
      setCurrentStepHint("finish");
    });
  }

  async function finishSetup() {
    if (!bootstrap) {
      return;
    }
    await runAction("finish-setup", async () => {
      const response = await sendJSON<SetupCompleteResponse>("/api/setup/complete", "POST");
      if (bootstrap.session.trustedLoopback) {
        window.location.assign("/");
        return;
      }
      setFinishInfo(response);
      setNotice({ tone: "good", message: response.message });
    });
  }

  function goToPreviousStep() {
    const previous = previousStepFor(resolvedCurrentStep);
    if (previous) {
      setCurrentStepHint(previous);
    }
  }

  function renderStepBody() {
    if (!bootstrap || !manifest) {
      return null;
    }
    switch (resolvedCurrentStep) {
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
            <div className="wizard-form-stack">
              {apps.length > 1 ? <div className="notice-banner warn">当前 setup 只继续处理一个应用。更多应用的新增、切换和运行管理请到本地管理页进行。</div> : null}
              {activeApp?.readOnly ? <div className="notice-banner warn">当前应用由运行时环境变量接管，setup 页面会直接对它做连接测试，但不会修改本地配置。</div> : null}
              <label className="field">
                <span>显示名称</span>
                <input value={draft.name} placeholder="Main Bot" disabled={Boolean(activeApp?.readOnly)} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} />
              </label>
              <label className="field">
                <span>App ID</span>
                <input value={draft.appId} placeholder="cli_xxx" disabled={Boolean(activeApp?.readOnly)} onChange={(event) => setDraft((current) => ({ ...current, appId: event.target.value }))} />
              </label>
              <label className="field">
                <span>App Secret</span>
                <input
                  type="password"
                  value={draft.appSecret}
                  placeholder={activeApp?.hasSecret ? "留空表示保留现有 App Secret" : "secret_xxx"}
                  disabled={Boolean(activeApp?.readOnly)}
                  onChange={(event) => setDraft((current) => ({ ...current, appSecret: event.target.value }))}
                />
              </label>
            </div>

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
              <button className="secondary-button" type="button" onClick={() => void copyText(scopesJSON, "权限配置 JSON 已复制。")} disabled={busyAction !== ""}>
                复制权限配置
              </button>
            </div>
            <label className="checkbox-card">
              <input type="checkbox" checked={permissionsConfirmed} onChange={(event) => setPermissionsConfirmed(event.target.checked)} />
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
              <p>保存订阅方式后，再把下面这些事件全部订阅进去并保存。完成后，再去下一页配置回调订阅方式。</p>
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
              <input type="checkbox" checked={eventsConfirmed} onChange={(event) => setEventsConfirmed(event.target.checked)} />
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
                <li>同时确认上一页里已经订阅了 <code>card.action.trigger</code>。</li>
              </ul>
            </div>
            <div className="manifest-block">
              <h4>这一步为什么重要</h4>
              <ul className="wizard-bullet-list">
                <li>approval request 等卡片按钮要靠回调长连接进入服务。</li>
                <li>如果这里没配好，用户点卡片会没有反应。</li>
              </ul>
            </div>
            <label className="checkbox-card">
              <input type="checkbox" checked={longConnectionConfirmed} onChange={(event) => setLongConnectionConfirmed(event.target.checked)} />
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
              <input type="checkbox" checked={menusConfirmed} onChange={(event) => setMenusConfirmed(event.target.checked)} />
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
            <div className="manifest-block">
              <h4>推荐模式</h4>
              <ul className="wizard-bullet-list">
                <li>SSH / Remote：推荐 <code>managed_shim</code>。</li>
                <li>其他情况：推荐 <code>all</code>。</li>
              </ul>
              <p>当前页面只给出推荐结论。需要排查 bundle、shim、settings 等细节时，再展开技术信息。</p>
            </div>
            {vscodeError ? <div className="notice-banner warn">VS Code 检测暂时不可用：{vscodeError}</div> : null}
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
                <p>{stepCompletion.vscode ? "已配置或已明确稍后处理" : "暂未处理"}</p>
              </div>
            </div>
          </div>
        );
      default:
        return null;
    }
  }

  function renderPrimaryAction() {
    switch (resolvedCurrentStep) {
      case "start":
        return (
          <button className="primary-button" type="button" onClick={() => { setSetupStarted(true); setCurrentStepHint("connect"); }} disabled={busyAction !== ""}>
            开始
          </button>
        );
      case "connect":
        return (
          <button className="primary-button" type="button" onClick={() => void testAndContinue()} disabled={busyAction !== ""}>
            测试并继续
          </button>
        );
      case "permissions":
        return (
          <button className="primary-button" type="button" onClick={() => void confirmPermissionsAndContinue()} disabled={busyAction !== ""}>
            继续
          </button>
        );
      case "events":
        return (
          <button className="primary-button" type="button" onClick={() => void confirmEventsAndContinue()} disabled={busyAction !== ""}>
            继续
          </button>
        );
      case "longConnection":
        return (
          <button className="primary-button" type="button" onClick={() => void confirmLongConnectionAndContinue()} disabled={busyAction !== ""}>
            继续
          </button>
        );
      case "menus":
        return (
          <button className="primary-button" type="button" onClick={() => void confirmMenusAndContinue()} disabled={busyAction !== ""}>
            继续
          </button>
        );
      case "publish":
        return (
          <button className="primary-button" type="button" onClick={() => void checkPublishAndContinue()} disabled={busyAction !== ""}>
            检查并继续
          </button>
        );
      case "vscode":
        return (
          <button className="primary-button" type="button" onClick={() => void applyRecommendedVSCode()} disabled={busyAction !== "" || !vscode}>
            应用推荐配置
          </button>
        );
      case "finish":
        return (
          <button className="primary-button" type="button" onClick={() => void finishSetup()} disabled={busyAction !== ""}>
            完成并进入本地管理页
          </button>
        );
      default:
        return null;
    }
  }

  function renderSecondaryAction() {
    if (resolvedCurrentStep === "vscode") {
      return (
        <button
          className="secondary-button"
          type="button"
          onClick={() => {
            setVSCodeDeferred(true);
            setNotice({ tone: "warn", message: "VS Code 集成已留到本地管理页继续处理。" });
            setCurrentStepHint("finish");
          }}
          disabled={busyAction !== ""}
        >
          稍后在管理页处理
        </button>
      );
    }
    if (resolvedCurrentStep === "permissions") {
      return (
        <button className="secondary-button" type="button" onClick={() => void copyText(scopesJSON, "权限配置 JSON 已复制。")} disabled={busyAction !== ""}>
          复制权限配置
        </button>
      );
    }
    return null;
  }

  if (finishInfo && bootstrap && !bootstrap.session.trustedLoopback) {
    return (
      <div className="app-shell wizard-shell">
        <aside className="side-rail wizard-rail">
          <div className="brand-lockup">
            <div className="brand-mark">CR</div>
            <div>
              <p className="brand-kicker">Setup Completed</p>
              <h1>Codex Remote</h1>
            </div>
          </div>
          <p className="side-copy">当前 setup access 已关闭。远程 SSH 场景下，正式管理页仍然只允许 localhost 访问。</p>
        </aside>
        <main className="main-stage">
          <Panel title="安装向导已完成" description={finishInfo.message}>
            <div className="wizard-link-row">
              <span>本地管理页地址</span>
              <a href={finishInfo.adminURL} target="_blank" rel="noreferrer">
                {finishInfo.adminURL}
              </a>
            </div>
          </Panel>
        </main>
      </div>
    );
  }

  return (
    <>
      <div className="app-shell wizard-shell">
        <aside className="side-rail wizard-rail">
          <div className="brand-lockup">
            <div className="brand-mark">CR</div>
            <div>
              <p className="brand-kicker">Setup Wizard</p>
              <h1>Codex Remote</h1>
            </div>
          </div>
          <p className="side-copy">向导一次只展示当前步骤。左侧只保留步骤名和状态，不提前暴露后面的配置细节。</p>
          <div className="wizard-step-nav" aria-label="Setup Steps">
            {wizardSteps.map((step) => {
              const state = stepState(step.id, resolvedCurrentStep, stepCompletion, bootstrap, activeApp);
              const disabled = state === "locked";
              return (
                <button key={step.id} type="button" className={`wizard-step-link${step.id === resolvedCurrentStep ? " current" : ""}`} onClick={() => setCurrentStepHint(step.id)} disabled={disabled}>
                  <div>
                    <strong>{step.label}</strong>
                    <p>{step.summary}</p>
                  </div>
                  <StatusBadge value={stepStateLabel(state)} tone={stepStateTone(state)} />
                </button>
              );
            })}
          </div>
        </aside>

        <main className="main-stage wizard-stage">
          <header className="page-hero wizard-hero">
            <div>
              <p className="page-kicker">
                Setup Step {currentStepIndex + 1}/{wizardSteps.length}
              </p>
              <h2>{currentStepMeta.label}</h2>
              <p className="wizard-hero-copy">{currentStepMeta.summary}</p>
            </div>
            <div className="hero-actions">
              <button className="secondary-button" type="button" onClick={() => void loadData(activeApp?.id)} disabled={busyAction !== ""}>
                刷新状态
              </button>
            </div>
          </header>

          {notice ? <div className={`notice-banner ${notice.tone}`}>{notice.message}</div> : null}
          {!bootstrap && !error ? <LoadingState title="正在初始化 Setup 页面" description="读取 bootstrap、飞书应用、manifest 和 VS Code 检测结果。" /> : null}
          {error ? <ErrorState title="无法加载 Setup 状态" description="setup shell 已就位，但当前状态读取失败。" detail={error} /> : null}
          {bootstrap && manifest ? (
            <Panel title={currentStepMeta.label} description={currentStepMeta.summary} className="wizard-panel">
              {renderStepBody()}
              <div className="wizard-footer">
                <div className="wizard-footer-left">
                  {resolvedCurrentStep !== "start" ? (
                    <button className="ghost-button" type="button" onClick={goToPreviousStep} disabled={busyAction !== ""}>
                      上一步
                    </button>
                  ) : null}
                </div>
                <div className="wizard-footer-right">
                  {renderSecondaryAction()}
                  {renderPrimaryAction()}
                </div>
              </div>
            </Panel>
          ) : null}
        </main>
      </div>

      <BlockingModal open={Boolean(blockingError)} title={blockingError?.title || ""} message={blockingError?.message || ""} detail={blockingError?.detail} onConfirm={() => setBlockingError(null)} />
    </>
  );
}

function chooseAppID(apps: FeishuAppSummary[], preferredID: string): string {
  const preferred = apps.find((app) => app.id === preferredID);
  if (preferred) {
    return preferred.id;
  }
  if (apps.length > 0) {
    return apps[0].id;
  }
  return newAppID;
}

function preferredSetupAppFromLocation(): string {
  const value = new URLSearchParams(window.location.search).get("app");
  const normalized = value?.trim();
  return normalized ? normalized : newAppID;
}

function appToDraft(app: FeishuAppSummary | null): SetupDraft {
  if (!app) {
    return emptyDraft();
  }
  return {
    isNew: false,
    name: app.name || "",
    appId: app.appId || "",
    appSecret: "",
  };
}

function blankToUndefined(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed ? trimmed : undefined;
}

async function loadVSCodeState(path: string): Promise<{ data: VSCodeDetectResponse | null; error: string }> {
  try {
    return { data: await requestJSON<VSCodeDetectResponse>(path), error: "" };
  } catch (err: unknown) {
    return { data: null, error: formatError(err) };
  }
}

function stepState(
  stepID: StepID,
  currentStep: StepID,
  completion: Record<Exclude<StepID, "finish">, boolean>,
  bootstrap: BootstrapState | null,
  activeApp: FeishuAppSummary | null,
): "current" | "done" | "pending" | "locked" {
  if (stepID === currentStep) {
    return "current";
  }
  if (stepID !== "finish" && completion[stepID as Exclude<StepID, "finish">]) {
    return "done";
  }
  if (isStepReachable(stepID, bootstrap, activeApp)) {
    return "pending";
  }
  return "locked";
}

function stepStateLabel(state: "current" | "done" | "pending" | "locked"): string {
  switch (state) {
    case "current":
      return "当前";
    case "done":
      return "已完成";
    case "pending":
      return "未开始";
    default:
      return "已锁定";
  }
}

function stepStateTone(state: "current" | "done" | "pending" | "locked"): "neutral" | "good" | "warn" | "danger" {
  switch (state) {
    case "current":
      return "warn";
    case "done":
      return "good";
    case "locked":
      return "neutral";
    default:
      return "neutral";
  }
}

function defaultStepFor(
  bootstrap: BootstrapState | null,
  apps: FeishuAppSummary[],
  activeApp: FeishuAppSummary | null,
  vscode: VSCodeDetectResponse | null,
  vscodeDeferred: boolean,
  setupStarted: boolean,
): StepID {
  const started = setupStarted || apps.length > 0;
  if (!started) {
    return "start";
  }
  if (!activeApp || !activeApp.wizard?.connectionVerifiedAt) {
    return "connect";
  }
  if (!activeApp.wizard?.scopesExportedAt) {
    return "permissions";
  }
  if (!activeApp.wizard?.eventsConfirmedAt) {
    return "events";
  }
  if (!activeApp.wizard?.callbacksConfirmedAt) {
    return "longConnection";
  }
  if (!activeApp.wizard?.menusConfirmedAt) {
    return "menus";
  }
  if (!activeApp.wizard?.publishedAt) {
    return "publish";
  }
  if (!(vscodeDeferred || vscodeIsReady(vscode))) {
    return "vscode";
  }
  if (bootstrap?.setupRequired) {
    return "connect";
  }
  return "finish";
}

function isStepReachable(stepID: StepID, bootstrap: BootstrapState | null, activeApp: FeishuAppSummary | null): boolean {
  switch (stepID) {
    case "start":
      return true;
    case "connect":
      return true;
    case "permissions":
      return Boolean(activeApp?.wizard?.connectionVerifiedAt);
    case "events":
      return Boolean(activeApp?.wizard?.scopesExportedAt);
    case "longConnection":
      return Boolean(activeApp?.wizard?.eventsConfirmedAt);
    case "menus":
      return Boolean(activeApp?.wizard?.callbacksConfirmedAt);
    case "publish":
      return Boolean(activeApp?.wizard?.menusConfirmedAt);
    case "vscode":
      return Boolean(activeApp?.wizard?.publishedAt);
    case "finish":
      return Boolean(activeApp?.wizard?.publishedAt) && !bootstrap?.setupRequired;
    default:
      return false;
  }
}

function previousStepFor(stepID: StepID): StepID | null {
  switch (stepID) {
    case "connect":
      return "start";
    case "permissions":
      return "connect";
    case "events":
      return "permissions";
    case "longConnection":
      return "events";
    case "menus":
      return "longConnection";
    case "publish":
      return "menus";
    case "vscode":
      return "publish";
    case "finish":
      return "vscode";
    default:
      return null;
  }
}

function vscodeIsReady(vscode: VSCodeDetectResponse | null): boolean {
  if (!vscode) {
    return false;
  }
  if (vscode.recommendedMode === "managed_shim") {
    return vscode.latestShim.matchesBinary;
  }
  if (vscode.recommendedMode === "all") {
    return vscode.settings.matchesBinary && vscode.latestShim.matchesBinary;
  }
  return vscode.settings.matchesBinary;
}

function feishuAppConsoleURL(appId?: string): string {
  const trimmed = (appId || "").trim();
  if (!trimmed) {
    return "https://open.feishu.cn/app?lang=zh-CN";
  }
  return `https://open.feishu.cn/app/${encodeURIComponent(trimmed)}?lang=zh-CN`;
}
