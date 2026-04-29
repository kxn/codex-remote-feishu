import { useEffect, useState } from "react";
import {
  APIRequestError,
  requestJSON,
  requestJSONAllowHTTPError,
  requestVoid,
  sendJSON,
} from "../../../lib/api";
import { relativeLocalPath } from "../../../lib/paths";
import type {
  FeishuAppResponse,
  FeishuAppTestStartResponse,
  FeishuAppVerifyResponse,
  FeishuManifestResponse,
  FeishuOnboardingCompleteResponse,
  FeishuOnboardingSession,
  FeishuOnboardingSessionResponse,
  OnboardingWorkflowResponse,
  SetupCompleteResponse,
} from "../../../lib/types";
import {
  blankToUndefined,
  vscodeApplyModeForScenario,
  vscodeIsReady,
} from "../helpers";
import type {
  ManualConnectForm,
  Notice,
  OnboardingFlowController,
  OnboardingFlowSurfaceProps,
  RuntimeApplyFailureDetails,
  TestState,
} from "./types";
import {
  buildVerifySuccessMessage,
  buildWorkflowPath,
  defaultQRCodePollIntervalSeconds,
  readAPIError,
  stageAllowsAction,
  stepTitle,
  syntheticConnectionStage,
  vscodeApplyTimeoutMs,
  vscodeDetectRecoveryTimeoutMs,
  workflowStageByID,
} from "./utils";

