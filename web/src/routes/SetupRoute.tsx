import { useEffect, useMemo, useState } from "react";
import {
  APIRequestError,
  requestJSON,
  requestVoid,
  sendJSON,
} from "../lib/api";
import { BrandLockup, Toast } from "../components/ui";
import { navigateToLocalPath } from "../lib/navigation";
import { relativeLocalPath } from "../lib/paths";
import type {
  BootstrapState,
  FeishuAppAutoConfigCompleteView,
  FeishuAppAutoConfigCompleteResponse,
  FeishuAppAutoConfigRequirementStatus,
  FeishuAppResponse,
  OnboardingWorkflowAutoConfig,
  OnboardingWorkflowResponse,
  RuntimeRequirementsDetectResponse,
  SetupCompleteResponse,
  VSCodeDetectResponse,
} from "../lib/types";
import { blankToUndefined, vscodeApplyModeForScenario, vscodeIsReady } from "./shared/helpers";
import {
  describeAutoConfigBlockingReason,
  describeAutoConfigActionFeedback,
  describeAutoConfigHeadline,
  describeAutoConfigRefreshFeedback,
  describeAutoConfigSummary,
  groupAutoConfigRequirements,
  onboardingAutoConfigNoticeTone,
} from "./shared/feishuAutoConfig";
import {
  resolveRuntimeApplyFailureTarget,
  runAutoConfigMutation,
  saveAndVerifyFeishuApp,
  useQRCodeOnboardingFlow,
} from "./shared/feishuFlow";

type SetupStepID =
  | "runtime_requirements"
  | "connect"
  | "auto_config"
  | "menu"
  | "autostart"
  | "vscode"
  | "done";

type SetupActID = 1 | 2 | 3;

type NoticeTone = "good" | "warn" | "danger";

type Notice = {
  tone: NoticeTone;
  message: string;
};

type ManualConnectForm = {
  name: string;
  appId: string;
  appSecret: string;
};

type ImmediateAutoConfig = {
  appID: string;
  view: FeishuAppAutoConfigCompleteView;
};

const setupActs: Array<{ id: SetupActID; name: string }> = [
  { id: 1, name: "准备环境" },
  { id: 2, name: "连接飞书机器人" },
  { id: 3, name: "本机集成" },
];

const setupStepOrder: SetupStepID[] = [
  "runtime_requirements",
  "connect",
  "auto_config",
  "menu",
  "autostart",
  "vscode",
  "done",
];

