import { useEffect, useMemo, useState } from "react";
import {
  APIRequestError,
  type APIErrorShape,
  type JSONResult,
  requestJSON,
  requestJSONAllowHTTPError,
  requestVoid,
  sendJSON,
} from "../lib/api";
import { navigateToLocalPath } from "../lib/navigation";
import { relativeLocalPath } from "../lib/paths";
import type {
  BootstrapState,
  FeishuAppAutoConfigApplyResponse,
  FeishuAppAutoConfigPublishResponse,
  FeishuAppAutoConfigRequirementStatus,
  FeishuAppResponse,
  FeishuAppSummary,
  FeishuAppVerifyResponse,
  FeishuOnboardingCompleteResponse,
  FeishuOnboardingSession,
  FeishuOnboardingSessionResponse,
  OnboardingWorkflowResponse,
  RuntimeRequirementsDetectResponse,
  SetupCompleteResponse,
  VSCodeDetectResponse,
} from "../lib/types";
import { vscodeApplyModeForScenario, vscodeIsReady } from "./shared/helpers";
import {
  autoConfigNoticeTone,
  describeAutoConfigBlockingReason,
  describeAutoConfigHeadline,
  describeAutoConfigRequirementDetail,
  describeAutoConfigRequirementLabel,
  describeAutoConfigSummary,
} from "./shared/feishuAutoConfig";

type SetupStepID =
  | "runtime_requirements"
  | "connect"
  | "auto_config"
  | "menu"
  | "autostart"
  | "vscode"
  | "done";

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

type RuntimeApplyFailureDetails = {
  gatewayId?: string;
  app?: FeishuAppSummary;
};

const setupSteps: Array<{ id: SetupStepID; name: string }> = [
  { id: "runtime_requirements", name: "环境检查" },
  { id: "connect", name: "飞书连接" },
  { id: "auto_config", name: "飞书自动配置" },
  { id: "menu", name: "菜单确认" },
  { id: "autostart", name: "自动启动" },
  { id: "vscode", name: "VS Code 集成" },
  { id: "done", name: "完成" },
];