export function useOnboardingFlowController({
  mode,
  preferredAppID = "",
  connectOnly = false,
  autoStartTests = mode === "setup",
  fallbackAdminURL = "/admin/",
  connectOnlyTitle = "新增机器人",
  connectOnlyDescription = "选择扫码创建或手动输入，连接验证通过后会自动加入机器人列表。",
  onConnectedApp,
  onContextRefresh,
}: OnboardingFlowSurfaceProps): OnboardingFlowController {
  const apiBasePath = mode === "setup" ? "/api/setup" : "/api/admin";
  const [loading, setLoading] = useState(!connectOnly);
  const [loadError, setLoadError] = useState("");
  const [manifest, setManifest] = useState<FeishuManifestResponse["manifest"] | null>(null);
  const [workflow, setWorkflow] = useState<OnboardingWorkflowResponse | null>(null);
  const [visibleStageID, setVisibleStageID] = useState("");
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
  const [eventTest, setEventTest] = useState<TestState>({
    status: "idle",
    message: "",
  });
  const [callbackTest, setCallbackTest] = useState<TestState>({
    status: "idle",
    message: "",
  });

  const currentStageID = connectOnly
    ? "connect"
    : workflow?.currentStage || workflow?.stages[0]?.id || "runtime_requirements";
  const stageID = connectOnly ? "connect" : visibleStageID || currentStageID;
  const currentStage = connectOnly
    ? syntheticConnectionStage()
    : workflowStageByID(workflow, currentStageID);
  const activeApp = workflow?.app?.app ?? null;
  const activeConsoleLinks = activeApp?.consoleLinks;
  const isReadOnlyApp = Boolean(activeApp?.readOnly);
  const connectionStage = connectOnly
    ? syntheticConnectionStage()
    : workflow?.app?.connection || workflowStageByID(workflow, "connect");
  const permissionStage = workflow?.app?.permission || null;
  const eventsStage = workflow?.app?.events || null;
  const callbackStage = workflow?.app?.callback || null;
  const menuStage = workflow?.app?.menu || null;

  useEffect(() => {
    let cancelled = false;
    if (connectOnly) {
      setLoading(false);
      return;
    }
    void loadWorkflowSurface({ preferredAppID, focusCurrentStage: true }).catch(() => {
      if (!cancelled) {
        setLoadError("当前页面暂时无法读取状态，请刷新后重试。");
        setLoading(false);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [connectOnly, preferredAppID]);

  useEffect(() => {
    if (!activeApp) {
      return;
    }
    setManualForm((current) => ({
      name: current.name || activeApp.name || "",
      appId: current.appId || activeApp.appId || "",
      appSecret: current.appSecret,
    }));
  }, [activeApp?.id, activeApp?.name, activeApp?.appId]);

  useEffect(() => {
    setEventTest({ status: "idle", message: "" });
    setCallbackTest({ status: "idle", message: "" });
  }, [activeApp?.id]);

  useEffect(() => {
    if (connectOnly || !workflow) {
      return;
    }
    if (visibleStageID && workflow.stages.some((stage) => stage.id === visibleStageID)) {
      return;
    }
    setVisibleStageID(workflow.currentStage || workflow.stages[0]?.id || "");
  }, [connectOnly, visibleStageID, workflow]);

  useEffect(() => {
    if (mode !== "setup") {
      return;
    }
    if (typeof window.scrollTo === "function") {
      window.scrollTo({ top: 0, behavior: "auto" });
    }
  }, [mode, stageID]);

  useEffect(() => {
    if (connectOnly || currentStageID === "connect") {
      return;
    }
    setOnboardingSession(null);
    setConnectError("");
  }, [connectOnly, currentStageID]);

  useEffect(() => {
    const canStart = connectOnly ? true : stageAllowsAction(connectionStage, "start_qr");
    const connectStageVisible = connectOnly || currentStageID === "connect";
    if (!connectStageVisible || connectMode !== "qr" || !canStart) {
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
  }, [
    actionBusy,
    connectError,
    connectMode,
    connectOnly,
    connectionStage,
    currentStageID,
    onboardingSession,
  ]);

  useEffect(() => {
    if (
      !autoStartTests ||
      connectOnly ||
      currentStageID !== "events" ||
      !activeApp?.id ||
      !stageAllowsAction(eventsStage, "start_test") ||
      eventTest.status !== "idle"
    ) {
      return;
    }
    void startTest(activeApp.id, "events");
  }, [activeApp?.id, autoStartTests, connectOnly, currentStageID, eventTest.status, eventsStage]);

  useEffect(() => {
    if (
      !autoStartTests ||
      connectOnly ||
      currentStageID !== "callback" ||
      !activeApp?.id ||
      !stageAllowsAction(callbackStage, "start_test") ||
      callbackTest.status !== "idle"
    ) {
      return;
    }
    void startTest(activeApp.id, "callback");
  }, [
    activeApp?.id,
    autoStartTests,
    callbackStage,
    callbackTest.status,
    connectOnly,
    currentStageID,
  ]);

  async function loadWorkflowSurface(options?: {
    preferredAppID?: string;
    soft?: boolean;
    focusCurrentStage?: boolean;
  }) {
    if (connectOnly) {
      setLoading(false);
      return null;
    }
    if (!options?.soft) {
      setLoading(true);
    }
    setLoadError("");
    const appID =
      options?.preferredAppID ||
      preferredAppID ||
      workflow?.selectedAppId ||
      activeApp?.id ||
      "";
    const [manifestState, workflowState] = await Promise.all([
      requestJSON<FeishuManifestResponse>(`${apiBasePath}/feishu/manifest`),
      requestJSON<OnboardingWorkflowResponse>(buildWorkflowPath(apiBasePath, appID)),
    ]);

    setManifest(manifestState.manifest);
    setWorkflow(workflowState);
    if (options?.focusCurrentStage) {
      setVisibleStageID(workflowState.currentStage || workflowState.stages[0]?.id || "");
    }
    setLoading(false);
    return workflowState;
  }

  async function retryEnvironmentCheck() {
    await loadWorkflowSurface({
      preferredAppID: activeApp?.id,
      soft: true,
      focusCurrentStage: true,
    });
  }

  function changeConnectMode(nextMode: "qr" | "manual") {
    setConnectMode(nextMode);
    setConnectError("");
    setOnboardingSession(null);
  }

  function resetQRCodeSession() {
    setOnboardingSession(null);
    setConnectError("");
  }

  function retryQRCodeVerification() {
    if (!onboardingSession?.id) {
      return;
    }
    setConnectError("");
    void completeQRCodeSession(onboardingSession.id);
  }

  async function startQRCodeSession() {
    setActionBusy("qr-start");
    setConnectError("");
    try {
      const response = await sendJSON<FeishuOnboardingSessionResponse>(
        `${apiBasePath}/feishu/onboarding/sessions`,
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
        `${apiBasePath}/feishu/onboarding/sessions/${encodeURIComponent(sessionID)}`,
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
        `${apiBasePath}/feishu/onboarding/sessions/${encodeURIComponent(sessionID)}/complete`,
        { method: "POST" },
      );
      setOnboardingSession(response.data.session);
      if (!response.ok) {
        setConnectError("扫码已经完成，但连接验证没有通过，请重新验证。");
        return;
      }
      if (connectOnly) {
        await onConnectedApp?.(response.data.app.id);
        return;
      }
      await loadWorkflowSurface({
        preferredAppID: response.data.app.id,
        soft: true,
        focusCurrentStage: true,
      });
      await onContextRefresh?.(response.data.app.id);
      setNotice({
        tone: "good",
        message: buildVerifySuccessMessage(
          mode,
          response.data.app,
          response.data.mutation,
          response.data.result?.duration,
        ),
      });
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
              `${apiBasePath}/feishu/apps/${encodeURIComponent(activeApp.id)}`,
              "PUT",
              payload,
            )
          : await sendJSON<FeishuAppResponse>(`${apiBasePath}/feishu/apps`, "POST", payload);
        appID = saved.app.id;
      }
      const verify = await requestJSONAllowHTTPError<FeishuAppVerifyResponse>(
        `${apiBasePath}/feishu/apps/${encodeURIComponent(appID)}/verify`,
        { method: "POST" },
      );
      if (connectOnly) {
        if (!verify.ok) {
          setNotice({
            tone: "danger",
            message: "连接验证没有通过，请检查 App ID 和 App Secret 后重试。",
          });
          return;
        }
        await onConnectedApp?.(appID);
        return;
      }
      await loadWorkflowSurface({
        preferredAppID: appID,
        soft: true,
        focusCurrentStage: true,
      });
      await onContextRefresh?.(appID);
      if (!verify.ok) {
        setNotice({
          tone: "danger",
          message: "连接验证没有通过，请检查 App ID 和 App Secret 后重试。",
        });
        return;
      }
      setNotice({
        tone: "good",
        message: buildVerifySuccessMessage(
          mode,
          verify.data.app,
          undefined,
          verify.data.result?.duration,
        ),
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
    if (!(error instanceof APIRequestError) || error.code !== "gateway_apply_failed") {
      return false;
    }
    const details = error.details as RuntimeApplyFailureDetails | undefined;
    const nextAppID = details?.app?.id || details?.gatewayId || fallbackAppID || "";
    if (connectOnly) {
      if (nextAppID) {
        await onConnectedApp?.(nextAppID);
      }
    } else {
      await loadWorkflowSurface({
        preferredAppID: nextAppID,
        soft: true,
        focusCurrentStage: true,
      });
      await onContextRefresh?.(nextAppID);
    }
    setNotice({
      tone: "warn",
      message:
        mode === "setup"
          ? "配置已经保存，但当前运行中的机器人还没有同步完成。你可以稍后去管理页面继续处理。"
          : "配置已经保存，但当前运行中的机器人还没有同步完成。你可以稍后刷新管理页继续处理。",
    });
    return true;
  }

  async function refreshWorkflowFocus() {
    if (connectOnly) {
      return;
    }
    await loadWorkflowSurface({
      preferredAppID: activeApp?.id,
      soft: true,
      focusCurrentStage: true,
    });
  }

  async function recheckPermissionStage() {
    if (!activeApp?.id) {
      await refreshWorkflowFocus();
      return;
    }
    setActionBusy("permission-recheck");
    try {
      if (permissionStage?.status === "deferred") {
        await requestVoid(
          `${apiBasePath}/feishu/apps/${encodeURIComponent(activeApp.id)}/onboarding-permission/reset`,
          { method: "POST" },
        );
      }
      await refreshWorkflowFocus();
    } catch {
      setNotice({ tone: "danger", message: "当前还不能重新检查权限，请稍后重试。" });
    } finally {
      setActionBusy("");
    }
  }

  async function skipPermissionStage() {
    if (!activeApp?.id) {
      return;
    }
    setActionBusy("permission-force-skip");
    try {
      await requestVoid(
        `${apiBasePath}/feishu/apps/${encodeURIComponent(activeApp.id)}/onboarding-permission/skip`,
        { method: "POST" },
      );
      await refreshWorkflowFocus();
      setNotice({
        tone: "warn",
        message:
          mode === "setup"
            ? "已跳过这一步，你可以继续后面的设置。"
            : "已跳过这一步，后续仍可回到这里重新检查权限。",
      });
    } catch {
      setNotice({ tone: "danger", message: "当前还不能跳过这一步，请稍后重试。" });
    } finally {
      setActionBusy("");
    }
  }

  async function startTest(appID: string, kind: "events" | "callback") {
    const setState = kind === "events" ? setEventTest : setCallbackTest;
    setState({ status: "sending", message: "" });
    const response = await requestJSONAllowHTTPError<FeishuAppTestStartResponse>(
      `${apiBasePath}/feishu/apps/${encodeURIComponent(appID)}/${kind === "events" ? "test-events" : "test-callback"}`,
      {
        method: "POST",
      },
    );
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
    setState({
      status: "sent",
      message: response.data.message,
    });
  }

  async function clearInstallTest(appID: string, kind: "events" | "callback") {
    await requestJSONAllowHTTPError<unknown>(
      `${apiBasePath}/feishu/apps/${encodeURIComponent(appID)}/install-tests/${encodeURIComponent(kind)}/clear`,
      {
        method: "POST",
      },
    );
  }

  async function confirmAppStep(step: "events" | "callback" | "menu") {
    if (!activeApp?.id) {
      return;
    }
    setActionBusy(`confirm-${step}`);
    try {
      await requestVoid(
        `${apiBasePath}/feishu/apps/${encodeURIComponent(activeApp.id)}/onboarding-steps/${encodeURIComponent(step)}/complete`,
        {
          method: "POST",
        },
      );
      if (step === "events" || step === "callback") {
        await clearInstallTest(activeApp.id, step);
      }
      if (step === "events") {
        setEventTest({ status: "idle", message: "" });
      }
      if (step === "callback") {
        setCallbackTest({ status: "idle", message: "" });
      }
      await refreshWorkflowFocus();
      setNotice({ tone: "good", message: `${stepTitle(step)}已记录完成。` });
    } catch {
      setNotice({ tone: "danger", message: `当前还不能记录${stepTitle(step)}完成，请稍后重试。` });
    } finally {
      setActionBusy("");
    }
  }

  async function recordMachineDecision(
    kind: "autostart" | "vscode",
    decision: string,
    message: string,
  ) {
    setActionBusy(`${kind}-${decision}`);
    try {
      await requestVoid(`${apiBasePath}/onboarding/machine-decisions/${kind}`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ decision }),
      });
      await refreshWorkflowFocus();
      setNotice({ tone: "good", message });
    } catch {
      setNotice({ tone: "danger", message: "当前还不能保存你的选择，请稍后重试。" });
    } finally {
      setActionBusy("");
    }
  }

  async function applyAutostart() {
    setActionBusy("autostart-apply");
    try {
      await sendJSON(`${apiBasePath}/autostart/apply`, "POST");
      await refreshWorkflowFocus();
      setNotice({
        tone: "good",
        message: mode === "setup" ? "已启用自动启动。" : "已启用自动运行。",
      });
    } catch {
      setNotice({ tone: "danger", message: "当前还不能启用自动启动，请稍后重试。" });
    } finally {
      setActionBusy("");
    }
  }

  async function applyVSCode() {
    const vscode = workflow?.vscode.vscode || null;
    if (!vscode) {
      setNotice({ tone: "danger", message: "暂时还不能完成 VS Code 集成，请稍后重试。" });
      return;
    }
    setActionBusy("vscode-apply");
    try {
      const modeValue = vscodeApplyModeForScenario(vscode, "current_machine");
      await sendJSON(
        `${apiBasePath}/vscode/apply`,
        "POST",
        {
          mode: modeValue || "managed_shim",
          bundleEntrypoint: vscode.latestBundleEntrypoint,
        },
        { timeoutMs: vscodeApplyTimeoutMs },
      );
      await refreshWorkflowFocus();
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
        buildWorkflowPath(apiBasePath, activeApp?.id || preferredAppID),
        undefined,
        { timeoutMs: vscodeDetectRecoveryTimeoutMs },
      );
      setWorkflow(refreshed);
      const ready =
        refreshed.vscode.status === "complete" ||
        vscodeIsReady(refreshed.vscode.vscode || null);
      if (ready) {
        setNotice({ tone: "good", message: "VS Code 集成已完成。" });
        return true;
      }
    } catch {
      // ignore refresh failure and continue with timeout handling below
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

  async function completeSetup() {
    setActionBusy("complete-setup");
    try {
      const response = await requestJSONAllowHTTPError<SetupCompleteResponse>(
        `${apiBasePath}/complete`,
        { method: "POST" },
      );
      if (!response.ok) {
        const error = readAPIError(response);
        setNotice({
          tone: "danger",
          message:
            typeof error?.details === "string" && error.details.trim()
              ? String(error.details)
              : "当前 setup 还不能完成，请先处理阻塞项。",
        });
        await refreshWorkflowFocus();
        return;
      }
      const nextURL = relativeLocalPath(response.data.adminURL || fallbackAdminURL || "/");
      window.location.assign(nextURL);
    } catch {
      setNotice({ tone: "danger", message: "当前还不能完成 setup，请稍后重试。" });
    } finally {
      setActionBusy("");
    }
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

  async function retryLoad() {
    if (connectOnly) {
      setLoadError("");
      return;
    }
    await loadWorkflowSurface({ preferredAppID, focusCurrentStage: true });
  }

  return {
    mode,
    connectOnly,
    fallbackAdminURL,
    connectOnlyTitle,
    connectOnlyDescription,
    loading,
    loadError,
    notice,
    manifest,
    workflow,
    stageID,
    currentStageID,
    currentStage,
    activeApp,
    activeConsoleLinks,
    isReadOnlyApp,
    connectionStage,
    permissionStage,
    eventsStage,
    callbackStage,
    menuStage,
    actionBusy,
    onboardingSession,
    connectError,
    connectMode,
    manualForm,
    eventTest,
    callbackTest,
    setVisibleStageID,
    setManualForm,
    retryLoad,
    retryEnvironmentCheck,
    changeConnectMode,
    resetQRCodeSession,
    retryQRCodeVerification,
    submitManualConnect,
    refreshWorkflowFocus,
    recheckPermissionStage,
    skipPermissionStage,
    startTest,
    confirmAppStep,
    recordMachineDecision,
    applyAutostart,
    applyVSCode,
    completeSetup,
    copyGrantJSON,
    copyRequirementValue,
  };
}