const vscodeApplyTimeoutMs = 10_000;
export function SetupRoute() {
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [bootstrap, setBootstrap] = useState<BootstrapState | null>(null);
  const [workflow, setWorkflow] = useState<OnboardingWorkflowResponse | null>(null);
  const [selectedAppID, setSelectedAppID] = useState("");
  const [currentStep, setCurrentStep] =
    useState<SetupStepID>("runtime_requirements");
  const [notice, setNotice] = useState<Notice | null>(null);
  const [manualForm, setManualForm] = useState<ManualConnectForm>({
    name: "",
    appId: "",
    appSecret: "",
  });
  const [actionBusy, setActionBusy] = useState("");
  const [finishingSetup, setFinishingSetup] = useState(false);
  const [autoConfigLastCheckedAt, setAutoConfigLastCheckedAt] = useState("");
  const [immediateAutoConfig, setImmediateAutoConfig] =
    useState<ImmediateAutoConfig | null>(null);

  const activeApp = useMemo(() => {
    if (workflow?.app?.app) {
      return workflow.app.app;
    }
    return workflow?.apps.find((app) => app.id === selectedAppID) ?? null;
  }, [selectedAppID, workflow]);
  const runtimeRequirements = workflow?.runtimeRequirements || null;
  const rawAutoConfigStage = workflow?.app?.autoConfig;
  const autoConfigStage = useMemo(
    () =>
      mergeImmediateAutoConfigStage(
        rawAutoConfigStage,
        activeApp?.id,
        immediateAutoConfig,
      ),
    [activeApp?.id, immediateAutoConfig, rawAutoConfigStage],
  );
  const menuStage = workflow?.app?.menu;
  const autostartStage = workflow?.autostart || null;
  const vscodeStage = workflow?.vscode || null;
  const title = buildSetupPageTitle(bootstrap);
  const adminURL = relativeLocalPath(bootstrap?.admin.url || "/");
  const activeConsoleLinks = activeApp?.consoleLinks;
  const isReadOnlyApp = Boolean(activeApp?.readOnly);
  const stageMap = useMemo(() => {
    const next = new Map<string, string>();
    for (const stage of workflow?.stages || []) {
      next.set(stage.id, stage.status);
    }
    return next;
  }, [workflow?.stages]);
  const stepDone: Record<SetupStepID, boolean> = {
    runtime_requirements: isResolvedStageStatus(
      stageMap.get("runtime_requirements") || "",
    ),
    connect: isResolvedStageStatus(stageMap.get("connect") || ""),
    auto_config: isResolvedStageStatus(stageMap.get("auto_config") || ""),
    menu: isResolvedStageStatus(stageMap.get("menu") || ""),
    autostart: isResolvedStageStatus(stageMap.get("autostart") || ""),
    vscode: isResolvedStageStatus(stageMap.get("vscode") || ""),
    done: currentStep === "done" || normalizeSetupStepID(workflow?.currentStage) === "done",
  };
  const currentAct = setupActForStep(currentStep);
  const actDone: Record<SetupActID, boolean> = {
    1: stepDone.runtime_requirements,
    2: stepDone.connect && stepDone.auto_config && stepDone.menu,
    3: stepDone.autostart && stepDone.vscode,
  };
  const {
    connectMode,
    connectError,
    onboardingSession,
    changeConnectMode,
    clearConnectError,
    completeQRCodeSession,
    resetConnectFlow,
  } = useQRCodeOnboardingFlow({
    enabled: currentStep === "connect",
    actionBusy,
    setActionBusy,
    sessionsPath: "/api/setup/feishu/onboarding/sessions",
    onCompleteSuccess: async (appID, _session, response) => {
      rememberImmediateAutoConfig(appID, response.autoConfig);
      await loadSetupPage({ preferredAppID: appID });
      setCurrentStep("auto_config");
      setNotice(noticeFromAutoConfigView(response.autoConfig) || {
        tone: "good",
        message: "连接验证成功。",
      });
    },
  });

  useEffect(() => {
    document.title = title;
  }, [title]);

  useEffect(() => {
    let cancelled = false;
    void loadSetupPage({ showEnvironmentAdvanceNotice: false }).catch(() => {
      if (!cancelled) {
        setLoadError("当前页面暂时无法读取状态，请刷新后重试。");
        setLoading(false);
      }
    });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!activeApp) {
      setManualForm({ name: "", appId: "", appSecret: "" });
      return;
    }
    setManualForm((current) => ({
      name: current.name || activeApp.name || "",
      appId: current.appId || activeApp.appId || "",
      appSecret: current.appSecret,
    }));
  }, [activeApp?.id, activeApp?.name, activeApp?.appId]);

  useEffect(() => {
    if (typeof window.scrollTo === "function") {
      window.scrollTo({ top: 0, behavior: "auto" });
    }
  }, [currentStep]);

  async function loadSetupPage(options?: {
    preferredAppID?: string;
    preserveDisplayedStep?: boolean;
    showEnvironmentAdvanceNotice?: boolean;
  }) {
    if (!options?.preserveDisplayedStep) {
      setLoading(true);
    }
    setLoadError("");
    const workflowPath = buildOnboardingWorkflowPath(options?.preferredAppID || selectedAppID);
    const [bootstrapState, workflowState] = await Promise.all([
      requestJSON<BootstrapState>("/api/setup/bootstrap-state"),
      requestJSON<OnboardingWorkflowResponse>(workflowPath),
    ]);

    setBootstrap(bootstrapState);
    setWorkflow(workflowState);
    setSelectedAppID(workflowState.selectedAppId || "");
    setLoading(false);

    if (!options?.preserveDisplayedStep) {
      const nextStep = normalizeSetupStepID(workflowState.currentStage);
      setCurrentStep(nextStep);
      if (
        options?.showEnvironmentAdvanceNotice &&
        nextStep === "connect"
      ) {
        setNotice({ tone: "good", message: "环境正常，已自动进入飞书连接。" });
      }
    }
    return workflowState;
  }

  async function refreshWorkflow(options?: { preserveDisplayedStep?: boolean }) {
    await loadSetupPage({
      preferredAppID: activeApp?.id || selectedAppID,
      preserveDisplayedStep: options?.preserveDisplayedStep,
    });
  }

  function rememberImmediateAutoConfig(
    appID: string,
    view?: FeishuAppAutoConfigCompleteView,
  ) {
    if (!view) {
      return;
    }
    setImmediateAutoConfig({ appID, view });
  }

  async function retryEnvironmentCheck() {
    await loadSetupPage({
      preferredAppID: activeApp?.id || selectedAppID,
      showEnvironmentAdvanceNotice: true,
    });
  }

  async function submitManualConnect() {
    if (!activeApp && !manualForm.appId.trim()) {
      setNotice({ tone: "danger", message: "请填写完整的 App ID 和 App Secret。" });
      return;
    }
    if (!isReadOnlyApp && (!manualForm.appId.trim() || !manualForm.appSecret.trim())) {
      setNotice({ tone: "danger", message: "请填写完整的 App ID 和 App Secret。" });
      return;
    }

    setActionBusy("manual-connect");
    setNotice(null);
    try {
      const result = await saveAndVerifyFeishuApp({
        save: async () => {
          if (isReadOnlyApp) {
            if (!activeApp?.id) {
              throw new Error("missing active app");
            }
            return { appID: activeApp.id };
          }
          const payload = {
            name: blankToUndefined(manualForm.name),
            appId: blankToUndefined(manualForm.appId),
            appSecret: blankToUndefined(manualForm.appSecret),
            enabled: true,
          };
          const saved = activeApp?.id
            ? await sendJSON<FeishuAppResponse>(
                `/api/setup/feishu/apps/${encodeURIComponent(activeApp.id)}`,
                "PUT",
                payload,
              )
            : await sendJSON<FeishuAppResponse>("/api/setup/feishu/apps", "POST", payload);
          return { appID: saved.app.id, autoConfig: saved.autoConfig };
        },
        verifyPath: (appID) =>
          `/api/setup/feishu/apps/${encodeURIComponent(appID)}/verify`,
        reload: async (appID) => {
          await loadSetupPage({ preferredAppID: appID });
        },
      });
      if (!result.verified) {
        setNotice({
          tone: "danger",
          message: "连接验证没有通过，请检查 App ID 和 App Secret 后重试。",
        });
        return;
      }
      rememberImmediateAutoConfig(result.appID, result.autoConfig);
      setCurrentStep("auto_config");
      setNotice(noticeFromAutoConfigView(result.autoConfig) || {
        tone: "good",
        message: "连接验证成功。",
      });
    } catch (error: unknown) {
      if (await maybeRecoverRuntimeApplyFailure(error, activeApp?.id)) {
        return;
      }
      setNotice({ tone: "danger", message: "当前还不能完成连接，请稍后重试。" });
    } finally {
      setActionBusy("");
    }
  }

  async function maybeRecoverRuntimeApplyFailure(
    error: unknown,
    fallbackAppID?: string,
  ): Promise<boolean> {
    const appID = resolveRuntimeApplyFailureTarget(error, fallbackAppID);
    if (!appID) {
      return false;
    }
    await loadSetupPage({
      preferredAppID: appID,
    });
    setNotice({
      tone: "warn",
      message:
        "配置已经保存，但当前运行中的机器人还没有同步完成。你可以稍后刷新状态后再继续。",
    });
    return true;
  }

  async function completeAutoConfig() {
    if (!activeApp?.id) {
      return;
    }
    setActionBusy("auto-config-complete");
    setNotice(null);
    try {
      const result = await runAutoConfigMutation<FeishuAppAutoConfigCompleteResponse>({
        path: `/api/setup/feishu/apps/${encodeURIComponent(activeApp.id)}/auto-config/complete`,
        init: {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
          },
          body: JSON.stringify({}),
        },
        fallbackErrorMessage: "自动补齐没有完成，请稍后重试。",
        fallbackSuccessMessage: "自动配置状态已更新。",
      });
      if (!result.ok) {
        setNotice({
          tone: "danger",
          message: `${result.message} 还没有提交到飞书。`,
        });
        return;
      }
      rememberImmediateAutoConfig(result.payload.app.id, { result: result.payload.result });
      await loadSetupPage({ preferredAppID: result.payload.app.id });
      setCurrentStep("auto_config");
      setNotice(result.notice);
    } catch {
      setNotice({
        tone: "danger",
        message: "自动补齐没有完成，还没有提交到飞书。",
      });
    } finally {
      setActionBusy("");
    }
  }

  async function refreshAutoConfigResult() {
    if (!activeApp?.id) {
      return;
    }
    setActionBusy("auto-config-refresh");
    try {
      setImmediateAutoConfig(null);
      const workflowState = await loadSetupPage({
        preferredAppID: activeApp.id,
        preserveDisplayedStep: true,
      });
      setCurrentStep("auto_config");
      const nextStage = workflowState.app?.autoConfig;
      const status = autoConfigStageDisplayStatus(nextStage);
      setAutoConfigLastCheckedAt(new Date().toISOString());
      setNotice({
        tone: onboardingAutoConfigNoticeTone(nextStage?.status || "pending"),
        message: describeAutoConfigRefreshFeedback(status),
      });
    } catch {
      setNotice({
        tone: "warn",
        message: "重新检查没有完成，暂时无法确认飞书配置状态。",
      });
    } finally {
      setActionBusy("");
    }
  }

  async function deferAutoConfig() {
    if (!activeApp?.id) {
      return;
    }
    setActionBusy("auto-config-defer");
    setNotice(null);
    try {
      await requestVoid(
        `/api/setup/feishu/apps/${encodeURIComponent(activeApp.id)}/onboarding-auto-config/defer`,
        { method: "POST" },
      );
      await loadSetupPage({ preferredAppID: activeApp.id });
      setNotice({
        tone: "warn",
        message: "已按降级继续，你后续仍可回到这里重新补齐。",
      });
    } catch {
      setNotice({
        tone: "danger",
        message: "当前还不能按降级继续，请稍后重试。",
      });
    } finally {
      setActionBusy("");
    }
  }

  async function resetAutoConfigDecision() {
    if (!activeApp?.id) {
      return;
    }
    setActionBusy("auto-config-reset");
    try {
      await requestVoid(
        `/api/setup/feishu/apps/${encodeURIComponent(activeApp.id)}/onboarding-auto-config/reset`,
        { method: "POST" },
      );
      await loadSetupPage({
        preferredAppID: activeApp.id,
        preserveDisplayedStep: true,
      });
      setNotice({
        tone: "good",
        message: "已恢复自动配置检查，你可以继续补齐或发布。",
      });
    } catch {
      setNotice({
        tone: "danger",
        message: "当前还不能恢复自动配置检查，请稍后重试。",
      });
    } finally {
      setActionBusy("");
    }
  }

  async function confirmMenu() {
    if (!activeApp?.id) {
      return;
    }
    setActionBusy("menu-confirm");
    try {
      await requestVoid(
        `/api/setup/feishu/apps/${encodeURIComponent(activeApp.id)}/onboarding-menu/confirm`,
        { method: "POST" },
      );
      await loadSetupPage({ preferredAppID: activeApp.id });
      setNotice({ tone: "good", message: "已记录菜单确认结果。" });
    } catch {
      setNotice({ tone: "danger", message: "当前还不能记录菜单确认，请稍后重试。" });
    } finally {
      setActionBusy("");
    }
  }

  async function applyAutostartAndContinue() {
    setActionBusy("autostart-apply");
    try {
      await sendJSON("/api/setup/autostart/apply", "POST");
      await loadSetupPage({ preferredAppID: activeApp?.id || selectedAppID });
      setNotice({ tone: "good", message: "已启用自动启动。" });
    } catch {
      setNotice({ tone: "danger", message: "当前还不能启用自动启动，请稍后重试。" });
    } finally {
      setActionBusy("");
    }
  }

  async function saveMachineDecision(
    kind: "autostart" | "vscode",
    decision: string,
    successMessage: string,
  ) {
    setActionBusy(`${kind}-${decision}`);
    try {
      await requestVoid(`/api/setup/onboarding/machine-decisions/${kind}`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ decision }),
      });
      await loadSetupPage({ preferredAppID: activeApp?.id || selectedAppID });
      setNotice({ tone: "good", message: successMessage });
    } catch {
      setNotice({
        tone: "danger",
        message: "当前还不能保存这一步的处理结果，请稍后重试。",
      });
    } finally {
      setActionBusy("");
    }
  }

  async function applyVSCodeAndContinue() {
    if (!vscodeStage?.vscode) {
      setNotice({ tone: "danger", message: "暂时还不能完成 VS Code 集成，请稍后重试。" });
      return;
    }
    setActionBusy("vscode-apply");
    try {
      const mode = vscodeApplyModeForScenario(vscodeStage.vscode, "current_machine");
      await sendJSON<VSCodeDetectResponse>(
        "/api/setup/vscode/apply",
        "POST",
        {
          mode: mode || "managed_shim",
          bundleEntrypoint: vscodeStage.vscode.latestBundleEntrypoint,
        },
        { timeoutMs: vscodeApplyTimeoutMs },
      );
      await loadSetupPage({ preferredAppID: activeApp?.id || selectedAppID });
      setNotice({ tone: "good", message: "VS Code 集成已完成。" });
    } catch (error: unknown) {
      if (await maybeRecoverVSCodeApply(error)) {
        return;
      }
      setNotice({
        tone: "danger",
        message: "当前还不能确认 VS Code 集成结果，请稍后重试。",
      });
    } finally {
      setActionBusy("");
    }
  }

  async function maybeRecoverVSCodeApply(error: unknown): Promise<boolean> {
    try {
      const refreshed = await requestJSON<OnboardingWorkflowResponse>(
        buildOnboardingWorkflowPath(activeApp?.id || selectedAppID),
      );
      setWorkflow(refreshed);
      setSelectedAppID(refreshed.selectedAppId || "");
      if (vscodeIsReady(refreshed.vscode?.vscode || null)) {
        setNotice({ tone: "good", message: "VS Code 集成已完成。" });
        return true;
      }
    } catch {
      // fall through to timeout-specific message
    }

    if (error instanceof APIRequestError && error.code === "request_timeout") {
      setNotice({
        tone: "warn",
        message: "集成请求返回超时，当前还不能确认已完成，请稍后重试。",
      });
      return true;
    }

    return false;
  }

  async function finishSetup() {
    setFinishingSetup(true);
    try {
      const payload = await sendJSON<SetupCompleteResponse>("/api/setup/complete", "POST");
      navigateToLocalPath(relativeLocalPath(payload.adminURL || bootstrap?.admin.url || "/admin/"));
    } catch {
      navigateToLocalPath(adminURL);
    } finally {
      setFinishingSetup(false);
    }
  }

  function goToStep(stepID: SetupStepID) {
    setCurrentStep(stepID);
  }

  function goToNextStep(from: SetupStepID) {
    const currentIndex = setupStepOrder.findIndex((step) => step === from);
    const next = setupStepOrder[currentIndex + 1];
    if (next) {
      setCurrentStep(next);
    }
  }

  function renderCurrentStep() {
    switch (currentStep) {
      case "runtime_requirements":
        return renderEnvironmentStep();
      case "connect":
        return renderConnectStep();
      case "auto_config":
        return renderAutoConfigStep();
      case "menu":
        return renderMenuStep();
      case "autostart":
      case "vscode":
        return renderMachineIntegrationStep();
      case "done":
        return renderDoneStep();
      default:
        return null;
    }
  }

  function renderConnectSubsteps(current: "connect" | "auto_config" | "menu") {
    const order: Array<{ id: "connect" | "auto_config" | "menu"; label: string }> = [
      { id: "connect", label: "连接" },
      { id: "auto_config", label: "配置" },
      { id: "menu", label: "菜单" },
    ];
    const currentIndex = order.findIndex((item) => item.id === current);
    return (
      <div className="substeps">
        {order.map((item, index) => (
          <span
            key={item.id}
            className={index < currentIndex ? "done" : index === currentIndex ? "current" : ""}
          >
            {index < currentIndex ? `✓ ${item.label}` : item.label}
          </span>
        ))}
      </div>
    );
  }

  function renderMachineIntegrationStep() {
    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>本机集成</h2>
        </div>
        <div className="two-col">
          {renderAutostartCard()}
          {renderVSCodeCard()}
        </div>
        {stepDone.autostart && stepDone.vscode ? (
          <div className="button-row">
            <button
              className="primary-button"
              type="button"
              onClick={() => goToStep("done")}
            >
              完成设置
            </button>
          </div>
        ) : null}
      </section>
    );
  }

  function renderAutostartCard() {
    if (!autostartStage) {
      return (
        <article className="opt-card">
          <h3>自动运行</h3>
          <div className="opt-tag">可选</div>
          <p className="opt-status">自动启动状态暂不可用。</p>
        </article>
      );
    }
    const autostartWarning = autostartStage.autostart?.warning?.trim() || "";
    const autostartLingerHint = autostartStage.autostart?.lingerHint?.trim() || "";
    const isComplete = isResolvedStageStatus(autostartStage.status);
    return (
      <article className="opt-card">
        <h3>自动运行</h3>
        <div className="opt-tag">可选</div>
        <div className="status-line">
          <span className={`dot ${isComplete ? "good" : "idle"}`} />
          <span className={isComplete ? "txt good" : "txt idle"}>
            {isComplete ? "已处理自动运行" : autostartStage.summary}
          </span>
        </div>
        {autostartWarning ? <div className="notice warn">{autostartWarning}</div> : null}
        {autostartLingerHint ? <div className="notice warn">{autostartLingerHint}</div> : null}
        <div className="button-row">
          {autostartStage.allowedActions?.includes("apply") ? (
            <button
              className="primary-button"
              type="button"
              disabled={actionBusy === "autostart-apply"}
              onClick={() => void applyAutostartAndContinue()}
            >
              启用自动启动
            </button>
          ) : null}
          {autostartStage.allowedActions?.includes("record_enabled") ? (
            <button
              className="secondary-button"
              type="button"
              disabled={actionBusy === "autostart-enabled"}
              onClick={() =>
                void saveMachineDecision(
                  "autostart",
                  "enabled",
                  "已记录自动启动状态。",
                )
              }
            >
              保持当前状态并继续
            </button>
          ) : null}
          {autostartStage.allowedActions?.includes("defer") ? (
            <button
              className="ghost-button"
              type="button"
              aria-label="稍后处理自动运行"
              disabled={actionBusy === "autostart-deferred"}
              onClick={() =>
                void saveMachineDecision(
                  "autostart",
                  "deferred",
                  "已记录稍后处理自动启动。",
                )
              }
            >
              稍后处理
            </button>
          ) : null}
        </div>
      </article>
    );
  }

  function renderVSCodeCard() {
    if (!vscodeStage) {
      return (
        <article className="opt-card">
          <h3>VS Code 集成</h3>
          <div className="opt-tag">可选</div>
          <p className="opt-status">VS Code 集成状态暂不可用。</p>
        </article>
      );
    }
    const isComplete = isResolvedStageStatus(vscodeStage.status);
    return (
      <article className="opt-card">
        <h3>VS Code 集成</h3>
        <div className="opt-tag">可选</div>
        <div className="status-line">
          <span className={`dot ${isComplete ? "good" : "idle"}`} />
          <span className={isComplete ? "txt good" : "txt idle"}>
            {isComplete ? "VS Code 集成已处理" : vscodeStage.summary}
          </span>
        </div>
        <div className="button-row">
          {vscodeStage.allowedActions?.includes("apply") ? (
            <button
              className="primary-button"
              type="button"
              disabled={actionBusy === "vscode-apply"}
              onClick={() => void applyVSCodeAndContinue()}
            >
              完成当前机器集成
            </button>
          ) : null}
          {vscodeStage.allowedActions?.includes("record_managed_shim") ? (
            <button
              className="secondary-button"
              type="button"
              disabled={actionBusy === "vscode-managed_shim"}
              onClick={() =>
                void saveMachineDecision(
                  "vscode",
                  "managed_shim",
                  "已记录当前 VS Code 集成状态。",
                )
              }
            >
              保持当前状态并继续
            </button>
          ) : null}
          {vscodeStage.allowedActions?.includes("remote_only") ? (
            <button
              className="ghost-button"
              type="button"
              disabled={actionBusy === "vscode-remote_only"}
              onClick={() =>
                void saveMachineDecision(
                  "vscode",
                  "remote_only",
                  "已记录稍后在目标 SSH 机器上处理 VS Code 集成。",
                )
              }
            >
              留到 SSH 目标机处理
            </button>
          ) : null}
          {vscodeStage.allowedActions?.includes("defer") ? (
            <button
              className="ghost-button"
              type="button"
              aria-label="稍后处理 VS Code 集成"
              disabled={actionBusy === "vscode-deferred"}
              onClick={() =>
                void saveMachineDecision(
                  "vscode",
                  "deferred",
                  "已记录稍后处理 VS Code 集成。",
                )
              }
            >
              稍后处理
            </button>
          ) : null}
        </div>
      </article>
    );
  }

  function renderEnvironmentStep() {
    const blockingChecks = buildEnvironmentActionItems(runtimeRequirements);
    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>准备环境</h2>
        </div>
        {runtimeRequirements?.ready ? (
          <div className="notice-banner good">环境正常</div>
        ) : (
          <div className="notice-banner warn">
            {runtimeRequirements?.summary || "当前服务还在检查中，请稍候。"}
          </div>
        )}
        {blockingChecks.length > 0 ? (
          <div className="req-group">
            <div className="req-group-title danger">当前需要处理</div>
            <ul className="ordered-checklist">
              {blockingChecks.map((item) => (
                <li key={item.id}>
                  <strong>{item.title}</strong>
                  <span>{item.summary}</span>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
        {!runtimeRequirements?.ready ? (
          <div className="button-row">
            <button
              className="secondary-button"
              type="button"
              onClick={() => void retryEnvironmentCheck()}
            >
              重新检查
            </button>
          </div>
        ) : (
          <div className="button-row">
            <button
              className="primary-button"
              type="button"
              onClick={() => goToStep("connect")}
            >
              继续
            </button>
          </div>
        )}
      </section>
    );
  }

  function renderConnectStep() {
    const connectionStatus = stageMap.get("connect") || "";
    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>连接飞书机器人</h2>
        </div>
        {renderConnectSubsteps("connect")}
        {connectionStatus === "complete" ? (
          <div className="notice-banner good">当前飞书应用连接验证已通过。</div>
        ) : null}
        <div className="choice-toggle">
          <button
            className={connectMode === "qr" ? "primary-button" : "ghost-button"}
            type="button"
            onClick={() => changeConnectMode("qr")}
          >
            扫码创建
          </button>
          <button
            className={connectMode === "manual" ? "primary-button" : "ghost-button"}
            type="button"
            onClick={() => changeConnectMode("manual")}
          >
            手动输入
          </button>
        </div>
        {connectMode === "qr" ? renderQRCodePanel() : renderManualPanel()}
      </section>
    );
  }

  function renderQRCodePanel() {
    return (
      <div className="detail-stack">
        <div className="scan-preview">
          <div>
            <div className="scan-frame">
              {onboardingSession?.qrCodeDataUrl ? (
                <img alt="飞书扫码创建二维码" src={onboardingSession.qrCodeDataUrl} />
              ) : (
                <span>二维码准备中</span>
              )}
            </div>
          </div>
          <div className="detail-stack">
            {onboardingSession?.status === "pending" ? (
              <div className="notice-banner warn">正在等待扫码结果...</div>
            ) : null}
            {onboardingSession?.status === "ready" && !connectError ? (
              <div className="notice-banner good">
                扫码成功，连接验证已通过，正在进入飞书自动配置...
              </div>
            ) : null}
            {onboardingSession?.status === "failed" ||
            onboardingSession?.status === "expired" ||
            connectError ? (
              <div className="notice-banner danger">
                {connectError || "当前扫码没有继续成功，请重新开始。"}
              </div>
            ) : null}
            <div className="button-row">
              {(connectError ||
                onboardingSession?.status === "failed" ||
                onboardingSession?.status === "expired") && (
                <button
                  className="primary-button"
                  type="button"
                  disabled={actionBusy === "qr-start"}
                  onClick={() => resetConnectFlow()}
                >
                  重新扫码
                </button>
              )}
              {onboardingSession?.status === "ready" && connectError ? (
                <button
                  className="primary-button"
                  type="button"
                  disabled={actionBusy === "qr-complete"}
                  onClick={() => {
                    if (onboardingSession?.id) {
                      clearConnectError();
                      void completeQRCodeSession(onboardingSession.id);
                    }
                  }}
                >
                  重新验证
                </button>
              ) : null}
              <button
                className="ghost-button"
                type="button"
                onClick={() => changeConnectMode("manual")}
              >
                改用手动输入
              </button>
            </div>
          </div>
        </div>
      </div>
    );
  }

  function renderManualPanel() {
    return (
      <div className="detail-stack">
        {isReadOnlyApp ? (
          <div className="notice-banner warn">
            当前机器人信息由当前运行环境提供，网页里不能修改，只能完成连接验证。
          </div>
        ) : null}
        <div className="form-grid">
          <label className="field">
            <span>
              App ID <em className="field-required">*</em>
            </span>
            <input
              aria-label="App ID"
              disabled={isReadOnlyApp}
              placeholder="请输入 App ID"
              value={manualForm.appId}
              onChange={(event) =>
                setManualForm((current) => ({
                  ...current,
                  appId: event.target.value,
                }))
              }
            />
          </label>
          <label className="field">
            <span>
              App Secret <em className="field-required">*</em>
            </span>
            <input
              aria-label="App Secret"
              disabled={isReadOnlyApp}
              placeholder="请输入 App Secret"
              value={manualForm.appSecret}
              onChange={(event) =>
                setManualForm((current) => ({
                  ...current,
                  appSecret: event.target.value,
                }))
              }
            />
          </label>
          <label className="field form-grid-span-2">
            <span>机器人名称（可选）</span>
            <input
              aria-label="机器人名称（可选）"
              disabled={isReadOnlyApp}
              placeholder="例如：团队机器人"
              value={manualForm.name}
              onChange={(event) =>
                setManualForm((current) => ({
                  ...current,
                  name: event.target.value,
                }))
              }
            />
          </label>
        </div>
        <div className="button-row">
          <button
            className="primary-button"
            type="button"
            disabled={actionBusy === "manual-connect"}
            onClick={() => void submitManualConnect()}
          >
            验证并继续
          </button>
        </div>
      </div>
    );
  }

  function renderAutoConfigStep() {
    if (!activeApp || !autoConfigStage) {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>配置飞书机器人</h2>
            <p>请先完成飞书连接。</p>
          </div>
          <div className="notice-banner warn">当前还没有可用的飞书应用。</div>
        </section>
      );
    }

    const plan = autoConfigStage.plan;
    const displayStatus = autoConfigStage.resultStatus || plan?.status || autoConfigStage.status;
    const busy =
      actionBusy === "auto-config-complete" ||
      actionBusy === "auto-config-defer" ||
      actionBusy === "auto-config-reset" ||
      actionBusy === "auto-config-refresh";
    const canComplete =
      autoConfigStage.allowedActions?.includes("apply") ||
      autoConfigStage.allowedActions?.includes("publish");

    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>配置飞书机器人</h2>
        </div>
        {renderConnectSubsteps("auto_config")}

        <div className={`notice-banner ${onboardingAutoConfigNoticeTone(autoConfigStage.status)}`}>
          {autoConfigStage.summary?.trim() ||
            (plan ? describeAutoConfigSummary(displayStatus) : "当前还没有读取到自动配置状态。")}
        </div>

        <div className="detail-stack">
          <div className="section-heading">
            <div>
              <h4>{describeAutoConfigHeadline(displayStatus)}</h4>
              <p>
                {plan?.summary?.trim() ||
                  autoConfigStage.summary?.trim() ||
                  describeAutoConfigSummary(displayStatus)}
              </p>
            </div>
          </div>

          {plan?.blockingReason ? (
            <p className="support-copy">
              {describeAutoConfigBlockingReason(plan.blockingReason)}
            </p>
          ) : null}

          {renderAutoConfigRequirementList(
            "需要先解决的问题",
            plan?.blockingRequirements || [],
            "danger",
          )}
          {renderAutoConfigRequirementList(
            "可按降级继续的能力",
            plan?.degradableRequirements || [],
            "warn",
          )}

          <div className="button-row">
            {canComplete ? (
              <button
                className="primary-button"
                type="button"
                disabled={busy}
                onClick={() => void completeAutoConfig()}
              >
                {actionBusy === "auto-config-complete" ? "补齐中..." : "自动补齐"}
              </button>
            ) : null}
            {autoConfigStage.allowedActions?.includes("defer") ? (
              <button
                className="secondary-button"
                type="button"
                disabled={busy}
                onClick={() => void deferAutoConfig()}
              >
                {actionBusy === "auto-config-defer" ? "继续中..." : "先按降级继续"}
              </button>
            ) : null}
            {autoConfigStage.status === "deferred" ? (
              <button
                className="secondary-button"
                type="button"
                disabled={busy}
                onClick={() => void resetAutoConfigDecision()}
              >
                {actionBusy === "auto-config-reset" ? "检查中..." : "重新检查自动配置"}
              </button>
            ) : (
              <button
                className="secondary-button"
                type="button"
                disabled={busy}
                onClick={() => void refreshAutoConfigResult()}
              >
                {actionBusy === "auto-config-refresh" ? "检查中..." : "重新检查"}
              </button>
            )}
            {activeConsoleLinks?.auth ? (
              <a
                className="ghost-button"
                href={activeConsoleLinks.auth}
                rel="noreferrer"
                target="_blank"
              >
                打开飞书后台
              </a>
            ) : null}
          </div>
          {autoConfigLastCheckedAt ? (
            <p className="support-copy">
              最近检查：{formatAutoConfigCheckedAt(autoConfigLastCheckedAt)}
            </p>
          ) : null}
        </div>
      </section>
    );
  }

  function renderMenuStep() {
    if (!activeApp || !menuStage) {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>确认机器人菜单</h2>
            <p>请先完成前面的步骤。</p>
          </div>
          <div className="notice-banner warn">当前还没有可继续的飞书应用。</div>
        </section>
      );
    }

    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>确认机器人菜单</h2>
          <p>在飞书后台确认机器人菜单配置，然后回到这里继续。</p>
        </div>
        {renderConnectSubsteps("menu")}
        <div className={`notice-banner ${menuStage.status === "complete" ? "good" : menuStage.status === "blocked" ? "warn" : "warn"}`}>
          {menuStage.summary}
        </div>
        <div className="button-row">
          {activeConsoleLinks?.bot ? (
            <a
              className="ghost-button"
              href={activeConsoleLinks.bot}
              rel="noreferrer"
              target="_blank"
            >
              打开飞书后台
            </a>
          ) : null}
          {menuStage.allowedActions?.includes("confirm") ? (
            <button
              className="primary-button"
              type="button"
              disabled={actionBusy === "menu-confirm"}
              onClick={() => void confirmMenu()}
            >
              我已完成菜单确认
            </button>
          ) : null}
          {menuStage.status === "complete" ? (
            <button
              className="primary-button"
              type="button"
              onClick={() => goToNextStep("menu")}
            >
              继续
            </button>
          ) : null}
        </div>
      </section>
    );
  }

  function renderDoneStep() {
    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>欢迎使用</h2>
        </div>
        <div className="completed-card">
          <h3>欢迎，设置已经完成。</h3>
          <p>你可以在管理页面继续调整设置、查看存储状态。</p>
        </div>
        <div className="button-row">
          <button
            className="primary-button"
            type="button"
            disabled={finishingSetup}
            onClick={() => void finishSetup()}
          >
            进入管理页面
          </button>
        </div>
      </section>
    );
  }

  if (loading) {
    return (
      <div className="setup-page">
        <header className="topbar">
          <BrandLockup subtitle="安装向导" />
        </header>
        {renderSetupProgress(currentAct, actDone)}
        <main className="column">
        <section className="card">
          <div className="empty-state">
            <div className="loading-dot" />
            <span>正在读取最新状态</span>
          </div>
        </section>
        </main>
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="setup-page">
        <header className="topbar">
          <BrandLockup subtitle="安装向导" />
        </header>
        {renderSetupProgress(currentAct, actDone)}
        <main className="column">
        <section className="card">
          <div className="empty-state error">
            <strong>当前页面暂时无法打开</strong>
            <p>{loadError}</p>
            <div className="button-row">
              <button
                className="secondary-button"
                type="button"
                onClick={() => void loadSetupPage()}
              >
                重新加载
              </button>
            </div>
          </div>
        </section>
        </main>
      </div>
    );
  }

  return (
    <div className="setup-page">
      <header className="topbar">
        <BrandLockup subtitle="安装向导" />
      </header>
      {renderSetupProgress(currentAct, actDone)}
      <main className="column">
        {notice ? <Toast tone={notice.tone} message={notice.message} /> : null}
        {currentStep === "done" ? (
          <section className="card done-card">{renderDoneStep()}</section>
        ) : (
          <section className="card">{renderCurrentStep()}</section>
        )}
      </main>

    </div>
  );
}

