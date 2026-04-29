import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  APIRequestError,
  type APIErrorShape,
  type JSONResult,
  requestJSON,
  requestJSONAllowHTTPError,
  requestVoid,
  sendJSON,
} from "../lib/api";
import { relativeLocalPath } from "../lib/paths";
import type {
  AutostartDetectResponse,
  BootstrapState,
  FeishuManifestResponse,
  FeishuAppPermissionCheckResponse,
  FeishuAppResponse,
  FeishuAppSummary,
  FeishuAppTestStartResponse,
  FeishuAppVerifyResponse,
  FeishuAppsResponse,
  FeishuOnboardingCompleteResponse,
  FeishuOnboardingSession,
  FeishuOnboardingSessionResponse,
  RuntimeRequirementsDetectResponse,
  VSCodeDetectResponse,
} from "../lib/types";
import {
  loadAutostartState,
  loadVSCodeState,
  vscodeApplyModeForScenario,
  vscodeIsReady,
} from "./shared/helpers";

type SetupStepID =
  | "env"
  | "connect"
  | "permission"
  | "events"
  | "callback"
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

type PermissionState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "ready"; data: FeishuAppPermissionCheckResponse }
  | { status: "skipped"; data: FeishuAppPermissionCheckResponse | null }
  | { status: "missing"; data: FeishuAppPermissionCheckResponse }
  | { status: "error"; message: string };

type TestState = {
  status: "idle" | "sending" | "sent" | "error";
  message: string;
};

type RuntimeApplyFailureDetails = {
  gatewayId?: string;
  app?: FeishuAppSummary;
};

type RequirementTableRow = {
  key: string;
  cells: ReactNode[];
};

const setupSteps: Array<{ id: SetupStepID; name: string }> = [
  { id: "env", name: "环境检查" },
  { id: "connect", name: "飞书连接" },
  { id: "permission", name: "权限检查" },
  { id: "events", name: "事件订阅" },
  { id: "callback", name: "回调配置" },
  { id: "menu", name: "菜单确认" },
  { id: "autostart", name: "自动启动" },
  { id: "vscode", name: "VS Code 集成" },
  { id: "done", name: "完成" },
];

const defaultQRCodePollIntervalSeconds = 5;
const vscodeApplyTimeoutMs = 10_000;
const vscodeDetectRecoveryTimeoutMs = 5_000;

