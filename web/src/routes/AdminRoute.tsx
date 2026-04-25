import { useEffect, useMemo, useState } from "react";
import {
  APIRequestError,
  type APIErrorShape,
  requestJSON,
  requestJSONAllowHTTPError,
  sendJSON,
} from "../lib/api";
import type {
  AutostartDetectResponse,
  BootstrapState,
  FeishuAppPermissionCheckResponse,
  FeishuAppResponse,
  FeishuAppSummary,
  FeishuAppTestStartResponse,
  FeishuAppVerifyResponse,
  FeishuAppsResponse,
  FeishuOnboardingCompleteResponse,
  FeishuOnboardingSession,
  FeishuOnboardingSessionResponse,
  ImageStagingCleanupResponse,
  ImageStagingStatusResponse,
  LogsStorageCleanupResponse,
  LogsStorageStatusResponse,
  PreviewDriveCleanupResponse,
  PreviewDriveStatusResponse,
  VSCodeDetectResponse,
} from "../lib/types";
import {
  loadAutostartState,
  loadVSCodeState,
  vscodeApplyModeForScenario,
  vscodeIsReady,
} from "./shared/helpers";

type NoticeTone = "good" | "warn" | "danger";

type DetailNotice = {
  tone: NoticeTone;
  message: string;
};

type PermissionSummaryState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "ready"; data: FeishuAppPermissionCheckResponse }
  | { status: "missing"; data: FeishuAppPermissionCheckResponse }
  | { status: "error"; message: string };

type RuntimeApplyFailureDetails = {
  gatewayId?: string;
  app?: FeishuAppSummary;
};

type NewRobotForm = {
  name: string;
  appId: string;
  appSecret: string;
};

const newRobotID = "new";