function renderSetupProgress(
  currentAct: SetupActID | 4,
  actDone: Record<SetupActID, boolean>,
) {
  return (
    <div className="progress-wrap">
      <div className="progress" aria-hidden="true">
        {setupActs.map((act) => (
          <div
            key={act.id}
            className={`seg${actDone[act.id] ? " done" : ""}${act.id === currentAct ? " active" : ""}`}
          />
        ))}
      </div>
      <div className="progress-labels">
        {setupActs.map((act) => (
          <span
            key={act.id}
            className={`${actDone[act.id] ? "done" : ""}${act.id === currentAct ? " active" : ""}`.trim()}
          >
            {act.name}
          </span>
        ))}
      </div>
    </div>
  );
}

function buildOnboardingWorkflowPath(preferredAppID: string): string {
  const appID = preferredAppID.trim();
  if (!appID) {
    return "/api/setup/onboarding/workflow";
  }
  return `/api/setup/onboarding/workflow?app=${encodeURIComponent(appID)}`;
}

function normalizeSetupStepID(value: string | undefined): SetupStepID {
  switch (value) {
    case "connect":
    case "auto_config":
    case "menu":
    case "autostart":
    case "vscode":
    case "done":
      return value;
    default:
      return "runtime_requirements";
  }
}

function isResolvedStageStatus(status: string): boolean {
  return status === "complete" || status === "deferred" || status === "not_applicable";
}