export function SetupRoute() {
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [bootstrap, setBootstrap] = useState<BootstrapState | null>(null);
  const [manifest, setManifest] = useState<FeishuManifestResponse["manifest"] | null>(
    null,
  );
  const [apps, setApps] = useState<FeishuAppSummary[]>([]);
  const [selectedAppID, setSelectedAppID] = useState("");
  const [runtimeRequirements, setRuntimeRequirements] =
    useState<RuntimeRequirementsDetectResponse | null>(null);
  const [autostart, setAutostart] = useState<AutostartDetectResponse | null>(
    null,
  );
  const [autostartError, setAutostartError] = useState("");
  const [vscode, setVSCode] = useState<VSCodeDetectResponse | null>(null);
  const [vscodeError, setVSCodeError] = useState("");
  const [currentStep, setCurrentStep] = useState<SetupStepID>("env");
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
  const [permissionState, setPermissionState] = useState<PermissionState>({
    status: "idle",
  });
  const [eventTest, setEventTest] = useState<TestState>({
    status: "idle",
    message: "",
  });
  const [callbackTest, setCallbackTest] = useState<TestState>({
    status: "idle",
    message: "",
  });
  const [eventsDone, setEventsDone] = useState(false);
  const [callbackDone, setCallbackDone] = useState(false);
  const [menuDone, setMenuDone] = useState(false);
  const [autostartDone, setAutostartDone] = useState(false);
  const [vscodeDone, setVSCodeDone] = useState(false);

  const activeApp = useMemo(
    () => apps.find((app) => app.id === selectedAppID) ?? null,
    [apps, selectedAppID],
  );
  const title = buildSetupPageTitle(bootstrap);
  const adminURL = relativeLocalPath(bootstrap?.admin.url || "/");
  const activeConsoleLinks = activeApp?.consoleLinks;
  const isReadOnlyApp = Boolean(activeApp?.readOnly);
  const currentStepIndex = setupSteps.findIndex((step) => step.id === currentStep);
  const setupComplete = vscodeDone || currentStep === "done";
  const autostartReady = Boolean(autostart);
  const vscodeReadyNow = vscodeIsReady(vscode);

  const stepDone: Record<SetupStepID, boolean> = {
    env: Boolean(runtimeRequirements?.ready),
    connect: hasConnectedApp(activeApp),
    permission:
      permissionState.status === "ready" || permissionState.status === "skipped",
    events: eventsDone,
    callback: callbackDone,
    menu: menuDone,
    autostart: autostartDone || Boolean(autostart?.enabled),
    vscode: vscodeDone || vscodeReadyNow,
    done: setupComplete,
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
    setPermissionState({ status: "idle" });
    setEventTest({ status: "idle", message: "" });
    setCallbackTest({ status: "idle", message: "" });
  }, [selectedAppID]);

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
    if (currentStep !== "permission") {
      return;
    }
    if (!activeApp?.id) {
      return;
    }
    if (permissionState.status === "idle") {
      void checkPermissions(activeApp.id);
      return;
    }
    if (permissionState.status !== "ready") {
      return;
    }
    const timer = window.setTimeout(() => {
      setNotice({ tone: "good", message: "权限检查通过，已进入事件订阅。" });
      setCurrentStep("events");
    }, 700);
    return () => window.clearTimeout(timer);
  }, [activeApp?.id, currentStep, permissionState]);

  useEffect(() => {
    if (currentStep === "events" && activeApp?.id && eventTest.status === "idle") {
      void startTest(activeApp.id, "events");
    }
  }, [activeApp?.id, currentStep, eventTest.status]);

  useEffect(() => {
    if (
      currentStep === "callback" &&
      activeApp?.id &&
      callbackTest.status === "idle"
    ) {
      void startTest(activeApp.id, "callback");
    }
  }, [activeApp?.id, callbackTest.status, currentStep]);

  useEffect(() => {
    if (currentStep !== "autostart" || !autostartReady) {
      return;
    }
    if (autostartError) {
      return;
    }
    if (autostart?.supported !== false) {
      return;
    }
    const timer = window.setTimeout(() => {
      setAutostartDone(true);
      setNotice({ tone: "warn", message: "当前系统不支持自动启动，已进入 VS Code 集成。" });
      setCurrentStep("vscode");
    }, 700);
    return () => window.clearTimeout(timer);
  }, [autostart, autostartError, autostartReady, currentStep]);

  async function loadSetupPage(options?: {
    preferredAppID?: string;
    preserveStep?: boolean;
    showEnvironmentAdvanceNotice?: boolean;
  }) {
    if (!options?.preserveStep) {
      setLoading(true);
    }
    setLoadError("");
    const [
      bootstrapState,
      manifestState,
      appList,
      runtimeState,
      autostartState,
      vscodeState,
    ] =
      await Promise.all([
        requestJSON<BootstrapState>("/api/setup/bootstrap-state"),
        requestJSON<FeishuManifestResponse>("/api/setup/feishu/manifest"),
        requestJSON<FeishuAppsResponse>("/api/setup/feishu/apps"),
        requestJSON<RuntimeRequirementsDetectResponse>(
          "/api/setup/runtime-requirements/detect",
        ),
        loadAutostartState("/api/setup/autostart/detect"),
        loadVSCodeState("/api/setup/vscode/detect"),
      ]);

    const nextSelectedAppID =
      appList.apps.find((app) => app.id === options?.preferredAppID)?.id ||
      appList.apps.find((app) => app.id === selectedAppID)?.id ||
      appList.apps[0]?.id ||
      "";

    setBootstrap(bootstrapState);
    setManifest(manifestState.manifest);
    setApps(appList.apps);
    setSelectedAppID(nextSelectedAppID);
    setRuntimeRequirements(runtimeState);
    setAutostart(autostartState.data);
    setAutostartError(autostartState.error);
    setVSCode(vscodeState.data);
    setVSCodeError(vscodeState.error);
    setLoading(false);

    if (!options?.preserveStep) {
      const selectedApp =
        appList.apps.find((app) => app.id === nextSelectedAppID) ?? null;
      const nextStep = deriveSetupEntryStep(runtimeState, selectedApp);
      setCurrentStep(nextStep);
      if (options?.showEnvironmentAdvanceNotice && nextStep === "connect") {
        setNotice({ tone: "good", message: "环境正常，已自动进入飞书连接。" });
      }
    }
  }

  async function retryEnvironmentCheck() {
    await loadSetupPage({
      preferredAppID: activeApp?.id,
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
      await loadSetupPage({
        preferredAppID: response.data.app.id,
        preserveStep: true,
      });
      setSelectedAppID(response.data.app.id);
      setNotice({ tone: "good", message: "连接验证成功。" });
      setConnectError("");
      setCurrentStep("permission");
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
      await loadSetupPage({ preferredAppID: appID, preserveStep: true });
      setSelectedAppID(appID);
      if (!verify.ok) {
        setNotice({
          tone: "danger",
          message: "连接验证没有通过，请检查 App ID 和 App Secret 后重试。",
        });
        return;
      }
      setNotice({ tone: "good", message: "连接验证成功。" });
      setCurrentStep("permission");
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
      preserveStep: true,
    });
    setNotice({
      tone: "warn",
      message:
        "配置已经保存，但当前运行中的机器人还没有同步完成。你可以稍后去管理页面继续处理。",
    });
    return true;
  }

  async function checkPermissions(appID: string) {
    setPermissionState({ status: "loading" });
    const response = await requestJSONAllowHTTPError<
      FeishuAppPermissionCheckResponse | APIErrorShape
    >(`/api/setup/feishu/apps/${encodeURIComponent(appID)}/permission-check`);
    if (!response.ok) {
      setPermissionState({
        status: "error",
        message: "暂时无法完成权限检查，请稍后重试。",
      });
      return;
    }
    const payload = response.data as FeishuAppPermissionCheckResponse;
    setPermissionState(payload.ready ? { status: "ready", data: payload } : { status: "missing", data: payload });
  }

  async function skipPermissions(appID: string) {
    const skippedData =
      permissionState.status === "missing" ? permissionState.data : null;
    setActionBusy("permission-skip");
    try {
      await requestVoid(
        `/api/setup/feishu/apps/${encodeURIComponent(appID)}/onboarding-permission/skip`,
        { method: "POST" },
      );
      setPermissionState({ status: "skipped", data: skippedData });
      setNotice({
        tone: "warn",
        message: "已跳过这一步，你可以继续后面的设置。",
      });
      setCurrentStep("events");
    } catch {
      setNotice({ tone: "danger", message: "当前还不能跳过这一步，请稍后重试。" });
    } finally {
      setActionBusy("");
    }
  }

  async function recheckPermissions(appID: string) {
    setActionBusy("permission-recheck");
    try {
      if (permissionState.status === "skipped") {
        await requestVoid(
          `/api/setup/feishu/apps/${encodeURIComponent(appID)}/onboarding-permission/reset`,
          { method: "POST" },
        );
      }
      await checkPermissions(appID);
    } catch {
      setPermissionState({
        status: "error",
        message: "暂时无法完成权限检查，请稍后重试。",
      });
    } finally {
      setActionBusy("");
    }
  }

  async function startTest(
    appID: string,
    kind: "events" | "callback",
  ) {
    const setState = kind === "events" ? setEventTest : setCallbackTest;
    setState({ status: "sending", message: "" });
    const response = await requestJSONAllowHTTPError<
      FeishuAppTestStartResponse | APIErrorShape
    >(`/api/setup/feishu/apps/${encodeURIComponent(appID)}/${kind === "events" ? "test-events" : "test-callback"}`, {
      method: "POST",
    });
    if (!response.ok) {
      const error = readAPIError(response);
      setState({
        status: "error",
        message:
          error?.code === "feishu_app_web_test_recipient_unavailable"
            ? String(
                error.details ||
                  "手动添加的机器人无法自动发送测试消息，请直接在飞书后台继续手动配置。",
              )
            : "暂时没有把测试提示发送成功，请稍后重试。",
      });
      return;
    }
    const payload = response.data as FeishuAppTestStartResponse;
    setState({ status: "sent", message: payload.message });
  }

  async function clearInstallTest(appID: string, kind: "events" | "callback") {
    await requestJSONAllowHTTPError<unknown>(
      `/api/setup/feishu/apps/${encodeURIComponent(appID)}/install-tests/${encodeURIComponent(kind)}/clear`,
      {
        method: "POST",
      },
    );
  }

  async function applyAutostartAndContinue() {
    setActionBusy("autostart");
    try {
      const response = await sendJSON<AutostartDetectResponse>(
        "/api/setup/autostart/apply",
        "POST",
      );
      setAutostart(response);
      setAutostartDone(true);
      setNotice({ tone: "good", message: "已启用自动启动。" });
      setCurrentStep("vscode");
    } catch {
      setNotice({ tone: "danger", message: "当前还不能启用自动启动，请稍后重试。" });
    } finally {
      setActionBusy("");
    }
  }

  async function applyVSCodeAndContinue() {
    if (!vscode) {
      setNotice({ tone: "danger", message: "暂时还不能完成 VS Code 集成，请稍后重试。" });
      return;
    }
    setActionBusy("vscode");
    try {
      const mode = vscodeApplyModeForScenario(vscode, "current_machine");
      const response = await sendJSON<VSCodeDetectResponse>(
        "/api/setup/vscode/apply",
        "POST",
        {
          mode: mode || "managed_shim",
          bundleEntrypoint: vscode.latestBundleEntrypoint,
        },
        { timeoutMs: vscodeApplyTimeoutMs },
      );
      setVSCode(response);
      setVSCodeError("");
      setVSCodeDone(true);
      setNotice({ tone: "good", message: "VS Code 集成已完成。" });
      setCurrentStep("done");
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
    const refreshed = await loadVSCodeState(
      "/api/setup/vscode/detect",
      vscodeDetectRecoveryTimeoutMs,
    );
    if (refreshed.data) {
      setVSCode(refreshed.data);
      setVSCodeError("");
      if (vscodeIsReady(refreshed.data)) {
        setVSCodeDone(true);
        setNotice({ tone: "good", message: "VS Code 集成已完成。" });
        setCurrentStep("done");
        return true;
      }
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

  function renderCurrentStep() {
    switch (currentStep) {
      case "env":
        return renderEnvironmentStep();
      case "connect":
        return renderConnectStep();
      case "permission":
        return renderPermissionStep();
      case "events":
        return renderEventsStep();
      case "callback":
        return renderCallbackStep();
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
    const failingChecks =
      runtimeRequirements?.checks.filter((check) => check.status !== "pass") || [];
    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>环境检查</h2>
          <p>进入后自动检查服务与运行条件。</p>
        </div>
        {runtimeRequirements?.ready ? (
          <div className="notice-banner good">
            环境检查通过，正在进入飞书连接...
          </div>
        ) : (
          <div className="notice-banner warn">当前服务还在检查中，请稍候。</div>
        )}
        {failingChecks.length > 0 ? (
          <div className="panel">
            <div className="section-heading">
              <div>
                <h4>当前需要处理</h4>
                <p>请先修复下面的问题，再重新检查。</p>
              </div>
            </div>
            <ul className="ordered-checklist">
              {failingChecks.map((check) => (
                <li key={check.id}>
                  <strong>{check.title}</strong>
                  <span>{check.summary}</span>
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
        ) : null}
      </section>
    );
  }

  function renderConnectStep() {
    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>飞书连接</h2>
          <p>选择扫码创建或手动输入，连接验证通过后会自动进入下一步。</p>
        </div>
        <div className="choice-toggle">
          <button
            className={connectMode === "qr" ? "primary-button" : "ghost-button"}
            type="button"
            onClick={() => changeConnectMode("qr")}
          >
            扫码创建
          </button>
          <button
            className={
              connectMode === "manual" ? "primary-button" : "ghost-button"
            }
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
              请使用飞书扫码完成创建，页面会自动轮询并继续下一步。
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
                扫码成功，连接验证已通过，正在进入权限检查...
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

  function renderPermissionStep() {
    if (permissionState.status === "loading" || permissionState.status === "idle") {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>权限检查</h2>
            <p>进入页面后自动检查当前权限。</p>
          </div>
          <div className="notice-banner warn">正在检查权限，请稍候...</div>
        </section>
      );
    }

    if (permissionState.status === "ready") {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>权限检查</h2>
            <p>权限已完整，系统会自动进入下一步。</p>
          </div>
          <div className="notice-banner good">检查通过，正在进入事件订阅...</div>
        </section>
      );
    }

    if (permissionState.status === "skipped") {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>权限检查</h2>
            <p>你已选择先跳过这一步，后续仍可回到这里重新检查。</p>
          </div>
          <div className="notice-banner warn">当前按已跳过处理，你可以继续后面的设置。</div>
          {(permissionState.data?.missingScopes || []).length > 0 ? (
            <div className="scope-list">
              {(permissionState.data?.missingScopes || []).map((scope) => (
                <span
                  key={`${scope.scopeType || "tenant"}-${scope.scope}`}
                  className="scope-pill"
                >
                  <code>{scope.scope}</code>
                </span>
              ))}
            </div>
          ) : null}
          <div className="panel">
            <div className="section-heading">
              <div>
                <h4>可复制的一次性权限配置</h4>
                <p>需要时随时回到这里补齐后再重新检查。</p>
              </div>
            </div>
            <textarea
              readOnly
              className="code-textarea"
              value={permissionState.data?.grantJSON || ""}
            />
            <div className="button-row">
              {permissionState.data?.grantJSON ? (
                <button
                  className="ghost-button"
                  type="button"
                  onClick={() => void copyGrantJSON(permissionState.data?.grantJSON || "")}
                >
                  复制配置
                </button>
              ) : null}
              <a
                className="ghost-button"
                href={permissionState.data?.app.consoleLinks?.auth || activeConsoleLinks?.auth || "#"}
                rel="noreferrer"
                target="_blank"
              >
                打开飞书后台权限配置
              </a>
              <button
                className="secondary-button"
                type="button"
                disabled={actionBusy === "permission-recheck"}
                onClick={() => activeApp?.id && void recheckPermissions(activeApp.id)}
              >
                重新检查
              </button>
              <button
                className="primary-button"
                type="button"
                onClick={() => setCurrentStep("events")}
              >
                继续后面的设置
              </button>
            </div>
          </div>
        </section>
      );
    }

    if (permissionState.status === "error") {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>权限检查</h2>
            <p>暂时没有拿到最新结果，请重新检查。</p>
          </div>
          <div className="notice-banner danger">{permissionState.message}</div>
          <div className="button-row">
            <button
              className="secondary-button"
              type="button"
              disabled={actionBusy === "permission-recheck"}
              onClick={() => activeApp?.id && void recheckPermissions(activeApp.id)}
            >
              重新检查
            </button>
          </div>
        </section>
      );
    }

    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>权限检查</h2>
          <p>检测到缺失权限，请先在飞书后台补齐。</p>
        </div>
        <div className="notice-banner danger">
          当前还不能进入下一步，请先补齐缺失权限。
        </div>
        <div className="scope-list">
          {(permissionState.data.missingScopes || []).map((scope) => (
            <span key={`${scope.scopeType || "tenant"}-${scope.scope}`} className="scope-pill">
              <code>{scope.scope}</code>
            </span>
          ))}
        </div>
        <div className="panel">
          <div className="section-heading">
            <div>
              <h4>可复制的一次性权限配置</h4>
              <p>补齐后重新检查即可继续。</p>
            </div>
          </div>
          <textarea
            readOnly
            className="code-textarea"
            value={permissionState.data.grantJSON || ""}
          />
          <div className="button-row">
            <button
              className="ghost-button"
              type="button"
              onClick={() =>
                void copyGrantJSON(permissionState.data.grantJSON || "")
              }
            >
              复制配置
            </button>
            <a
              className="ghost-button"
              href={permissionState.data.app.consoleLinks?.auth || activeConsoleLinks?.auth || "#"}
              rel="noreferrer"
              target="_blank"
            >
              打开飞书后台权限配置
            </a>
            <button
              className="primary-button"
              type="button"
              disabled={actionBusy === "permission-recheck"}
              onClick={() => activeApp?.id && void recheckPermissions(activeApp.id)}
            >
              我已处理，重新检查
            </button>
            <button
              className="ghost-button"
              type="button"
              disabled={actionBusy === "permission-skip"}
              onClick={() => activeApp?.id && void skipPermissions(activeApp.id)}
            >
              强制跳过这一步
            </button>
          </div>
        </div>
      </section>
    );
  }

  function renderEventsStep() {
    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>事件订阅</h2>
          <p>进入本页后，机器人会自动发出事件订阅测试提示。</p>
        </div>
        {eventTest.status === "sent" ? (
          <div className="notice-banner good">
            {eventTest.message || "事件订阅测试提示已发送。"}
          </div>
        ) : null}
        {eventTest.status === "error" ? (
          <div className="notice-banner danger">{eventTest.message}</div>
        ) : null}
        <p className="support-copy">
          前往
          {" "}
          <a
            className="inline-link"
            href={activeConsoleLinks?.events || "#"}
            rel="noreferrer"
            target="_blank"
          >
            飞书后台
          </a>
          {" "}
          配置事件订阅。
        </p>
        {renderRequirementTable(
          ["事件", "用途"],
          (manifest?.events || []).map((item) => ({
            key: item.event,
            cells: [
              renderCopyableRequirement(item.event, "事件名"),
              item.purpose || "",
            ],
          })),
        )}
        <div className="button-row">
          <button
            className="primary-button"
            type="button"
            onClick={() => {
              if (activeApp?.id) {
                void clearInstallTest(activeApp.id, "events");
              }
              setEventsDone(true);
              setCurrentStep("callback");
            }}
          >
            下一步
          </button>
        </div>
      </section>
    );
  }

  function renderCallbackStep() {
    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>回调配置</h2>
          <p>进入本页后，机器人会自动发出回调测试卡片。</p>
        </div>
        {callbackTest.status === "sent" ? (
          <div className="notice-banner good">
            {callbackTest.message || "回调测试卡片已发送。"}
          </div>
        ) : null}
        {callbackTest.status === "error" ? (
          <div className="notice-banner danger">{callbackTest.message}</div>
        ) : null}
        <p className="support-copy">
          前往
          {" "}
          <a
            className="inline-link"
            href={activeConsoleLinks?.callback || "#"}
            rel="noreferrer"
            target="_blank"
          >
            飞书后台
          </a>
          {" "}
          配置回调。
        </p>
        {renderRequirementTable(
          ["回调", "用途"],
          (manifest?.callbacks || []).map((item) => ({
            key: item.callback,
            cells: [
              renderCopyableRequirement(item.callback, "回调名"),
              item.purpose || "",
            ],
          })),
        )}
        <div className="button-row">
          <button
            className="primary-button"
            type="button"
            onClick={() => {
              if (activeApp?.id) {
                void clearInstallTest(activeApp.id, "callback");
              }
              setCallbackDone(true);
              setCurrentStep("menu");
            }}
          >
            下一步
          </button>
        </div>
      </section>
    );
  }

  function renderMenuStep() {
    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>菜单确认</h2>
          <p>请在飞书后台完成菜单配置后继续下一步。</p>
        </div>
        <p className="support-copy">
          前往
          {" "}
          <a
            className="inline-link"
            href={activeConsoleLinks?.bot || "#"}
            rel="noreferrer"
            target="_blank"
          >
            飞书后台
          </a>
          {" "}
          完成菜单配置。
        </p>
        <div className="button-row">
          <button
            className="primary-button"
            type="button"
            onClick={() => {
              setMenuDone(true);
              setCurrentStep("autostart");
            }}
          >
            下一步
          </button>
        </div>
      </section>
    );
  }

  function renderAutostartStep() {
    if (!autostartReady) {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>自动启动</h2>
            <p>正在检查当前系统是否支持自动启动。</p>
          </div>
          <div className="notice-banner warn">检测中，请稍候...</div>
        </section>
      );
    }

    if (autostartError) {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>自动启动</h2>
            <p>暂时没有确认当前系统的自动启动状态。</p>
          </div>
          <div className="notice-banner warn">
            当前还不能判断自动启动状态。你可以稍后在管理页面继续处理。
          </div>
          <div className="button-row">
            <button
              className="ghost-button"
              type="button"
              onClick={() => {
                setAutostartDone(true);
                setCurrentStep("vscode");
              }}
            >
              先跳过
            </button>
          </div>
        </section>
      );
    }

    if (autostart?.supported === false) {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>自动启动</h2>
            <p>当前系统不支持自动启动，已自动跳过。</p>
          </div>
          <div className="notice-banner warn">
            当前系统不支持自动启动，正在进入 VS Code 集成...
          </div>
        </section>
      );
    }

    if (autostart?.enabled) {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>自动启动</h2>
            <p>当前已经启用自动启动。</p>
          </div>
          <div className="notice-banner good">当前已启用。</div>
          <div className="button-row">
            <button
              className="primary-button"
              type="button"
              onClick={() => {
                setAutostartDone(true);
                setCurrentStep("vscode");
              }}
            >
              下一步
            </button>
          </div>
        </section>
      );
    }

    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>自动启动</h2>
          <p>请选择是否在登录后自动运行。</p>
        </div>
        <div className="notice-banner warn">当前默认未启用。</div>
        <div className="button-row">
          <button
            className="primary-button"
            type="button"
            disabled={actionBusy === "autostart"}
            onClick={() => void applyAutostartAndContinue()}
          >
            启用自动启动
          </button>
          <button
            className="ghost-button"
            type="button"
            onClick={() => {
              setAutostartDone(true);
              setNotice({ tone: "good", message: "自动启动保持关闭。" });
              setCurrentStep("vscode");
            }}
          >
            保持关闭并继续
          </button>
        </div>
      </section>
    );
  }

  function renderVSCodeStep() {
    if (vscodeError) {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>VS Code 集成</h2>
            <p>是否在这台机器上使用 VS Code + Codex。</p>
          </div>
          <div className="notice-banner warn">
            当前还不能确认 VS Code 集成状态。你可以稍后在管理页面继续处理。
          </div>
          <div className="button-row">
            <button
              className="ghost-button"
              type="button"
              onClick={() => {
                setVSCodeDone(true);
                setCurrentStep("done");
              }}
            >
              先不使用
            </button>
          </div>
        </section>
      );
    }

    if (vscodeReadyNow) {
      return (
        <section className="step-section">
          <div className="step-stage-head">
            <h2>VS Code 集成</h2>
            <p>当前已经完成 VS Code 集成。</p>
          </div>
          <div className="notice-banner good">当前已接入。</div>
          <div className="button-row">
            <button
              className="primary-button"
              type="button"
              onClick={() => {
                setVSCodeDone(true);
                setCurrentStep("done");
              }}
            >
              下一步
            </button>
          </div>
        </section>
      );
    }

    return (
      <section className="step-section">
        <div className="step-stage-head">
          <h2>VS Code 集成</h2>
          <p>是否在这台机器上使用 VS Code + Codex。</p>
        </div>
        <div className="notice-banner warn">
          如果你需要在这台机器上使用 VS Code，请完成集成。
        </div>
        <div className="button-row">
          <button
            className="primary-button"
            type="button"
            disabled={actionBusy === "vscode"}
            onClick={() => void applyVSCodeAndContinue()}
          >
            确认集成
          </button>
          <button
            className="ghost-button"
            type="button"
            onClick={() => {
              setVSCodeDone(true);
              setCurrentStep("done");
            }}
          >
            先不使用
          </button>
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
          <p>你现在可以进入管理页面，继续维护机器人、系统集成和存储清理。</p>
        </div>
        <div className="button-row">
          <a className="primary-button" href={adminURL}>
            去管理页面
          </a>
        </div>
      </section>
    );
  }

  async function copyGrantJSON(value: string) {
    if (!value.trim()) {
      return;
    }
    try {
      await navigator.clipboard.writeText(value);
      setNotice({ tone: "good", message: "已复制权限配置。" });
    } catch {
      setNotice({ tone: "warn", message: "复制没有成功，请手动复制。" });
    }
  }

  async function copyRequirementValue(value: string, label: string) {
    if (!value.trim()) {
      return;
    }
    try {
      await navigator.clipboard.writeText(value);
      setNotice({ tone: "good", message: `已复制${label}。` });
    } catch {
      setNotice({ tone: "warn", message: `${label}复制没有成功，请手动复制。` });
    }
  }

  function renderCopyableRequirement(value: string, label: string) {
    return (
      <div className="requirement-copy-cell">
        <code>{value}</code>
        <button
          className="table-copy-button"
          type="button"
          aria-label={`复制${label} ${value}`}
          onClick={() => void copyRequirementValue(value, label)}
        >
          复制
        </button>
      </div>
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
            <p>可随时回看已完成步骤。</p>
          </div>
          <div className="step-list">
            {setupSteps.map((step, index) => {
              const disabled = !(index <= currentStepIndex || stepDone[step.id]);
              return (
                <button
                  key={step.id}
                  className={`step-item${step.id === currentStep ? " active" : ""}${stepDone[step.id] ? " done" : ""}`}
                  disabled={disabled}
                  type="button"
                  onClick={() => setCurrentStep(step.id)}
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
    </div>
  );
}

function deriveSetupEntryStep(
  runtimeRequirements: RuntimeRequirementsDetectResponse | null,
  app: FeishuAppSummary | null,
): SetupStepID {
  if (!runtimeRequirements?.ready) {
    return "env";
  }
  if (!hasConnectedApp(app)) {
    return "connect";
  }
  return "permission";
}

function hasConnectedApp(app: FeishuAppSummary | null): boolean {
  return Boolean(app?.verifiedAt);
}

function buildSetupPageTitle(bootstrap: BootstrapState | null): string {
  const name = bootstrap?.product.name?.trim() || "Codex Remote Feishu";
  const version = bootstrap?.product.version?.trim();
  return version ? `${name} ${version} 安装程序` : `${name} 安装程序`;
}

function renderRequirementTable(headers: string[], rows: RequirementTableRow[]) {
  return (
    <div className="detail-table-wrap">
      <table className="detail-table">
        <thead>
          <tr>
            {headers.map((header) => (
              <th key={header} scope="col">
                {header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, rowIndex) => (
            <tr key={row.key || `${rowIndex}-row`}>
              {row.cells.map((value, cellIndex) => (
                <td key={`${rowIndex}-${cellIndex}`}>{value}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
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
