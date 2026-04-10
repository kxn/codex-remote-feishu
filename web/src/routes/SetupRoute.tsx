import { useEffect, useMemo, useState } from "react";
import { formatError, requestJSON, requestJSONAllowHTTPError, sendJSON } from "../lib/api";
import type {
  AutostartDetectResponse,
  BootstrapState,
  FeishuAppMutation,
  FeishuOnboardingCompleteResponse,
  FeishuOnboardingSession,
  FeishuOnboardingSessionResponse,
  FeishuAppPublishCheckResponse,
  FeishuAppResponse,
  FeishuAppSummary,
  FeishuAppVerifyResponse,
  FeishuAppsResponse,
  FeishuManifestResponse,
  RuntimeRequirementsDetectResponse,
  SetupCompleteResponse,
  VSCodeDetectResponse,
} from "../lib/types";
import { BlockingModal, ErrorState, LoadingState, Panel, ShellScaffold, StatusBadge } from "../components/ui";
import { relativeLocalPath } from "../lib/paths";
import {
  SetupStepContent,
  SetupStepPrimaryAction,
  SetupStepSecondaryAction,
} from "./setup/SetupStepContent";
import {
  appToDraft,
  autostartIsComplete,
  chooseAppID,
  defaultStepFor,
  emptyDraft,
  isStepReachable,
  preferredSetupAppFromLocation,
  previousStepFor,
  stepState,
  stepStateLabel,
  stepStateTone,
} from "./setup/helpers";
import type { BlockingErrorState, FeishuConnectMode, FeishuConnectStage, SetupDraft, SetupNotice, StepID } from "./setup/types";
import { newAppID, wizardSteps } from "./setup/types";
import {
  blankToUndefined,
  buildSetupFeishuVerifySuccessMessage,
  loadAutostartState,
  type VSCodeSetupOutcome,
  type VSCodeUsageScenario,
  loadVSCodeState,
  vscodeApplyModeForScenario,
  vscodeHasDetectedBundle,
  vscodeOutcomeSummary,
  vscodePrimaryActionLabel,
  vscodeRequiresBundle,
  vscodeIsReady,
} from "./shared/helpers";