function mergeImmediateAutoConfigStage(
  stage: OnboardingWorkflowAutoConfig | undefined,
  appID: string | undefined,
  immediate: ImmediateAutoConfig | null,
): OnboardingWorkflowAutoConfig | undefined {
  if (!stage || !appID || !immediate || immediate.appID !== appID) {
    return stage;
  }
  const result = immediate.view.result;
  if (!result) {
    if (!immediate.view.error) {
      return stage;
    }
    return {
      ...stage,
      status: "pending",
      resultStatus: "verification_failed",
      summary: "已保存机器人，但暂时无法确认飞书配置结果。",
      error: immediate.view.error,
      allowedActions: mergeAllowedActions(stage.allowedActions, ["apply", "retry", "defer"]),
    };
  }
  return {
    ...stage,
    status: onboardingStageStatusFromAutoConfigResult(result.status),
    resultStatus: result.status,
    summary: result.summary?.trim() || describeAutoConfigSummary(result.status),
    plan: result.plan || stage.plan,
    allowedActions: allowedActionsForImmediateAutoConfig(stage.allowedActions, result.status),
  };
}

function onboardingStageStatusFromAutoConfigResult(status: string): string {
  switch (status) {
    case "clean":
    case "degraded":
      return "complete";
    case "blocked":
    case "unsupported":
      return "blocked";
    default:
      return "pending";
  }
}