export function AdminRoute() {
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [bootstrap, setBootstrap] = useState<BootstrapState | null>(null);
  const [apps, setApps] = useState<FeishuAppSummary[]>([]);
  const [selectedRobotID, setSelectedRobotID] = useState(newRobotID);
  const [permissionChecks, setPermissionChecks] = useState<
    Record<string, PermissionSummaryState>
  >({});
  const [detailNotice, setDetailNotice] = useState<DetailNotice | null>(null);
  const [newRobotForm, setNewRobotForm] = useState<NewRobotForm>({
    name: "",
    appId: "",
    appSecret: "",
  });
  const [connectMode, setConnectMode] = useState<"qr" | "manual">("qr");
  const [onboardingSession, setOnboardingSession] =
    useState<FeishuOnboardingSession | null>(null);
  const [connectError, setConnectError] = useState("");
  const [autostart, setAutostart] = useState<AutostartDetectResponse | null>(
    null,
  );
  const [autostartError, setAutostartError] = useState("");
  const [vscode, setVSCode] = useState<VSCodeDetectResponse | null>(null);
  const [vscodeError, setVSCodeError] = useState("");
  const [imageStaging, setImageStaging] =
    useState<ImageStagingStatusResponse | null>(null);
  const [imageStagingError, setImageStagingError] = useState("");
  const [logsStorage, setLogsStorage] = useState<LogsStorageStatusResponse | null>(
    null,
  );
  const [logsStorageError, setLogsStorageError] = useState("");
  const [previewMap, setPreviewMap] = useState<
    Record<string, PreviewDriveStatusResponse>
  >({});
  const [previewError, setPreviewError] = useState("");
  const [actionBusy, setActionBusy] = useState("");
  const [deleteTargetID, setDeleteTargetID] = useState<string | null>(null);

  const selectedApp = useMemo(
    () => apps.find((app) => app.id === selectedRobotID) ?? null,
    [apps, selectedRobotID],
  );
  const selectedPermission: PermissionSummaryState = selectedApp
    ? permissionChecks[selectedApp.id] || { status: "idle" }
    : { status: "idle" };
  const versionTitle = buildAdminPageTitle(bootstrap);
  const previewSummary = useMemo(() => {
    return Object.values(previewMap).reduce(
      (accumulator, item) => {
        accumulator.fileCount += item.summary.fileCount;
        accumulator.bytes += item.summary.estimatedBytes;
        return accumulator;
      },
      { fileCount: 0, bytes: 0 },
    );
  }, [previewMap]);

  useEffect(() => {
    document.title = versionTitle;
  }, [versionTitle]);

  useEffect(() => {
    void loadAdminPage().catch(() => {
      setLoadError("当前页面暂时无法读取状态，请刷新后重试。");
      setLoading(false);
    });
  }, []);

  useEffect(() => {
    setPermissionChecks((current) => {
      const next: Record<string, PermissionSummaryState> = {};
      for (const app of apps) {
        next[app.id] = current[app.id] || { status: "idle" };
      }
      return next;
    });
  }, [apps]);

  useEffect(() => {
    for (const app of apps) {
      const current = permissionChecks[app.id];
      if (current && current.status !== "idle") {
        continue;
      }
      void loadPermissionCheck(app.id);
    }
  }, [apps, permissionChecks]);

  useEffect(() => {
    if (selectedRobotID === newRobotID) {
      return;
    }
    setConnectError("");
    setOnboardingSession(null);
  }, [selectedRobotID]);

  useEffect(() => {
    if (selectedRobotID !== newRobotID || connectMode !== "qr") {
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
    const timer = window.setTimeout(() => {
      void refreshQRCodeSession(onboardingSession.id);
    }, 2_000);
    return () => window.clearTimeout(timer);
  }, [actionBusy, connectError, connectMode, onboardingSession, selectedRobotID]);

  async function loadAdminPage(options?: { preferredRobotID?: string }) {
    setLoading(true);
    setLoadError("");

    const [
      bootstrapState,
      appList,
      autostartState,
      vscodeState,
      imageResult,
      logsResult,
    ] = await Promise.all([
      requestJSON<BootstrapState>("/api/admin/bootstrap-state"),
      requestJSON<FeishuAppsResponse>("/api/admin/feishu/apps"),
      loadAutostartState("/api/admin/autostart/detect"),
      loadVSCodeState("/api/admin/vscode/detect"),
      safeRequest<ImageStagingStatusResponse>("/api/admin/storage/image-staging"),
      safeRequest<LogsStorageStatusResponse>("/api/admin/storage/logs"),
    ]);

    const previewResults = await Promise.allSettled(
      appList.apps.map(async (app) => {
        const data = await requestJSON<PreviewDriveStatusResponse>(
          `/api/admin/storage/preview-drive/${encodeURIComponent(app.id)}`,
        );
        return [app.id, data] as const;
      }),
    );

    const previews: Record<string, PreviewDriveStatusResponse> = {};
    let previewFailed = false;
    previewResults.forEach((result) => {
      if (result.status === "fulfilled") {
        previews[result.value[0]] = result.value[1];
        return;
      }
      previewFailed = true;
    });

    const nextSelectedRobotID =
      appList.apps.find((app) => app.id === options?.preferredRobotID)?.id ||
      appList.apps.find((app) => app.id === selectedRobotID)?.id ||
      appList.apps[0]?.id ||
      newRobotID;

    setBootstrap(bootstrapState);
    setApps(appList.apps);
    setSelectedRobotID(nextSelectedRobotID);
    setAutostart(autostartState.data);
    setAutostartError(autostartState.error);
    setVSCode(vscodeState.data);
    setVSCodeError(vscodeState.error);
    setImageStaging(imageResult.data);
    setImageStagingError(imageResult.error);
    setLogsStorage(logsResult.data);
    setLogsStorageError(logsResult.error);
    setPreviewMap(previews);
    setPreviewError(previewFailed ? "部分预览文件状态暂时没有读取成功。" : "");
    setLoading(false);
  }

  async function loadPermissionCheck(appID: string) {
    setPermissionChecks((current) => ({
      ...current,
      [appID]: { status: "loading" },
    }));
    const response = await requestJSONAllowHTTPError<
      FeishuAppPermissionCheckResponse | APIErrorShape
    >(`/api/admin/feishu/apps/${encodeURIComponent(appID)}/permission-check`);
    if (!response.ok) {
      setPermissionChecks((current) => ({
        ...current,
        [appID]: {
          status: "error",
          message: "暂时无法读取权限状态，请稍后重试。",
        },
      }));
      return;
    }
    const payload = response.data as FeishuAppPermissionCheckResponse;
    setPermissionChecks((current) => ({
      ...current,
      [appID]: payload.ready
        ? { status: "ready", data: payload }
        : { status: "missing", data: payload },
    }));
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
        "/api/admin/feishu/onboarding/sessions",
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
        `/api/admin/feishu/onboarding/sessions/${encodeURIComponent(sessionID)}`,
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
        `/api/admin/feishu/onboarding/sessions/${encodeURIComponent(sessionID)}/complete`,
        { method: "POST" },
      );
      setOnboardingSession(response.data.session);
      if (!response.ok) {
        setConnectError("扫码已经完成，但连接验证没有通过，请重新验证。");
        return;
      }
      await loadAdminPage({ preferredRobotID: response.data.app.id });
      setSelectedRobotID(response.data.app.id);
      setDetailNotice({ tone: "good", message: "已完成连接验证。" });
      setConnectError("");
      setOnboardingSession(null);
      void loadPermissionCheck(response.data.app.id);
    } catch {
      setConnectError("扫码已经完成，但当前还不能继续，请稍后重试。");
    } finally {
      setActionBusy("");
    }
  }

  async function createRobot() {
    if (!newRobotForm.appId.trim() || !newRobotForm.appSecret.trim()) {
      setDetailNotice({
        tone: "danger",
        message: "请填写完整的 App ID 和 App Secret。",
      });
      return;
    }

    setActionBusy("create-robot");
    try {
      const saved = await sendJSON<FeishuAppResponse>("/api/admin/feishu/apps", "POST", {
        name: blankToUndefined(newRobotForm.name),
        appId: blankToUndefined(newRobotForm.appId),
        appSecret: blankToUndefined(newRobotForm.appSecret),
        enabled: true,
      });
      const verify = await requestJSONAllowHTTPError<FeishuAppVerifyResponse>(
        `/api/admin/feishu/apps/${encodeURIComponent(saved.app.id)}/verify`,
        { method: "POST" },
      );
      await loadAdminPage({ preferredRobotID: saved.app.id });
      setSelectedRobotID(saved.app.id);
      void loadPermissionCheck(saved.app.id);
      if (!verify.ok) {
        setDetailNotice({
          tone: "danger",
          message: "连接验证没有通过，请检查 App ID 和 App Secret 后重试。",
        });
        return;
      }
      setDetailNotice({ tone: "good", message: "已完成连接验证。" });
      setNewRobotForm({ name: "", appId: "", appSecret: "" });
    } catch (error: unknown) {
      if (await maybeRecoverRuntimeApplyFailure(error)) {
        return;
      }
      setDetailNotice({ tone: "danger", message: "当前还不能保存这个机器人，请稍后重试。" });
    } finally {
      setActionBusy("");
    }
  }

  async function maybeRecoverRuntimeApplyFailure(error: unknown): Promise<boolean> {
    if (!(error instanceof APIRequestError) || error.code !== "gateway_apply_failed") {
      return false;
    }
    const details = error.details as RuntimeApplyFailureDetails | undefined;
    await loadAdminPage({
      preferredRobotID: details?.app?.id || details?.gatewayId,
    });
    if (details?.app?.id || details?.gatewayId) {
      setSelectedRobotID(details?.app?.id || details?.gatewayId || newRobotID);
    }
    setDetailNotice({
      tone: "warn",
      message:
        "配置已经保存，但当前运行中的机器人还没有同步完成。请稍后刷新状态后再继续。",
    });
    return true;
  }

  async function triggerRobotTest(kind: "events" | "callback") {
    if (!selectedApp?.id) {
      return;
    }
    setActionBusy(`test-${kind}`);
    const response = await requestJSONAllowHTTPError<
      FeishuAppTestStartResponse | APIErrorShape
    >(`/api/admin/feishu/apps/${encodeURIComponent(selectedApp.id)}/${kind === "events" ? "test-events" : "test-callback"}`, {
      method: "POST",
    });
    if (!response.ok) {
      const payload = readAPIError(response);
      setDetailNotice({
        tone: "danger",
        message:
          payload?.code === "feishu_app_web_test_recipient_unavailable"
            ? String(payload.details || "当前机器人还没有可用的飞书测试接收者。")
            : kind === "events"
              ? "事件订阅测试没有发出，请稍后重试。"
              : "回调测试没有发出，请稍后重试。",
      });
      setActionBusy("");
      return;
    }
    const payload = response.data as FeishuAppTestStartResponse;
    setDetailNotice({
      tone: "good",
      message: payload.message,
    });
    setActionBusy("");
  }

  async function deleteRobot() {
    if (!deleteTargetID) {
      return;
    }
    setActionBusy("delete-robot");
    try {
      const response = await requestJSONAllowHTTPError<unknown>(
        `/api/admin/feishu/apps/${encodeURIComponent(deleteTargetID)}`,
        { method: "DELETE" },
      );
      if (!response.ok) {
        throw new APIRequestError(
          response.status,
          "delete failed",
          readAPIError(response)?.code,
          readAPIError(response)?.details,
        );
      }
      await loadAdminPage();
      setDetailNotice({ tone: "good", message: "机器人已删除。" });
      setDeleteTargetID(null);
    } catch (error: unknown) {
      if (await maybeRecoverRuntimeApplyFailure(error)) {
        setDeleteTargetID(null);
        return;
      }
      setDetailNotice({ tone: "danger", message: "当前还不能删除这个机器人，请稍后重试。" });
    } finally {
      setActionBusy("");
      setDeleteTargetID(null);
    }
  }

  async function enableAutostart() {
    setActionBusy("autostart");
    try {
      const response = await sendJSON<AutostartDetectResponse>(
        "/api/admin/autostart/apply",
        "POST",
      );
      setAutostart(response);
      setAutostartError("");
    } catch {
      setAutostartError("自动运行设置暂时没有更新成功。");
    } finally {
      setActionBusy("");
    }
  }

  async function repairVSCode() {
    setActionBusy("vscode");
    try {
      if (!vscode) {
        await loadAdminPage({ preferredRobotID: selectedRobotID });
        return;
      }
      if (vscode.needsShimReinstall && vscode.latestBundleEntrypoint) {
        const response = await sendJSON<VSCodeDetectResponse>(
          "/api/admin/vscode/reinstall-shim",
          "POST",
          { bundleEntrypoint: vscode.latestBundleEntrypoint },
        );
        setVSCode(response);
        setVSCodeError("");
        return;
      }
      const mode = vscodeApplyModeForScenario(vscode, "current_machine");
      const response = await sendJSON<VSCodeDetectResponse>(
        "/api/admin/vscode/apply",
        "POST",
        {
          mode: mode || "managed_shim",
          bundleEntrypoint: vscode.latestBundleEntrypoint,
        },
      );
      setVSCode(response);
      setVSCodeError("");
    } catch {
      setVSCodeError("VS Code 集成暂时没有更新成功。");
    } finally {
      setActionBusy("");
    }
  }

  async function cleanupImageStaging() {
    setActionBusy("cleanup-image");
    try {
      const response = await sendJSON<ImageStagingCleanupResponse>(
        "/api/admin/storage/image-staging/cleanup",
        "POST",
      );
      setImageStaging((current) =>
        current
          ? {
              ...current,
              fileCount: response.remainingFileCount,
              totalBytes: response.remainingBytes,
            }
          : current,
      );
      setImageStagingError("");
    } catch {
      setImageStagingError("图片暂存清理没有完成，请稍后重试。");
    } finally {
      setActionBusy("");
    }
  }

  async function cleanupLogsStorage() {
    setActionBusy("cleanup-logs");
    try {
      const response = await sendJSON<LogsStorageCleanupResponse>(
        "/api/admin/storage/logs/cleanup",
        "POST",
        { olderThanHours: 24 },
      );
      setLogsStorage((current) =>
        current
          ? {
              ...current,
              fileCount: response.remainingFileCount,
              totalBytes: response.remainingBytes,
            }
          : current,
      );
      setLogsStorageError("");
    } catch {
      setLogsStorageError("日志清理没有完成，请稍后重试。");
    } finally {
      setActionBusy("");
    }
  }

  async function cleanupPreviewDrive() {
    if (apps.length === 0) {
      return;
    }
    setActionBusy("cleanup-preview");
    try {
      const results = await Promise.allSettled(
        apps.map((app) =>
          sendJSON<PreviewDriveCleanupResponse>(
            `/api/admin/storage/preview-drive/${encodeURIComponent(app.id)}/cleanup`,
            "POST",
          ),
        ),
      );
      const nextMap: Record<string, PreviewDriveStatusResponse> = { ...previewMap };
      let failed = false;
      results.forEach((result) => {
        if (result.status !== "fulfilled") {
          failed = true;
          return;
        }
        nextMap[result.value.gatewayId] = {
          gatewayId: result.value.gatewayId,
          name: result.value.name,
          summary: result.value.result.summary,
        };
      });
      setPreviewMap(nextMap);
      setPreviewError(failed ? "部分预览文件暂时没有清理成功。" : "");
    } catch {
      setPreviewError("预览文件清理没有完成，请稍后重试。");
    } finally {
      setActionBusy("");
    }
  }

  function renderRobotDetail() {
    if (!selectedApp) {
      return (
        <section className="panel">
          <div className="step-stage-head">
            <h2>新增机器人</h2>
            <p>选择扫码创建或手动输入，连接验证通过后会自动加入机器人列表。</p>
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
          {connectMode === "qr" ? (
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
                      扫码成功，连接验证已通过，正在加入机器人列表...
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
          ) : (
            <div className="panel">
              <div className="form-grid">
                <label className="field">
                  <span>
                    App ID <em className="field-required">*</em>
                  </span>
                  <input
                    aria-label="App ID"
                    placeholder="请输入 App ID"
                    value={newRobotForm.appId}
                    onChange={(event) =>
                      setNewRobotForm((current) => ({
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
                    placeholder="请输入 App Secret"
                    value={newRobotForm.appSecret}
                    onChange={(event) =>
                      setNewRobotForm((current) => ({
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
                    placeholder="例如：运营机器人"
                    value={newRobotForm.name}
                    onChange={(event) =>
                      setNewRobotForm((current) => ({
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
                  disabled={actionBusy === "create-robot"}
                  onClick={() => void createRobot()}
                >
                  验证并保存
                </button>
              </div>
            </div>
          )}
          {detailNotice ? (
            <div className={`notice-banner ${detailNotice.tone}`}>
              {detailNotice.message}
            </div>
          ) : null}
        </section>
      );
    }

    return (
      <section className="panel">
        <div className="step-stage-head">
          <h2>{selectedApp.name || "未命名机器人"}</h2>
          <p>机器人状态与测试入口。</p>
        </div>
        <dl className="definition-list">
          <div>
            <dt>App ID</dt>
            <dd>{selectedApp.appId || "未填写"}</dd>
          </div>
          <div>
            <dt>连接状态</dt>
            <dd>{describeConnectionState(selectedApp)}</dd>
          </div>
          <div>
            <dt>启用状态</dt>
            <dd>{selectedApp.enabled ? "已启用" : "未启用"}</dd>
          </div>
          <div>
            <dt>最近验证</dt>
            <dd>{selectedApp.verifiedAt ? formatTimestamp(selectedApp.verifiedAt) : "暂未验证"}</dd>
          </div>
        </dl>
        {selectedApp.runtimeApply?.pending ? (
          <div className="notice-banner warn">
            当前机器人还在同步设置，请稍后刷新状态后再继续操作。
          </div>
        ) : null}
        {selectedPermission.status === "missing" ? (
          <>
            <div className="notice-banner danger">当前还需要补齐权限。</div>
            <div className="scope-list">
              {(selectedPermission.data.missingScopes || []).map((scope: { scope: string; scopeType?: string }) => (
                <span
                  key={`${scope.scopeType || "tenant"}-${scope.scope}`}
                  className="scope-pill"
                >
                  <code>{scope.scope}</code>
                </span>
              ))}
            </div>
            <p className="support-copy">
              前往{" "}
              <a
                className="inline-link"
                href={
                  selectedPermission.data.app.consoleLinks?.auth ||
                  selectedApp.consoleLinks?.auth ||
                  "#"
                }
                rel="noreferrer"
                target="_blank"
              >
                打开飞书后台处理权限
              </a>
              。
            </p>
          </>
        ) : null}
        {selectedPermission.status === "error" ? (
          <div className="notice-banner warn">{selectedPermission.message}</div>
        ) : null}
        {detailNotice ? (
          <div className={`notice-banner ${detailNotice.tone}`}>
            {detailNotice.message}
          </div>
        ) : null}
        <div className="button-row">
          <button
            className="primary-button"
            type="button"
            disabled={actionBusy === "test-events" || Boolean(selectedApp.runtimeApply?.pending)}
            onClick={() => void triggerRobotTest("events")}
          >
            测试事件订阅
          </button>
          <button
            className="secondary-button"
            type="button"
            disabled={actionBusy === "test-callback" || Boolean(selectedApp.runtimeApply?.pending)}
            onClick={() => void triggerRobotTest("callback")}
          >
            测试回调
          </button>
          <button
            className="danger-button"
            type="button"
            disabled={Boolean(selectedApp.readOnly)}
            onClick={() => setDeleteTargetID(selectedApp.id)}
          >
            删除机器人
          </button>
        </div>
        {selectedApp.readOnly ? (
          <p className="support-copy">当前机器人由运行环境提供，不能在这里删除。</p>
        ) : null}
      </section>
    );
  }

  if (loading) {
    return (
      <div className="product-page">
        <header className="product-topbar">
          <h1>{versionTitle}</h1>
          <p>管理机器人、系统集成与本地存储。</p>
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
          <h1>{versionTitle}</h1>
          <p>管理机器人、系统集成与本地存储。</p>
        </header>
        <section className="panel">
          <div className="empty-state error">
            <strong>当前页面暂时无法打开</strong>
            <p>{loadError}</p>
            <div className="button-row">
              <button
                className="secondary-button"
                type="button"
                onClick={() => void loadAdminPage()}
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
        <h1>{versionTitle}</h1>
        <p>管理机器人、系统集成与本地存储。</p>
      </header>

      <section className="panel">
        <div className="step-stage-head">
          <h2>机器人管理</h2>
          <p>查看所有机器人并处理需要关注的状态。</p>
        </div>
        <div className="robot-layout" style={{ marginTop: "1rem" }}>
          <div className="robot-list">
            {apps.map((app) => {
              const permission = permissionChecks[app.id];
              const showWarn =
                permission?.status === "missing" || Boolean(app.runtimeApply?.pending);
              return (
                <button
                  key={app.id}
                  className={`robot-list-button${selectedRobotID === app.id ? " active" : ""}`}
                  type="button"
                  onClick={() => {
                    setDetailNotice(null);
                    setSelectedRobotID(app.id);
                  }}
                >
                  <div className="robot-list-head">
                    <strong>{app.name || "未命名机器人"}</strong>
                    {showWarn ? <span className="robot-tag warn">有异常</span> : null}
                  </div>
                  <p>{app.appId || "未填写 App ID"}</p>
                </button>
              );
            })}
            <button
              className={`robot-list-button${selectedRobotID === newRobotID ? " active" : ""}`}
              type="button"
              onClick={() => {
                setDetailNotice(null);
                setSelectedRobotID(newRobotID);
              }}
            >
              <div className="robot-list-head">
                <strong>新增机器人</strong>
                <span className="robot-tag">新增</span>
              </div>
              <p>点击开始接入</p>
            </button>
          </div>
          {renderRobotDetail()}
        </div>
      </section>

      <section className="panel">
        <div className="step-stage-head">
          <h2>系统集成</h2>
          <p>统一管理自动运行设置与 VS Code 集成。</p>
        </div>
        <div className="soft-grid two-column" style={{ marginTop: "1rem" }}>
          <article className="soft-card-v2">
            <h4>自动运行设置</h4>
            <p>{describeAutostart(autostart, autostartError)}</p>
            {autostartError ? (
              <div className="notice-banner warn">{autostartError}</div>
            ) : null}
            {!autostartError && autostart?.supported && !autostart.enabled ? (
              <div className="button-row">
                <button
                  className="secondary-button"
                  type="button"
                  disabled={actionBusy === "autostart" || !autostart.canApply}
                  onClick={() => void enableAutostart()}
                >
                  启用自动运行
                </button>
              </div>
            ) : null}
          </article>
          <article className="soft-card-v2">
            <h4>VS Code 集成</h4>
            <p>{describeVSCode(vscode, vscodeError)}</p>
            {vscodeError ? (
              <div className="notice-banner warn">{vscodeError}</div>
            ) : null}
            <div className="button-row">
              <button
                className="ghost-button"
                type="button"
                disabled={actionBusy === "vscode"}
                onClick={() => void repairVSCode()}
              >
                重新检查并修复
              </button>
            </div>
          </article>
        </div>
      </section>

      <section className="panel">
        <div className="step-stage-head">
          <h2>存储管理</h2>
          <p>查看占用并按需清理旧文件。</p>
        </div>
        <div className="soft-grid" style={{ marginTop: "1rem" }}>
          <article className="soft-card-v2">
            <h4>预览文件</h4>
            <p>
              {formatFileSummary(previewSummary.fileCount, previewSummary.bytes)}
            </p>
            {previewError ? <div className="notice-banner warn">{previewError}</div> : null}
            <div className="button-row">
              <button
                className="secondary-button"
                type="button"
                disabled={actionBusy === "cleanup-preview" || apps.length === 0}
                onClick={() => void cleanupPreviewDrive()}
              >
                清理旧预览
              </button>
            </div>
          </article>
          <article className="soft-card-v2">
            <h4>图片暂存</h4>
            <p>
              {formatFileSummary(
                imageStaging?.fileCount || 0,
                imageStaging?.totalBytes || 0,
              )}
            </p>
            {imageStagingError ? (
              <div className="notice-banner warn">{imageStagingError}</div>
            ) : null}
            <div className="button-row">
              <button
                className="secondary-button"
                type="button"
                disabled={actionBusy === "cleanup-image"}
                onClick={() => void cleanupImageStaging()}
              >
                清理旧图片
              </button>
            </div>
          </article>
          <article className="soft-card-v2">
            <h4>日志文件</h4>
            <p>
              {formatFileSummary(
                logsStorage?.fileCount || 0,
                logsStorage?.totalBytes || 0,
              )}
            </p>
            {logsStorageError ? (
              <div className="notice-banner warn">{logsStorageError}</div>
            ) : null}
            <div className="button-row">
              <button
                className="secondary-button"
                type="button"
                disabled={actionBusy === "cleanup-logs"}
                onClick={() => void cleanupLogsStorage()}
              >
                清理一天前日志
              </button>
            </div>
          </article>
        </div>
      </section>

      {deleteTargetID ? (
        <div className="modal-backdrop" role="presentation">
          <div
            className="modal-card"
            role="dialog"
            aria-modal="true"
            aria-labelledby="delete-robot-title"
          >
            <h3 id="delete-robot-title">确认删除机器人</h3>
            <p className="modal-copy">
              删除后将移除“
              {apps.find((app) => app.id === deleteTargetID)?.name || "当前机器人"}
              ”，此操作不可恢复。
            </p>
            <div className="modal-actions">
              <button
                className="ghost-button"
                type="button"
                onClick={() => setDeleteTargetID(null)}
              >
                取消
              </button>
              <button
                className="danger-button"
                type="button"
                disabled={actionBusy === "delete-robot"}
                onClick={() => void deleteRobot()}
              >
                确认删除
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

async function safeRequest<T>(path: string) {
  try {
    return {
      data: await requestJSON<T>(path),
      error: "",
    };
  } catch {
    return {
      data: null,
      error: "暂时没有读取成功，请稍后重试。",
    };
  }
}

function blankToUndefined(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed ? trimmed : undefined;
}

function readAPIError(response: { ok: boolean; data: unknown }) {
  if (response.ok) {
    return null;
  }
  const payload = response.data as APIErrorShape;
  return payload.error || null;
}

function buildAdminPageTitle(bootstrap: BootstrapState | null): string {
  const name = bootstrap?.product.name?.trim() || "Codex Remote Feishu";
  const version = bootstrap?.product.version?.trim();
  return version ? `${name} ${version} 管理` : `${name} 管理`;
}

function describeConnectionState(app: FeishuAppSummary): string {
  switch (app.status?.state) {
    case "connected":
      return "连接正常";
    case "disabled":
      return "已停用";
    case "error":
      return "需要处理";
    default:
      return "待确认";
  }
}

function describeAutostart(
  autostart: AutostartDetectResponse | null,
  error: string,
): string {
  if (error) {
    return "暂时没有读取成功。";
  }
  if (!autostart) {
    return "暂时没有读取成功。";
  }
  if (!autostart.supported) {
    return "当前系统不支持。";
  }
  return autostart.enabled ? "当前已启用。" : "当前未启用。";
}

function describeVSCode(
  vscode: VSCodeDetectResponse | null,
  error: string,
): string {
  if (error) {
    return "暂时没有读取成功。";
  }
  if (!vscode) {
    return "暂时没有读取成功。";
  }
  return vscodeIsReady(vscode)
    ? "当前已接入。"
    : "检测到 VS Code 集成未完成，请先修复。";
}

function formatBytes(value: number): string {
  if (value <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let current = value;
  let index = 0;
  while (current >= 1024 && index < units.length - 1) {
    current /= 1024;
    index += 1;
  }
  return `${current >= 100 || index === 0 ? current.toFixed(0) : current.toFixed(1)} ${units[index]}`;
}

function formatFileSummary(fileCount: number, bytes: number): string {
  return `${fileCount} 个文件，约 ${formatBytes(bytes)}`;
}

function formatTimestamp(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "暂不可用";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}