const defaultQRCodePollIntervalSeconds = 5;
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
  const [connectMode, setConnectMode] = useState<"qr" | "manual">("qr");
  const [manualForm, setManualForm] = useState<ManualConnectForm>({
    name: "",
    appId: "",
    appSecret: "",
  });
  const [actionBusy, setActionBusy] = useState("");
  const [onboardingSession, setOnboardingSession] =
    useState<FeishuOnboardingSession | null>(null);
  const [connectError, setConnectError] = useState("");
  const [publishConfirmOpen, setPublishConfirmOpen] = useState(false);
  const [finishingSetup, setFinishingSetup] = useState(false);

  const activeApp = useMemo(() => {
    if (workflow?.app?.app) {
      return workflow.app.app;
    }
    return workflow?.apps.find((app) => app.id === selectedAppID) ?? null;
  }, [selectedAppID, workflow]);
  const runtimeRequirements = workflow?.runtimeRequirements || null;
  const autoConfigStage = workflow?.app?.autoConfig;
  const menuStage = workflow?.app?.menu;
  const autostartStage = workflow?.autostart || null;
  const vscodeStage = workflow?.vscode || null;
  const title = buildSetupPageTitle(bootstrap);
  const adminURL = relativeLocalPath(bootstrap?.admin.url || "/");
  const activeConsoleLinks = activeApp?.consoleLinks;
  const isReadOnlyApp = Boolean(activeApp?.readOnly);
  const currentStageIndex = setupSteps.findIndex(
    (step) => step.id === normalizeSetupStepID(workflow?.currentStage),
  );
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

  useEffect(() => {
    if (currentStep !== "connect" || connectMode !== "qr") {
      return;
    }
    if (actionBusy === "qr-start" || actionBusy === "qr-complete") {
      return;
    }
    if (!onboardingSession) {
      if (!connectError) {
        void startQRCodeSession();
      }
      return;
    }
    if (onboardingSession.status === "ready" && !connectError) {
      void completeQRCodeSession(onboardingSession.id);
      return;
    }
    if (onboardingSession.status !== "pending") {
      return;
    }
    const pollDelaySeconds = Math.max(
      onboardingSession.pollIntervalSeconds || defaultQRCodePollIntervalSeconds,
      defaultQRCodePollIntervalSeconds,
    );
    const timer = window.setTimeout(() => {
      void refreshQRCodeSession(onboardingSession.id);
    }, pollDelaySeconds * 1_000);
    return () => window.clearTimeout(timer);
  }, [actionBusy, connectError, connectMode, currentStep, onboardingSession]);

  useEffect(() => {
    setPublishConfirmOpen(false);
  }, [selectedAppID]);

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
  }

  async function refreshWorkflow(options?: { preserveDisplayedStep?: boolean }) {
    await loadSetupPage({
      preferredAppID: activeApp?.id || selectedAppID,
      preserveDisplayedStep: options?.preserveDisplayedStep,
    });
  }

  async function retryEnvironmentCheck() {
    await loadSetupPage({
      preferredAppID: activeApp?.id || selectedAppID,
      showEnvironmentAdvanceNotice: true,
    });
  }

  function changeConnectMode(nextMode: "qr" | "manual") {
    setConnectMode(nextMode);
    setConnectError("");
    setOnboardingSession(null);
  }

  async function startQRCodeSession() {
    setActionBusy("qr-start");
    setConnectError("");
    try {
      const response = await sendJSON<FeishuOnboardingSessionResponse>(
        "/api/setup/feishu/onboarding/sessions",
        "POST",
      );
      setOnboardingSession(response.session);
    } catch {
      setConnectError("暂时无法开始扫码，请稍后重试。");
    } finally {
      setActionBusy("");
    }
  }

  async function refreshQRCodeSession(sessionID: string) {
    try {
      const response = await requestJSON<FeishuOnboardingSessionResponse>(
        `/api/setup/feishu/onboarding/sessions/${encodeURIComponent(sessionID)}`,
      );
      setOnboardingSession(response.session);
      if (response.session.status === "pending") {
        setConnectError("");
      }
    } catch {
      setConnectError("扫码状态暂时没有刷新成功，请稍后重试。");
    }
  }

  async function completeQRCodeSession(sessionID: string) {
    setActionBusy("qr-complete");
    try {
      const response = await requestJSONAllowHTTPError<FeishuOnboardingCompleteResponse>(
        `/api/setup/feishu/onboarding/sessions/${encodeURIComponent(sessionID)}/complete`,
        { method: "POST" },
      );
      setOnboardingSession(response.data.session);
      if (!response.ok) {
        setConnectError("扫码已经完成，但连接验证没有通过，请重新验证。");
        return;
      }
      await loadSetupPage({ preferredAppID: response.data.app.id });
      setNotice({ tone: "good", message: "连接验证成功。" });
      setConnectError("");
    } catch {
      setConnectError("扫码已经完成，但当前还不能继续，请稍后重试。");
    } finally {
      setActionBusy("");
    }
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
      let appID = activeApp?.id || "";
      if (!isReadOnlyApp) {
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
        appID = saved.app.id;
      }
      const verify = await requestJSONAllowHTTPError<FeishuAppVerifyResponse>(
        `/api/setup/feishu/apps/${encodeURIComponent(appID)}/verify`,
        { method: "POST" },
      );
      await loadSetupPage({ preferredAppID: appID });
      if (!verify.ok) {
        setNotice({
          tone: "danger",
          message: "连接验证没有通过，请检查 App ID 和 App Secret 后重试。",
        });
        return;
      }
      setNotice({ tone: "good", message: "连接验证成功。" });
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
    if (!(error instanceof APIRequestError) || error.code !== "gateway_apply_failed") {
      return false;
    }
    const details = error.details as RuntimeApplyFailureDetails | undefined;
    await loadSetupPage({
      preferredAppID: details?.app?.id || details?.gatewayId || fallbackAppID,
    });
    setNotice({
      tone: "warn",
      message:
        "配置已经保存，但当前运行中的机器人还没有同步完成。你可以稍后刷新状态后再继续。",
    });
    return true;
  }

  async function applyAutoConfig() {
    if (!activeApp?.id) {
      return;
    }
    setActionBusy("auto-config-apply");
    setNotice(null);
    try {
      const response = await requestJSONAllowHTTPError<
        FeishuAppAutoConfigApplyResponse | APIErrorShape
      >(`/api/setup/feishu/apps/${encodeURIComponent(activeApp.id)}/auto-config/apply`, {
        method: "POST",
      });
      if (!response.ok) {
        const payload = readAPIError(response);
        setNotice({
          tone: "danger",
          message:
            typeof payload?.details === "string" && payload.details.trim()
              ? payload.details.trim()
              : "自动补齐没有完成，请稍后重试。",
        });
        return;
      }
      const payload = response.data as FeishuAppAutoConfigApplyResponse;
      await loadSetupPage({ preferredAppID: payload.app.id });
      setNotice({
        tone: autoConfigNoticeTone(payload.result.status),
        message: payload.result.summary?.trim() || "自动配置状态已更新。",
      });
    } catch {
      setNotice({
        tone: "danger",
        message: "自动补齐没有完成，请稍后重试。",
      });
    } finally {
      setActionBusy("");
    }
  }

  async function publishAutoConfig() {
    if (!activeApp?.id) {
      return;
    }
    setActionBusy("auto-config-publish");
    setNotice(null);
    try {
      const response = await requestJSONAllowHTTPError<
        FeishuAppAutoConfigPublishResponse | APIErrorShape
      >(`/api/setup/feishu/apps/${encodeURIComponent(activeApp.id)}/auto-config/publish`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({}),
      });
      if (!response.ok) {
        const payload = readAPIError(response);
        setNotice({
          tone: "danger",
          message:
            typeof payload?.details === "string" && payload.details.trim()
              ? payload.details.trim()
              : "提交发布没有成功，请稍后重试。",
        });
        return;
      }
      const payload = response.data as FeishuAppAutoConfigPublishResponse;
      await loadSetupPage({ preferredAppID: payload.app.id });
      setNotice({
        tone: autoConfigNoticeTone(payload.result.status),
        message: payload.result.summary?.trim() || "发布状态已更新。",
      });
      setPublishConfirmOpen(false);
    } catch {
      setNotice({
        tone: "danger",
        message: "提交发布没有成功，请稍后重试。",
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
    const currentIndex = setupSteps.findIndex((step) => step.id === from);
    const next = setupSteps[currentIndex + 1];
    if (next) {
      setCurrentStep(next.id);
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
        return renderAutostartStep();
      case "vscode":
        return renderVSCodeStep();
      case "done":
        return renderDoneStep();
      default:
        return null;
    }
  }

  function renderEnvironmentStep() {
    const blockingChecks = buildEnvironmentActionItems(runtimeRequirements);
    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>环境检查</h2>
          <p>确认服务与运行条件正常</p>
        </div>
        {runtimeRequirements?.ready ? (
          <div className="notice-banner good">环境正常</div>
        ) : (
          <div className="notice-banner warn">
            {runtimeRequirements?.summary || "当前服务还在检查中，请稍候。"}
          </div>
        )}
        {blockingChecks.length > 0 ? (
          <div className="panel">
            <div className="section-heading">
              <div>
                <h4>当前需要处理</h4>
                <p>请先修复下面的问题，再重新检查。</p>
              </div>
            </div>
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
          <h2>飞书连接</h2>
          <p>扫码创建或手动输入接入飞书应用</p>
        </div>
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
      <div className="panel">
        <div className="scan-preview">
          <div>
            <h4 style={{ margin: 0 }}>扫码创建</h4>
            <p className="support-copy">
              使用飞书扫描二维码，页面将自动完成后续操作。
            </p>
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
                  className="secondary-button"
                  type="button"
                  disabled={actionBusy === "qr-start"}
                  onClick={() => {
                    setOnboardingSession(null);
                    setConnectError("");
                  }}
                >
                  重新扫码
                </button>
              )}
              {onboardingSession?.status === "ready" && connectError ? (
                <button
                  className="secondary-button"
                  type="button"
                  disabled={actionBusy === "qr-complete"}
                  onClick={() => {
                    if (onboardingSession?.id) {
                      setConnectError("");
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
      <div className="panel">
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
            <h2>飞书自动配置</h2>
            <p>请先完成飞书连接。</p>
          </div>
          <div className="notice-banner warn">当前还没有可用的飞书应用。</div>
        </section>
      );
    }

    const plan = autoConfigStage.plan;
    const busy =
      actionBusy === "auto-config-apply" ||
      actionBusy === "auto-config-publish" ||
      actionBusy === "auto-config-defer" ||
      actionBusy === "auto-config-reset";

    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>飞书自动配置</h2>
          <p>
            自动检查并尽可能补齐权限、事件、回调和发布状态，避免再走测试消息路径。
          </p>
        </div>

        <div className={`notice-banner ${autoConfigBannerTone(autoConfigStage.status)}`}>
          {autoConfigStage.summary?.trim() ||
            (plan ? describeAutoConfigSummary(plan.status) : "当前还没有读取到自动配置状态。")}
        </div>

        <div className="panel">
          <div className="section-heading">
            <div>
              <h4>{describeAutoConfigHeadline(plan?.status || autoConfigStage.status)}</h4>
              <p>
                {plan?.summary?.trim() ||
                  autoConfigStage.summary?.trim() ||
                  describeAutoConfigSummary(plan?.status || autoConfigStage.status)}
              </p>
            </div>
          </div>

          {plan?.blockingReason ? (
            <p className="support-copy">
              当前原因：{describeAutoConfigBlockingReason(plan.blockingReason)}
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
            {autoConfigStage.allowedActions?.includes("apply") ? (
              <button
                className="primary-button"
                type="button"
                disabled={busy}
                onClick={() => void applyAutoConfig()}
              >
                自动补齐
              </button>
            ) : null}
            {autoConfigStage.allowedActions?.includes("publish") ? (
              <button
                className="primary-button"
                type="button"
                disabled={busy}
                onClick={() => setPublishConfirmOpen(true)}
              >
                继续发布
              </button>
            ) : null}
            {autoConfigStage.allowedActions?.includes("defer") ? (
              <button
                className="ghost-button"
                type="button"
                disabled={busy}
                onClick={() => void deferAutoConfig()}
              >
                先按降级继续
              </button>
            ) : null}
            {autoConfigStage.status === "deferred" ? (
              <button
                className="secondary-button"
                type="button"
                disabled={busy}
                onClick={() => void resetAutoConfigDecision()}
              >
                重新检查自动配置
              </button>
            ) : (
              <button
                className="secondary-button"
                type="button"
                disabled={busy}
                onClick={() => void refreshWorkflow({ preserveDisplayedStep: true })}
              >
                刷新结果
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
        </div>
      </section>
    );
  }

  function renderMenuStep() {
    if (!activeApp || !menuStage) {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>菜单确认</h2>
            <p>请先完成前面的步骤。</p>
          </div>
          <div className="notice-banner warn">当前还没有可继续的飞书应用。</div>
        </section>
      );
    }

    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>菜单确认</h2>
          <p>在飞书后台确认机器人菜单配置，然后回到这里继续。</p>
        </div>
        <div className={`notice-banner ${menuStage.status === "complete" ? "good" : menuStage.status === "blocked" ? "warn" : "warn"}`}>
          {menuStage.summary}
        </div>
        <div className="button-row">
          {activeConsoleLinks?.bot ? (
            <a
              className="secondary-button"
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
              className="ghost-button"
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

  function renderAutostartStep() {
    if (!autostartStage) {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>自动启动</h2>
            <p>自动启动状态暂不可用。</p>
          </div>
        </section>
      );
    }

    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>自动启动</h2>
          <p>决定是否在登录后自动运行。</p>
        </div>
        <div className={`notice-banner ${autostartStage.status === "complete" ? "good" : "warn"}`}>
          {autostartStage.summary}
        </div>
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
          {isResolvedStageStatus(autostartStage.status) ? (
            <button
              className="ghost-button"
              type="button"
              onClick={() => goToNextStep("autostart")}
            >
              继续
            </button>
          ) : null}
        </div>
      </section>
    );
  }

  function renderVSCodeStep() {
    if (!vscodeStage) {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>VS Code 集成</h2>
            <p>VS Code 集成状态暂不可用。</p>
          </div>
        </section>
      );
    }

    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>VS Code 集成</h2>
          <p>决定如何处理这台机器上的 VS Code 集成。</p>
        </div>
        <div className={`notice-banner ${vscodeStage.status === "complete" ? "good" : "warn"}`}>
          {vscodeStage.summary}
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
          {isResolvedStageStatus(vscodeStage.status) ? (
            <button
              className="ghost-button"
              type="button"
              onClick={() => goToNextStep("vscode")}
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
          <p>基础设置已完成。</p>
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
      <div className="product-page">
        <header className="product-topbar">
          <h1>{title}</h1>
        </header>
        <section className="panel">
          <div className="empty-state">
            <div className="loading-dot" />
            <span>正在读取最新状态</span>
          </div>
        </section>
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="product-page">
        <header className="product-topbar">
          <h1>{title}</h1>
        </header>
        <section className="panel">
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
      </div>
    );
  }

  return (
    <div className="product-page">
      <header className="product-topbar">
        <h1>{title}</h1>
      </header>
      {notice ? (
        <div className="product-notice-slot">
          <div className={`notice-banner ${notice.tone}`}>{notice.message}</div>
        </div>
      ) : null}
      <main className="setup-grid">
        <aside className="panel step-rail">
          <div className="step-stage-head">
            <h2>设置流程</h2>
            <p>共 7 步</p>
          </div>
          <div className="step-list">
            {setupSteps.map((step, index) => {
              const stageStatus = stageMap.get(step.id) || "";
              const disabled =
                currentStageIndex >= 0 &&
                index > currentStageIndex &&
                !isResolvedStageStatus(stageStatus);
              return (
                <button
                  key={step.id}
                  className={`step-item${step.id === currentStep ? " active" : ""}${stepDone[step.id] ? " done" : ""}`}
                  disabled={disabled}
                  type="button"
                  onClick={() => goToStep(step.id)}
                >
                  <strong>{step.name}</strong>
                  <span>
                    {step.id === currentStep
                      ? "进行中"
                      : stepDone[step.id]
                        ? "已完成"
                        : "待处理"}
                  </span>
                </button>
              );
            })}
          </div>
        </aside>
        <section className="panel step-stage">{renderCurrentStep()}</section>
      </main>

      {publishConfirmOpen ? (
        <div className="modal-backdrop" role="presentation">
          <div
            className="modal-card"
            role="dialog"
            aria-modal="true"
            aria-labelledby="publish-app-title"
          >
            <h3 id="publish-app-title">确认提交发布</h3>
            <p className="modal-copy">
              这会把当前自动补齐后的飞书配置提交到发布流程。若飞书要求管理员审核，后续状态会显示为“等待管理员处理”。
            </p>
            <div className="modal-actions">
              <button
                className="ghost-button"
                type="button"
                onClick={() => setPublishConfirmOpen(false)}
              >
                取消
              </button>
              <button
                className="primary-button"
                type="button"
                disabled={actionBusy === "auto-config-publish"}
                onClick={() => void publishAutoConfig()}
              >
                确认提交
              </button>
            </div>
          </div>
        </div>
      ) : null}
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

function autoConfigBannerTone(status: string): NoticeTone {
  switch (status) {
    case "complete":
      return "good";
    case "deferred":
      return "warn";
    case "blocked":
      return "danger";
    default:
      return "warn";
  }
}

function renderAutoConfigRequirementList(
  title: string,
  requirements: FeishuAppAutoConfigRequirementStatus[],
  tone: "warn" | "danger",
) {
  if (requirements.length === 0) {
    return null;
  }
  return (
    <div className="panel">
      <div className="section-heading">
        <div>
          <h4>{title}</h4>
          <p>
            {tone === "danger"
              ? "这些问题会阻塞当前 setup。"
              : "这些问题不会阻塞 setup，但会影响部分能力。"}
          </p>
        </div>
      </div>
      <ul className="ordered-checklist">
        {requirements.map((item) => (
          <li key={`${item.kind}-${item.key}`}>
            <strong>{describeAutoConfigRequirementLabel(item)}</strong>
            {describeAutoConfigRequirementDetail(item)
              ? `：${describeAutoConfigRequirementDetail(item)}`
              : ""}
          </li>
        ))}
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

function blankToUndefined(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed ? trimmed : undefined;
}

function readAPIError(result: JSONResult<unknown>) {
  if (result.ok) {
    return null;
  }
  const payload = result.data as APIErrorShape;
  return payload.error || null;
}