function allowedActionsForImmediateAutoConfig(
  existing: string[] | undefined,
  status: string,
): string[] {
  switch (status) {
    case "apply_required":
    case "publish_required":
    case "verification_failed":
      return mergeAllowedActions(existing, ["apply", "retry", "defer"]);
    case "awaiting_review":
      return mergeAllowedActions(existing, ["retry", "defer"]);
    default:
      return mergeAllowedActions(existing, ["retry"]);
  }
}

function mergeAllowedActions(
  existing: string[] | undefined,
  extra: string[],
): string[] {
  return Array.from(new Set([...(existing || []), ...extra]));
}

function noticeFromAutoConfigView(
  view?: FeishuAppAutoConfigCompleteView,
): Notice | null {
  if (!view) {
    return null;
  }
  if (view.result) {
    return {
      tone: onboardingAutoConfigNoticeTone(
        onboardingStageStatusFromAutoConfigResult(view.result.status),
      ),
      message: describeAutoConfigActionFeedback(view.result),
    };
  }
  if (view.error) {
    return {
      tone: "warn",
      message: "已保存机器人，但暂时无法确认飞书配置结果。",
    };
  }
  return null;
}

function autoConfigStageDisplayStatus(
  stage: OnboardingWorkflowAutoConfig | undefined,
): string {
  return stage?.resultStatus || stage?.plan?.status || stage?.status || "";
}