export function SetupRoute() {
  const [bootstrap, setBootstrap] = useState<BootstrapState | null>(null);
  const [apps, setApps] = useState<FeishuAppSummary[]>([]);
  const [manifest, setManifest] = useState<FeishuManifestResponse["manifest"] | null>(null);
  const [runtimeRequirements, setRuntimeRequirements] = useState<RuntimeRequirementsDetectResponse | null>(null);
  const [runtimeRequirementsError, setRuntimeRequirementsError] = useState<string>("");
  const [autostart, setAutostart] = useState<AutostartDetectResponse | null>(null);
  const [autostartError, setAutostartError] = useState<string>("");
  const [vscode, setVSCode] = useState<VSCodeDetectResponse | null>(null);
  const [vscodeError, setVSCodeError] = useState<string>("");
  const [selectedID, setSelectedID] = useState<string>(() => preferredSetupAppFromLocation());
  const [draft, setDraft] = useState<SetupDraft>(emptyDraft());
  const [setupStarted, setSetupStarted] = useState(false);
  const [permissionsConfirmed, setPermissionsConfirmed] = useState(false);
  const [eventsConfirmed, setEventsConfirmed] = useState(false);
  const [longConnectionConfirmed, setLongConnectionConfirmed] = useState(false);
  const [menusConfirmed, setMenusConfirmed] = useState(false);
  const [autostartSkipped, setAutostartSkipped] = useState(false);
  const [vscodeScenario, setVSCodeScenario] = useState<VSCodeUsageScenario | null>(null);
  const [vscodeOutcome, setVSCodeOutcome] = useState<VSCodeSetupOutcome | null>(null);
  const [currentStepHint, setCurrentStepHint] = useState<StepID>("start");
  const [error, setError] = useState<string>("");
  const [notice, setNotice] = useState<SetupNotice | null>(null);
  const [busyAction, setBusyAction] = useState<string>("");
  const [finishInfo, setFinishInfo] = useState<SetupCompleteResponse | null>(null);
  const [blockingError, setBlockingError] = useState<BlockingErrorState>(null);
  const [feishuConnectStage, setFeishuConnectStage] = useState<FeishuConnectStage>("mode_select");
  const [feishuConnectMode, setFeishuConnectMode] = useState<FeishuConnectMode | null>(null);
  const [onboardingSession, setOnboardingSession] = useState<FeishuOnboardingSession | null>(null);
  const [onboardingCompletion, setOnboardingCompletion] = useState<FeishuOnboardingCompleteResponse | null>(null);
  const [onboardingNeedsManualRetry, setOnboardingNeedsManualRetry] = useState(false);

  async function loadData(preferredID?: string) {
    const [bootstrapState, appList, manifestResponse, runtimeRequirementsState, autostartState, vscodeState] = await Promise.all([
      requestJSON<BootstrapState>("/api/setup/bootstrap-state"),
      requestJSON<FeishuAppsResponse>("/api/setup/feishu/apps"),
      requestJSON<FeishuManifestResponse>("/api/setup/feishu/manifest"),
      loadRuntimeRequirementsState(),
      loadAutostartState("/api/setup/autostart/detect"),
      loadVSCodeState("/api/setup/vscode/detect"),
    ]);
    const nextSelectedID = chooseAppID(appList.apps, preferredID ?? selectedID);
    const nextActiveApp = appList.apps.find((app) => app.id === nextSelectedID) ?? null;

    setBootstrap(bootstrapState);
    setApps(appList.apps);
    setManifest(manifestResponse.manifest);
    setRuntimeRequirements(runtimeRequirementsState.data);
    setRuntimeRequirementsError(runtimeRequirementsState.error);
    setAutostart(autostartState.data);
    setAutostartError(autostartState.error);
    setVSCode(vscodeState.data);
    setVSCodeError(vscodeState.error);
    setSelectedID(nextSelectedID);
    setDraft(appToDraft(nextActiveApp));
    setCurrentStepHint((current) => {
      const fallback = defaultStepFor(
        bootstrapState,
        appList.apps,
        nextActiveApp,
        Boolean(runtimeRequirementsState.data?.ready),
        autostartIsComplete(autostartState.data, autostartSkipped),
        Boolean(vscodeOutcome) || vscodeIsReady(vscodeState.data),
        setupStarted,
      );
      if (current === "start" && fallback !== "start") {
        return fallback;
      }
      return isStepReachable(current, bootstrapState, nextActiveApp, Boolean(runtimeRequirementsState.data?.ready)) ? current : fallback;
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
    setFeishuConnectStage("mode_select");
    setOnboardingSession(null);
    setOnboardingNeedsManualRetry(false);
    setFeishuConnectMode(apps.length > 0 ? "existing" : "new");
  }, [selectedID, apps.length]);

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

  useEffect(() => {
    if (vscode?.sshSession) {
      setVSCodeScenario(null);
    }
  }, [vscode?.sshSession]);

  const vscodeComplete = Boolean(vscodeOutcome) || vscodeIsReady(vscode);
  const runtimeRequirementsReady = Boolean(runtimeRequirements?.ready);
  const autostartComplete = autostartIsComplete(autostart, autostartSkipped);
  const autostartSummary = useMemo(() => {
    if (autostartSkipped) {
      if (autostart && !autostart.supported) {
        return "当前平台暂不支持";
      }
      return "已跳过，可稍后在管理页启用";
    }
    if (!autostart) {
      return "暂未处理";
    }
    if (!autostart.supported) {
      return "当前平台暂不支持";
    }
    if (autostart.status === "enabled") {
      return "已为当前用户启用登录后自动启动";
    }
    return "当前未启用";
  }, [autostart, autostartSkipped]);
  const vscodeSummary = vscodeOutcomeSummary(vscode, vscodeOutcome);
  const vscodeBundleDetected = vscodeHasDetectedBundle(vscode);
  const vscodeNeedsBundle = vscodeRequiresBundle(vscode, vscodeScenario);
  const vscodePrimaryLabel = vscodePrimaryActionLabel(vscode, vscodeScenario);
  const vscodeCanContinue = Boolean(vscode) && (vscode?.sshSession ? vscodeBundleDetected : vscodeScenario !== null && (!vscodeNeedsBundle || vscodeBundleDetected));

  const resolvedCurrentStep = useMemo(
    () =>
      isStepReachable(currentStepHint, bootstrap, activeApp, runtimeRequirementsReady)
        ? currentStepHint
        : defaultStepFor(bootstrap, apps, activeApp, runtimeRequirementsReady, autostartComplete, vscodeComplete, setupStarted),
    [activeApp, apps, autostartComplete, bootstrap, currentStepHint, runtimeRequirementsReady, setupStarted, vscodeComplete],
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
    runtimeRequirements: runtimeRequirementsReady,
    autostart: autostartComplete,
    vscode: vscodeComplete,
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

  async function loadRuntimeRequirementsState() {
    try {
      const data = await requestJSON<RuntimeRequirementsDetectResponse>("/api/setup/runtime-requirements/detect");
      return { data, error: "" };
    } catch (err: unknown) {
      return {
        data: null,
        error: formatError(err),
      };
    }
  }

  function resetFeishuConnectFlow(nextMode?: FeishuConnectMode) {
    setFeishuConnectStage("mode_select");
    setFeishuConnectMode(nextMode ?? (apps.length > 0 ? "existing" : "new"));
    setOnboardingSession(null);
    setOnboardingCompletion(null);
    setOnboardingNeedsManualRetry(false);
  }

  async function continueFeishuConnectModeSelection() {
    if (!feishuConnectMode) {
      showBlockingError("这一步还没有完成", "请先选择你想怎么接入飞书应用。");
      return;
    }
    if (feishuConnectMode === "existing") {
      setFeishuConnectStage("existing_manual");
      setOnboardingSession(null);
      setOnboardingNeedsManualRetry(false);
      return;
    }
    await startFeishuOnboarding();
  }

  async function startFeishuOnboarding() {
    await runAction("start-feishu-onboarding", async () => {
      const response = await sendJSON<FeishuOnboardingSessionResponse>("/api/setup/feishu/onboarding/sessions", "POST");
      setFeishuConnectMode("new");
      setFeishuConnectStage("new_qr");
      setOnboardingSession(response.session);
      setOnboardingCompletion(null);
      setOnboardingNeedsManualRetry(false);
    });
  }

  async function refreshFeishuOnboarding(options?: { silent?: boolean }) {
    if (!onboardingSession?.id) {
      return;
    }
    try {
      const response = await requestJSON<FeishuOnboardingSessionResponse>(`/api/setup/feishu/onboarding/sessions/${encodeURIComponent(onboardingSession.id)}`);
      setOnboardingSession(response.session);
      if (response.session.status === "pending") {
        setOnboardingNeedsManualRetry(false);
      }
    } catch (err: unknown) {
      if (options?.silent) {
        return;
      }
      throw err;
    }
  }

  async function retryOnboardingComplete() {
    if (!onboardingSession?.id) {
      showBlockingError("这一步还没有完成", "当前还没有可用的扫码会话。请重新开始扫码。");
      return;
    }
    await runAction("complete-feishu-onboarding", async () => {
      const response = await requestJSONAllowHTTPError<FeishuOnboardingCompleteResponse>(
        `/api/setup/feishu/onboarding/sessions/${encodeURIComponent(onboardingSession.id)}/complete`,
        { method: "POST" },
      );
      setOnboardingSession(response.data.session);
      if (!response.ok) {
        const detail = `${response.data.result.errorCode || "verify_failed"} ${response.data.result.errorMessage || ""}`.trim();
        setOnboardingNeedsManualRetry(true);
        showBlockingError("这一步还没有完成", "扫码创建的飞书应用已经拿到凭据，但连接测试失败。请检查机器人能力是否可用，然后重试连接测试。", detail);
        return;
      }
      setOnboardingCompletion(response.data);
      setOnboardingNeedsManualRetry(false);
      setFeishuConnectStage("new_qr_notice");
    });
  }

  async function continueOnboardingNotice() {
    if (!onboardingCompletion) {
      showBlockingError("这一步还没有完成", "当前还没有可继续的扫码结果，请重新开始扫码。");
      return;
    }
    await runAction("continue-feishu-onboarding-notice", async () => {
      await loadData(onboardingCompletion.app.id);
      setNotice({
        tone: onboardingCompletion.app.status?.state !== "connected" ? "warn" : "good",
        message: buildSetupFeishuVerifySuccessMessage(onboardingCompletion.app, onboardingCompletion.mutation),
      });
      setSetupStarted(true);
      resetFeishuConnectFlow("existing");
      setCurrentStepHint("permissions");
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
      await verifyExistingAppAndAdvance(response.app.id, response.mutation);
    });
  }

  async function verifyExistingAppAndAdvance(appID: string, mutation?: FeishuAppMutation) {
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
      setNotice({
        tone: mutation?.kind === "identity_changed" || response.data.app.status?.state !== "connected" ? "warn" : "good",
        message: buildSetupFeishuVerifySuccessMessage(response.data.app, mutation),
      });
      setSetupStarted(true);
      resetFeishuConnectFlow("existing");
      setCurrentStepHint("permissions");
    });
  }

  useEffect(() => {
    if (resolvedCurrentStep !== "connect" || feishuConnectStage !== "new_qr" || !onboardingSession?.id) {
      return;
    }
    if (onboardingSession.status === "ready" && !onboardingNeedsManualRetry && busyAction !== "complete-feishu-onboarding") {
      void retryOnboardingComplete();
      return;
    }
    if (onboardingSession.status !== "pending") {
      return;
    }
    const delay = Math.max(2_000, (onboardingSession.pollIntervalSeconds ?? 5) * 1_000);
    const timer = window.setTimeout(() => {
      void refreshFeishuOnboarding({ silent: true });
    }, delay);
    return () => window.clearTimeout(timer);
  }, [
    busyAction,
    feishuConnectStage,
    onboardingNeedsManualRetry,
    onboardingSession?.id,
    onboardingSession?.pollIntervalSeconds,
    onboardingSession?.status,
    resolvedCurrentStep,
  ]);

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
      setCurrentStepHint("runtimeRequirements");
    });
  }

  async function checkRuntimeRequirementsAndContinue() {
    await runAction("runtime-requirements-detect", async () => {
      const response = await requestJSON<RuntimeRequirementsDetectResponse>("/api/setup/runtime-requirements/detect");
      setRuntimeRequirements(response);
      setRuntimeRequirementsError("");
      if (!response.ready) {
        const failedChecks = response.checks
          .filter((check) => check.status === "fail")
          .map((check) => `${check.title}: ${check.summary}`);
        showBlockingError(
          "这一步还没有完成",
          "当前机器还不满足正常使用要求。请先修复失败项，再重新检查。",
          failedChecks.join("\n"),
        );
        return;
      }
      setNotice({
        tone: response.checks.some((check) => check.status === "warn") ? "warn" : "good",
        message: response.summary,
      });
      setCurrentStepHint("autostart");
    });
  }

  async function continueAutostart() {
    if (!autostart) {
      showBlockingError("这一步还没有完成", "当前还没拿到自动启动检测结果。请先刷新状态后再继续。");
      return;
    }
    if (!autostart.supported) {
      setAutostartSkipped(true);
      setCurrentStepHint("vscode");
      return;
    }
    if (autostart.status === "enabled") {
      setCurrentStepHint("vscode");
      return;
    }
    await runAction("autostart-apply", async () => {
      const response = await sendJSON<AutostartDetectResponse>("/api/setup/autostart/apply", "POST");
      setAutostart(response);
      setAutostartError("");
      setAutostartSkipped(false);
      setNotice({ tone: "good", message: "已为当前用户启用登录后自动启动。" });
      setCurrentStepHint("vscode");
    });
  }

  function missingBundleMessage(remoteMachine: boolean): string {
    if (remoteMachine) {
      return "还没检测到这台机器上的 VS Code 扩展。请先在这台机器上打开一次 VS Code Remote 窗口，并确保 Codex 扩展已经安装，然后再回来继续。";
    }
    return "还没检测到这台机器上的 VS Code 扩展安装。请先在这台机器上打开一次 VS Code，并确保 Codex 扩展已经安装，然后再回来继续。";
  }

  async function applyVSCodeMode(message: string) {
    await runAction("vscode-apply", async () => {
      const response = await sendJSON<VSCodeDetectResponse>("/api/setup/vscode/apply", "POST", {
        mode: "managed_shim",
      });
      setVSCode(response);
      setVSCodeError("");
      setVSCodeOutcome("managed_shim");
      setNotice({ tone: "good", message });
      setCurrentStepHint("finish");
    });
  }

  async function continueVSCode() {
    if (!vscode) {
      showBlockingError("这一步还没有完成", "当前还没拿到 VS Code 检测结果。请先刷新状态后再继续。");
      return;
    }
    if (vscode.sshSession) {
      if (!vscodeBundleDetected) {
        showBlockingError("这一步还没有完成", missingBundleMessage(true));
        return;
      }
      await applyVSCodeMode("已接管这台远程机器上的 VS Code 扩展入口。以后如果扩展升级，回到管理页重新安装扩展入口即可。");
      return;
    }
    if (!vscodeScenario) {
      showBlockingError("这一步还没有完成", "请先选择你以后主要怎么使用 VS Code 里的 Codex。");
      return;
    }
    if (vscodeScenario === "remote_only") {
      setVSCodeOutcome("remote_only_skip");
      setNotice({ tone: "warn", message: "已跳过当前机器的 VS Code 接入。等你在目标 SSH 机器上安装 codex-remote 后，再在那里完成 VS Code 接入即可。" });
      setCurrentStepHint("finish");
      return;
    }

    const mode = vscodeApplyModeForScenario(vscode, vscodeScenario);
    if (!mode) {
      showBlockingError("这一步还没有完成", "当前选择还不能映射到可执行的 VS Code 接入方式。");
      return;
    }
    if (mode === "managed_shim" && !vscodeBundleDetected) {
      showBlockingError("这一步还没有完成", missingBundleMessage(false));
      return;
    }
    await applyVSCodeMode("已接管这台机器上的 VS Code 扩展入口。当前策略不会写本机 settings.json；如果扩展升级，回到管理页重新安装扩展入口即可。");
  }

  async function finishSetup() {
    if (!bootstrap) {
      return;
    }
    await runAction("finish-setup", async () => {
      const response = await sendJSON<SetupCompleteResponse>("/api/setup/complete", "POST");
      if (bootstrap.session.trustedLoopback) {
        window.location.assign(relativeLocalPath("/"));
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

  if (finishInfo && bootstrap && !bootstrap.session.trustedLoopback) {
    return (
      <ShellScaffold
        routeLabel="Setup Completed"
        subtitle="当前 setup access 已关闭。远程 SSH 场景下，正式管理页仍然只允许 localhost 访问。"
        railToggleLabel="状态说明"
        railClassName="wizard-rail"
        railContent={<div className="wizard-rail-note">setup 已完成，后续请回到本地管理页继续操作。</div>}
      >
          <Panel title="安装向导已完成" description={finishInfo.message}>
            <div className="wizard-link-row">
              <span>本地管理页地址</span>
              <a href={finishInfo.adminURL} target="_blank" rel="noreferrer">
                {finishInfo.adminURL}
              </a>
            </div>
          </Panel>
      </ShellScaffold>
    );
  }

  return (
    <>
      <ShellScaffold
        routeLabel="Setup Wizard"
        subtitle="向导一次只展示当前步骤。左侧只保留步骤名和状态，不提前暴露后面的配置细节。"
        railToggleLabel="步骤导航"
        railClassName="wizard-rail"
        mainClassName="wizard-stage"
        railContent={
          <div className="wizard-step-nav" aria-label="Setup Steps">
            {wizardSteps.map((step) => {
              const state = stepState(step.id, resolvedCurrentStep, stepCompletion, bootstrap, activeApp, runtimeRequirementsReady);
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
        }
      >
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
          {!bootstrap && !error ? <LoadingState title="正在初始化 Setup 页面" description="读取 bootstrap、飞书应用、运行环境检查、manifest、自动启动和 VS Code 检测结果。" /> : null}
          {error ? <ErrorState title="无法加载 Setup 状态" description="setup shell 已就位，但当前状态读取失败。" detail={error} /> : null}
          {bootstrap && manifest ? (
            <Panel title={currentStepMeta.label} description={currentStepMeta.summary} className="wizard-panel">
              <SetupStepContent
                currentStep={resolvedCurrentStep}
                apps={apps}
                activeApp={activeApp}
                manifest={manifest}
                draft={draft}
                connectStage={feishuConnectStage}
                connectMode={feishuConnectMode}
                onboardingSession={onboardingSession}
                onboardingCompletion={onboardingCompletion}
                onboardingNeedsManualRetry={onboardingNeedsManualRetry}
                scopesJSON={scopesJSON}
                permissionsConfirmed={permissionsConfirmed}
                eventsConfirmed={eventsConfirmed}
                longConnectionConfirmed={longConnectionConfirmed}
                menusConfirmed={menusConfirmed}
                runtimeRequirements={runtimeRequirements}
                runtimeRequirementsError={runtimeRequirementsError}
                autostart={autostart}
                autostartError={autostartError}
                autostartSummary={autostartSummary}
                vscodeScenario={vscodeScenario}
                vscodeSummary={vscodeSummary}
                vscode={vscode}
                vscodeError={vscodeError}
                onDraftChange={setDraft}
                onConnectModeChange={setFeishuConnectMode}
                onContinueModeSelection={() => void continueFeishuConnectModeSelection()}
                onVerifyManual={() => void testAndContinue()}
                onBackToConnectModeSelection={() => resetFeishuConnectFlow()}
                onRefreshOnboarding={() => void runAction("refresh-feishu-onboarding", async () => refreshFeishuOnboarding())}
                onRestartOnboarding={() => void startFeishuOnboarding()}
                onSwitchToExistingFlow={() => {
                  setFeishuConnectMode("existing");
                  setFeishuConnectStage("existing_manual");
                  setOnboardingSession(null);
                  setOnboardingCompletion(null);
                  setOnboardingNeedsManualRetry(false);
                }}
                onRetryOnboardingComplete={() => void retryOnboardingComplete()}
                onContinueOnboardingNotice={() => void continueOnboardingNotice()}
                onPermissionsConfirmedChange={setPermissionsConfirmed}
                onEventsConfirmedChange={setEventsConfirmed}
                onLongConnectionConfirmedChange={setLongConnectionConfirmed}
                onMenusConfirmedChange={setMenusConfirmed}
                onVSCodeScenarioChange={setVSCodeScenario}
                onCopyScopes={() => void copyText(scopesJSON, "权限配置 JSON 已复制。")}
                busyAction={busyAction}
              />
              <div className="wizard-footer">
                <div className="wizard-footer-left">
                  {resolvedCurrentStep !== "start" ? (
                    <button className="ghost-button" type="button" onClick={goToPreviousStep} disabled={busyAction !== ""}>
                      上一步
                    </button>
                  ) : null}
                </div>
                <div className="wizard-footer-right">
                  <SetupStepSecondaryAction
                    currentStep={resolvedCurrentStep}
                    busyAction={busyAction}
                    onCopyScopes={() => void copyText(scopesJSON, "权限配置 JSON 已复制。")}
                    onSkipAutostart={() => {
                      setAutostartSkipped(true);
                      setNotice({ tone: "warn", message: "自动启动已跳过，可稍后在管理页再启用。" });
                      setCurrentStepHint("vscode");
                    }}
                    onDeferVSCode={() => {
                      setVSCodeOutcome("deferred");
                      setNotice({ tone: "warn", message: "VS Code 已跳过；如果以后要用，可以随时在管理页继续处理。" });
                      setCurrentStepHint("finish");
                    }}
                  />
                  <SetupStepPrimaryAction
                    currentStep={resolvedCurrentStep}
                    busyAction={busyAction}
                    autostart={autostart}
                    canContinueVSCode={vscodeCanContinue}
                    vscodePrimaryLabel={vscodePrimaryLabel}
                    onStart={() => {
                      setSetupStarted(true);
                      setCurrentStepHint("connect");
                    }}
                    onTestAndContinue={() => void testAndContinue()}
                    onConfirmPermissions={() => void confirmPermissionsAndContinue()}
                    onConfirmEvents={() => void confirmEventsAndContinue()}
                    onConfirmLongConnection={() => void confirmLongConnectionAndContinue()}
                    onConfirmMenus={() => void confirmMenusAndContinue()}
                    onCheckPublish={() => void checkPublishAndContinue()}
                    onCheckRuntimeRequirements={() => void checkRuntimeRequirementsAndContinue()}
                    onContinueAutostart={() => void continueAutostart()}
                    onContinueVSCode={() => void continueVSCode()}
                    onFinishSetup={() => void finishSetup()}
                  />
                </div>
              </div>
            </Panel>
          ) : null}
      </ShellScaffold>

      <BlockingModal open={Boolean(blockingError)} title={blockingError?.title || ""} message={blockingError?.message || ""} detail={blockingError?.detail} onConfirm={() => setBlockingError(null)} />
    </>
  );
}