function formatAutoConfigCheckedAt(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "刚刚";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(date);
}

function renderAutoConfigRequirementList(
  title: string,
  requirements: FeishuAppAutoConfigRequirementStatus[],
  tone: "warn" | "danger",
) {
  if (requirements.length === 0) {
    return null;
  }
  const rows = groupAutoConfigRequirements(requirements);
  return (
    <div className="req-group">
      <div className={`req-group-title ${tone}`}>{title}</div>
      <ul className="requirement-list">
        {rows.map((item) => {
          return (
            <li key={item.key} className="requirement-row">
              <div className="requirement-main">
                <span className={`badge ${tone === "danger" ? "danger" : "warn"}`}>
                  {item.meta}
                </span>
                <strong className="mono">{item.label}</strong>
              </div>
              <div className="requirement-impact">
                {item.impacts.length > 0 ? item.impacts.join("、") : "基础配置"}
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

type EnvironmentActionItem = {
  id: string;
  title: string;
  summary: string;
};

function buildEnvironmentActionItems(
  runtimeRequirements: RuntimeRequirementsDetectResponse | null,
): EnvironmentActionItem[] {
  if (!runtimeRequirements || runtimeRequirements.ready) {
    return [];
  }

  const hasFail = (id: string) =>
    runtimeRequirements.checks.some(
      (check) => check.id === id && check.status === "fail",
    );
  const items: EnvironmentActionItem[] = [];

  if (hasFail("headless_launcher")) {
    items.push({
      id: "headless_launcher",
      title: "本机服务",
      summary: "当前服务还不能正常启动，请先修复后再重新检查。",
    });
  }

  if (
    hasFail("binary_loop") ||
    (hasFail("real_codex_binary") && hasFail("claude_binary"))
  ) {
    items.push({
      id: "available_backend",
      title: "对话后端",
      summary: "请先保证 Claude 或 Codex 至少一个可用。",
    });
  }

  return items;
}

function buildSetupPageTitle(bootstrap: BootstrapState | null): string {
  const name = bootstrap?.product.name?.trim() || "Codex Remote Feishu";
  const version = bootstrap?.product.version?.trim();
  return version ? `${name} ${version} 安装程序` : `${name} 安装程序`;
}

function setupActForStep(stepID: SetupStepID): SetupActID | 4 {
  switch (stepID) {
    case "runtime_requirements":
      return 1;
    case "connect":
    case "auto_config":
    case "menu":
      return 2;
    case "autostart":
    case "vscode":
      return 3;
    case "done":
      return 4;
  }
}
