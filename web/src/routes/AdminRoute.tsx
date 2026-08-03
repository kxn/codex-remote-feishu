import { type ReactNode, useEffect, useMemo, useState } from "react";
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
  ClaudeProfilesResponse,
  ClaudeProfileSummary,
  CodexProfilesResponse,
  CodexProfileSummary,
  FeishuAppAutoConfigCompleteView,
  FeishuAppAutoConfigCompleteResponse,
  FeishuAppAutoConfigPlanResponse,
  FeishuAppPermissionCheckResponse,
  FeishuAppResponse,
  FeishuAppSummary,
  FeishuAppsResponse,
  ImageStagingCleanupResponse,
  ImageStagingStatusResponse,
  LogsStorageCleanupResponse,
  LogsStorageStatusResponse,
  PreviewDriveCleanupResponse,
  PreviewDriveStatusResponse,
  VSCodeDetectResponse,
} from "../lib/types";
import {
  blankToUndefined,
  loadAutostartState,
  loadVSCodeState,
  readAPIError,
  vscodeApplyModeForScenario,
  vscodeIsReady,
} from "./shared/helpers";
import {
  resolveRuntimeApplyFailureTarget,
  runAutoConfigMutation,
  saveAndVerifyFeishuApp,
  useQRCodeOnboardingFlow,
} from "./shared/feishuFlow";
import {
  describeAutoConfigActionFeedback,
  describeAutoConfigRefreshFeedback,
  describeAutoConfigSummary,
  autoConfigNoticeTone,
} from "./shared/feishuAutoConfig";
import { runAdminStorageCleanup } from "./shared/adminStorage";
import { ClaudeProfileSection } from "./admin/ClaudeProfileSection";
import { CodexProviderSection } from "./admin/CodexProviderSection";
import { BrandLockup, Toast } from "../components/ui";

type NoticeTone = "good" | "warn" | "danger";

type DetailNotice = {
  tone: NoticeTone;
  message: string;
};

type AutoConfigState =
  | { status: "idle" }
  | { status: "ready"; data: FeishuAppAutoConfigPlanResponse; resultStatus?: string }
  | { status: "error"; message: string };

type PermissionCheckState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "ready"; data: FeishuAppPermissionCheckResponse }
  | { status: "error"; message: string };

type NewRobotForm = {
  name: string;
  appId: string;
  appSecret: string;
};

type AdminAreaID = "overview" | "bots" | "backends" | "system";
type BackendTabID = "claude" | "codex";
type OverviewTodoItem = {
  id: string;
  text: string;
  tone: "warn" | "danger" | "idle";
  area: AdminAreaID;
  robotID?: string;
};

const newRobotID = "new";

const adminAreas: Array<{ id: AdminAreaID; name: string }> = [
  { id: "overview", name: "总览" },
  { id: "bots", name: "机器人" },
  { id: "backends", name: "对话后端" },
  { id: "system", name: "系统" },
];

export function AdminRoute() {
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [activeArea, setActiveArea] = useState<AdminAreaID>("overview");
  const [backendTab, setBackendTab] = useState<BackendTabID>("claude");
  const [bootstrap, setBootstrap] = useState<BootstrapState | null>(null);
  const [apps, setApps] = useState<FeishuAppSummary[]>([]);
  const [selectedRobotID, setSelectedRobotID] = useState(newRobotID);
  const [autoConfigPlans, setAutoConfigPlans] = useState<Record<string, AutoConfigState>>(
    {},
  );
  const [autoConfigCheckedAt, setAutoConfigCheckedAt] = useState<Record<string, string>>(
    {},
  );
  const [permissionChecks, setPermissionChecks] = useState<
    Record<string, PermissionCheckState>
  >({});
  const [detailNotice, setDetailNotice] = useState<DetailNotice | null>(null);
  const [codexProviders, setCodexProviders] = useState<CodexProfileSummary[]>([]);
  const [codexProvidersError, setCodexProvidersError] = useState("");
  const [claudeProfiles, setClaudeProfiles] = useState<ClaudeProfileSummary[]>([]);
  const [claudeProfilesError, setClaudeProfilesError] = useState("");
  const [newRobotForm, setNewRobotForm] = useState<NewRobotForm>({
    name: "",
    appId: "",
    appSecret: "",
  });
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
  const selectedAutoConfig: AutoConfigState = selectedApp
    ? autoConfigPlans[selectedApp.id] || { status: "idle" }
    : { status: "idle" };
  const selectedPermissionCheck: PermissionCheckState = selectedApp
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
  const {
    connectMode,
    connectError,
    onboardingSession,
    changeConnectMode,
    clearConnectError,
    completeQRCodeSession,
    resetConnectFlow,
  } = useQRCodeOnboardingFlow({
    enabled: selectedRobotID === newRobotID,
    actionBusy,
    setActionBusy,
    sessionsPath: "/api/admin/feishu/onboarding/sessions",
    onCompleteSuccess: async (appID, _session, response) => {
      await loadAdminPage({ preferredRobotID: appID });
      syncAutoConfigView(response.app, response.autoConfig);
      setSelectedRobotID(appID);
      setActiveArea("bots");
      setDetailNotice(noticeFromCompleteView(response.autoConfig) || {
        tone: "good",
        message: "已完成连接验证。",
      });
    },
    resetSessionOnSuccess: true,
  });

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
    setAutoConfigPlans((current) => {
      const next: Record<string, AutoConfigState> = {};
      for (const app of apps) {
        next[app.id] = current[app.id] || { status: "idle" };
      }
      return next;
    });
    setAutoConfigCheckedAt((current) => {
      const next: Record<string, string> = {};
      for (const app of apps) {
        if (current[app.id]) {
          next[app.id] = current[app.id];
        }
      }
      return next;
    });
    setPermissionChecks((current) => {
      const next: Record<string, PermissionCheckState> = {};
      for (const app of apps) {
        next[app.id] = current[app.id] || { status: "idle" };
      }
      return next;
    });
  }, [apps]);

  useEffect(() => {
    if (selectedRobotID === newRobotID) {
      return;
    }
    resetConnectFlow();
  }, [selectedRobotID]);

  async function loadAdminPage(options?: { preferredRobotID?: string }) {
    setLoading(true);
    setLoadError("");

    const [
      bootstrapState,
      appList,
      codexProvidersResult,
      claudeProfilesResult,
      autostartState,
      vscodeState,
      imageResult,
      logsResult,
    ] = await Promise.all([
      requestJSON<BootstrapState>("/api/admin/bootstrap-state"),
      requestJSON<FeishuAppsResponse>("/api/admin/feishu/apps"),
      safeRequest<CodexProfilesResponse>("/api/admin/codex/profiles"),
      safeRequest<ClaudeProfilesResponse>("/api/admin/claude/profiles"),
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
    setCodexProviders(codexProvidersResult.data?.profiles || []);
    setCodexProvidersError(codexProvidersResult.error);
    setClaudeProfiles(claudeProfilesResult.data?.profiles || []);
    setClaudeProfilesError(claudeProfilesResult.error);
    setAutostart(autostartState.data);
    setAutostartError(autostartState.error);
    setVSCode(vscodeState.data);
    setVSCodeError(vscodeState.error);
    setImageStaging(imageResult.data);
    setImageStagingError(imageResult.error);
    setLogsStorage(logsResult.data);
    setLogsStorageError(logsResult.error);
    setPreviewMap(previews);
    setPreviewError(previewFailed ? "部分机器人预览文件状态暂时无法读取，请稍后重试。" : "");
    setLoading(false);
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
      let savedApp: FeishuAppSummary | null = null;
      const result = await saveAndVerifyFeishuApp({
        save: async () => {
          const saved = await sendJSON<FeishuAppResponse>("/api/admin/feishu/apps", "POST", {
            name: blankToUndefined(newRobotForm.name),
            appId: blankToUndefined(newRobotForm.appId),
            appSecret: blankToUndefined(newRobotForm.appSecret),
            enabled: true,
          });
          savedApp = saved.app;
          return { appID: saved.app.id, autoConfig: saved.autoConfig };
        },
        verifyPath: (appID) =>
          `/api/admin/feishu/apps/${encodeURIComponent(appID)}/verify`,
        reload: (appID) => loadAdminPage({ preferredRobotID: appID }),
      });
      setSelectedRobotID(result.appID);
      setActiveArea("bots");
      if (!result.verified) {
        setDetailNotice({
          tone: "danger",
          message: "连接验证没有通过，请检查 App ID 和 App Secret 后重试。",
        });
        return;
      }
      if (result.autoConfig && savedApp) {
        syncAutoConfigView(savedApp, result.autoConfig);
      }
      setDetailNotice(noticeFromCompleteView(result.autoConfig) || {
        tone: "good",
        message: "已完成连接验证。",
      });
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
    const appID = resolveRuntimeApplyFailureTarget(error);
    if (!appID) {
      return false;
    }
    await loadAdminPage({
      preferredRobotID: appID,
    });
    setSelectedRobotID(appID);
    setDetailNotice({
      tone: "warn",
      message:
        "配置已经保存，但当前运行中的机器人还没有同步完成。请稍后刷新状态后再继续。",
    });
    return true;
  }

  function syncAppSummary(app: FeishuAppSummary) {
    setApps((current) => current.map((item) => (item.id === app.id ? app : item)));
  }

  function syncAutoConfigPlan(
    app: FeishuAppSummary,
    plan: FeishuAppAutoConfigPlanResponse["plan"],
    resultStatus?: string,
  ) {
    syncAppSummary(app);
    setAutoConfigPlans((current) => ({
      ...current,
      [app.id]: {
        status: "ready",
        resultStatus,
        data: {
          app,
          plan,
        },
      },
    }));
  }

  function syncAutoConfigView(
    app: FeishuAppSummary,
    view?: FeishuAppAutoConfigCompleteView,
  ) {
    if (!view) {
      return;
    }
    syncAppSummary(app);
    if (view.result) {
      syncAutoConfigPlan(app, view.result.plan, view.result.status);
      return;
    }
    if (view.error) {
      syncAutoConfigError(app.id, "已保存机器人，但暂时无法确认飞书配置结果。");
    }
  }

  function syncAutoConfigError(appID: string, message: string) {
    setAutoConfigPlans((current) => ({
      ...current,
      [appID]: { status: "error", message },
    }));
  }

  async function completeRobotConfiguration() {
    if (!selectedApp?.id) {
      return;
    }
    const appID = selectedApp.id;
    setActionBusy("permission-complete");
    try {
      const result = await runAutoConfigMutation<FeishuAppAutoConfigCompleteResponse>({
        path: `/api/admin/feishu/apps/${encodeURIComponent(appID)}/auto-config/complete`,
        init: {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
          },
          body: JSON.stringify({}),
        },
        fallbackErrorMessage: "当前还不能自动补齐，请稍后重试。",
        fallbackSuccessMessage: "已自动补齐飞书配置。",
      });
      if (!result.ok) {
        setDetailNotice({
          tone: "danger",
          message: `${result.message} 还没有提交到飞书。`,
        });
        return;
      }
      syncAutoConfigPlan(
        result.payload.app,
        result.payload.result.plan,
        result.payload.result.status,
      );
      setDetailNotice(result.notice);
      await checkRobotPermissions({ appID, silent: true });
    } catch {
      setDetailNotice({ tone: "danger", message: "当前还不能自动补齐，请稍后重试。" });
    } finally {
      setActionBusy("");
    }
  }

  async function loadRobotConfigurationPlan(appID: string) {
    const response = await requestJSONAllowHTTPError<
      FeishuAppAutoConfigPlanResponse | APIErrorShape
    >(`/api/admin/feishu/apps/${encodeURIComponent(appID)}/auto-config/plan`);
    if (!response.ok) {
      syncAutoConfigError(appID, "暂时无法确认飞书自动配置状态。");
      setAutoConfigCheckedAt((current) => ({
        ...current,
        [appID]: new Date().toISOString(),
      }));
      return;
    }
    const payload = response.data as FeishuAppAutoConfigPlanResponse;
    syncAutoConfigPlan(payload.app, payload.plan);
    setAutoConfigCheckedAt((current) => ({
      ...current,
      [appID]: new Date().toISOString(),
    }));
    return payload;
  }

  async function recheckRobotConfigurationPlan() {
    if (!selectedApp?.id) {
      return;
    }
    const appID = selectedApp.id;
    setActionBusy("permission-auto-config-check");
    try {
      const payload = await loadRobotConfigurationPlan(appID);
      if (!payload) {
        setDetailNotice({
          tone: "warn",
          message: "重新检查没有完成，暂时无法确认飞书自动配置状态。",
        });
        return;
      }
      setDetailNotice({
        tone: autoConfigNoticeTone(payload.plan.status),
        message: describeAutoConfigRefreshFeedback(payload.plan.status),
      });
    } catch {
      syncAutoConfigError(appID, "暂时无法确认飞书自动配置状态。");
      setDetailNotice({
        tone: "warn",
        message: "重新检查没有完成，暂时无法确认飞书自动配置状态。",
      });
    } finally {
      setActionBusy("");
    }
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
    await runAdminStorageCleanup({
      busyKey: "cleanup-image",
      setActionBusy,
      request: () =>
        sendJSON<ImageStagingCleanupResponse>(
          "/api/admin/storage/image-staging/cleanup",
          "POST",
        ),
      onSuccess: (response) => {
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
      },
      onError: () => {
        setImageStagingError("图片暂存清理没有完成，请稍后重试。");
      },
    });
  }

  async function cleanupLogsStorage() {
    await runAdminStorageCleanup({
      busyKey: "cleanup-logs",
      setActionBusy,
      request: () =>
        sendJSON<LogsStorageCleanupResponse>(
          "/api/admin/storage/logs/cleanup",
          "POST",
          { olderThanHours: 24 },
        ),
      onSuccess: (response) => {
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
      },
      onError: () => {
        setLogsStorageError("日志清理没有完成，请稍后重试。");
      },
    });
  }

  async function cleanupPreviewDrive() {
    if (apps.length === 0) {
      return;
    }
    await runAdminStorageCleanup({
      busyKey: "cleanup-preview",
      setActionBusy,
      request: () =>
        Promise.allSettled(
          apps.map((app) =>
            sendJSON<PreviewDriveCleanupResponse>(
              `/api/admin/storage/preview-drive/${encodeURIComponent(app.id)}/cleanup`,
              "POST",
            ),
          ),
        ),
      onSuccess: (results) => {
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
      },
      onError: () => {
        setPreviewError("预览文件清理没有完成，请稍后重试。");
      },
    });
  }

  async function checkRobotPermissions(options?: { appID?: string; silent?: boolean }) {
    const appID = options?.appID || selectedApp?.id;
    if (!appID) {
      return;
    }
    setPermissionChecks((current) => ({
      ...current,
      [appID]: { status: "loading" },
    }));
    if (!options?.silent) {
      setActionBusy("permission-check");
    }
    try {
      const response = await requestJSONAllowHTTPError<
        FeishuAppPermissionCheckResponse | APIErrorShape
      >(`/api/admin/feishu/apps/${encodeURIComponent(appID)}/permissions/check`);
      if (!response.ok) {
        setPermissionChecks((current) => ({
          ...current,
          [appID]: {
            status: "error",
            message: "当前还不能完成权限检查，请稍后重试。",
          },
        }));
        if (!options?.silent) {
          setDetailNotice({ tone: "danger", message: "权限检查没有完成，请稍后重试。" });
        }
        return;
      }
      const data = response.data as FeishuAppPermissionCheckResponse;
      syncAppSummary(data.app);
      setPermissionChecks((current) => ({
        ...current,
        [appID]: { status: "ready", data },
      }));
      await loadRobotConfigurationPlan(appID);
    } catch {
      setPermissionChecks((current) => ({
        ...current,
        [appID]: {
          status: "error",
          message: "当前还不能完成权限检查，请稍后重试。",
        },
      }));
      if (!options?.silent) {
        setDetailNotice({ tone: "danger", message: "权限检查没有完成，请稍后重试。" });
      }
    } finally {
      if (!options?.silent) {
        setActionBusy("");
      }
    }
  }

  async function copyPermissionGrantJSON(data: FeishuAppPermissionCheckResponse) {
    const content = data.grantJSON?.trim();
    if (!content || !navigator.clipboard?.writeText) {
      setDetailNotice({
        tone: "warn",
        message: "当前浏览器不能自动复制，请手动选择导入 JSON。",
      });
      return;
    }
    try {
      await navigator.clipboard.writeText(content);
      setDetailNotice({ tone: "good", message: "导入 JSON 已复制。" });
    } catch {
      setDetailNotice({
        tone: "warn",
        message: "当前浏览器不能自动复制，请手动选择导入 JSON。",
      });
    }
  }

  function renderRobotDetail() {
    if (!selectedApp) {
      return (
        <section className="card">
          <div className="step-stage-head">
            <h3>添加机器人</h3>
            <p>扫码或手动输入接入飞书应用</p>
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
            <div className="scan-preview">
              <div>
                <div className="scan-frame">
                  {onboardingSession?.qrCodeDataUrl ? (
                    <img alt="飞书扫码创建二维码" src={onboardingSession.qrCodeDataUrl} />
                  ) : (
                    <span>二维码准备中</span>
                  )}
                </div>
                <p className="qr-hint">用飞书扫一扫，创建并接入机器人</p>
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
          ) : (
            <div className="detail-stack stack-top">
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
                  连接并验证
                </button>
              </div>
            </div>
          )}
        </section>
      );
    }

    return (
      <>
        <section className="card">
          <div className="card-head">
            <div>
              <h3>{selectedApp.name || "未命名机器人"}</h3>
            </div>
            <span className={`badge ${connectionTone(selectedApp)}`}>
              {describeConnectionState(selectedApp)}
            </span>
          </div>
          <dl className="definition-list">
            <div>
              <dt>连接</dt>
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
        </section>
        {renderPermissionCheckCard()}
        <section className="card">
          <h3>连接信息</h3>
          <dl className="definition-list">
            <div>
              <dt>App ID</dt>
              <dd className="mono">{selectedApp.appId || "未填写"}</dd>
            </div>
            <div>
              <dt>飞书后台</dt>
              <dd>
                {selectedApp.consoleLinks?.auth ? (
                  <a
                    className="inline-link"
                    href={selectedApp.consoleLinks.auth}
                    rel="noreferrer"
                    target="_blank"
                  >
                    应用权限页面
                  </a>
                ) : null}
                {selectedApp.consoleLinks?.auth && selectedApp.consoleLinks?.bot ? " · " : null}
                {selectedApp.consoleLinks?.bot ? (
                  <a
                    className="inline-link"
                    href={selectedApp.consoleLinks.bot}
                    rel="noreferrer"
                    target="_blank"
                  >
                    机器人后台
                  </a>
                ) : null}
                {!selectedApp.consoleLinks?.auth && !selectedApp.consoleLinks?.bot
                  ? "暂不可用"
                  : null}
              </dd>
            </div>
          </dl>
        </section>
        <section className="card">
          <h3>危险区</h3>
          {selectedApp.readOnly ? (
            <div className="notice-banner warn">当前机器人由运行环境提供，不能在这里删除。</div>
          ) : (
            <p className="card-sub">删除后将移除这个机器人，此操作不可恢复。</p>
          )}
          <div className="button-row">
            <button
              className="danger-button"
              type="button"
              disabled={Boolean(selectedApp.readOnly)}
              onClick={() => setDeleteTargetID(selectedApp.id)}
            >
              删除机器人
            </button>
          </div>
        </section>
      </>
    );
  }

  function renderPermissionCheckCard() {
    if (!selectedApp) {
      return null;
    }
    const disabled = Boolean(actionBusy);
    const autoConfigPlan =
      selectedAutoConfig.status === "ready" ? selectedAutoConfig.data.plan : null;
    const autoConfigStatus =
      selectedAutoConfig.status === "ready"
        ? selectedAutoConfig.resultStatus || selectedAutoConfig.data.plan.status
        : "";
    const canComplete =
      selectedAutoConfig.status === "error" ||
      autoConfigPlan?.status === "apply_required" ||
      autoConfigPlan?.status === "publish_required" ||
      autoConfigStatus === "verification_failed";
    const awaitingReview = autoConfigStatus === "awaiting_review";
    const checkedAt = autoConfigCheckedAt[selectedApp.id] || "";
    return (
      <section className="card">
        <div className="card-head">
          <div>
            <h3>权限检查</h3>
          </div>
          <button
            className="secondary-button"
            type="button"
            disabled={disabled}
            onClick={() => void checkRobotPermissions()}
          >
            检查权限
          </button>
        </div>
        {selectedPermissionCheck.status === "loading" ? (
          <div className="notice-banner warn">正在检查权限...</div>
        ) : null}
        {selectedPermissionCheck.status === "error" ? (
          <div className="detail-stack">
            <div className="notice-banner warn">{selectedPermissionCheck.message}</div>
            <div className="button-row">
              <button
                className="secondary-button"
                type="button"
                disabled={disabled}
                onClick={() => void checkRobotPermissions()}
              >
                重新检查
              </button>
            </div>
          </div>
        ) : null}
        {selectedPermissionCheck.status === "ready" ? (
          selectedPermissionCheck.data.ready ? (
            <div className="detail-stack">
              <div className="notice-banner good">权限已就绪</div>
              {selectedAutoConfig.status === "error" ? (
                <div className="notice-banner warn">{selectedAutoConfig.message}</div>
              ) : null}
              {canComplete ? (
                <>
                  <div className="notice-banner warn">
                    {selectedAutoConfig.status === "error"
                      ? "可以重新执行自动补齐后再检查。"
                      : describeAutoConfigSummary(autoConfigStatus || "apply_required")}
                  </div>
                  <div className="button-row">
                    <button
                      className="primary-button"
                      type="button"
                      disabled={disabled}
                      onClick={() => void completeRobotConfiguration()}
                    >
                      {actionBusy === "permission-complete" ? "补齐中..." : "自动补齐"}
                    </button>
                    <button
                      className="secondary-button"
                      type="button"
                      disabled={disabled}
                      onClick={() => void recheckRobotConfigurationPlan()}
                    >
                      {actionBusy === "permission-auto-config-check"
                        ? "检查中..."
                        : "重新检查配置"}
                    </button>
                  </div>
                </>
              ) : null}
              {awaitingReview ? (
                <div className="notice-banner warn">飞书正在审核发布</div>
              ) : null}
              {awaitingReview && !canComplete ? (
                <div className="button-row">
                  <button
                    className="secondary-button"
                    type="button"
                    disabled={disabled}
                    onClick={() => void recheckRobotConfigurationPlan()}
                  >
                    {actionBusy === "permission-auto-config-check"
                      ? "检查中..."
                      : "重新检查配置"}
                  </button>
                </div>
              ) : null}
              {checkedAt ? (
                <p className="support-copy">最近检查：{formatTimestamp(checkedAt)}</p>
              ) : null}
            </div>
          ) : (
            <div className="detail-stack">
              <div className="notice-banner warn">
                还缺少 {selectedPermissionCheck.data.missingScopes?.length || 0} 项权限
              </div>
              {selectedAutoConfig.status === "error" ? (
                <div className="notice-banner warn">{selectedAutoConfig.message}</div>
              ) : null}
              {selectedAutoConfig.status === "ready" && autoConfigStatus ? (
                <div className={`notice-banner ${autoConfigNoticeTone(autoConfigStatus)}`}>
                  {describeAutoConfigSummary(autoConfigStatus)}
                </div>
              ) : null}
              {awaitingReview ? (
                <div className="notice-banner warn">飞书正在审核发布</div>
              ) : null}
              <div className="req-group">
                {(selectedPermissionCheck.data.missingScopes || []).map((item) => (
                  <div
                    className="req-item"
                    key={`${item.scopeType || "tenant"}:${item.scope}`}
                  >
                    <span className="dot warn" />
                    <div>
                      <div className="label mono">{item.scope}</div>
                    </div>
                    {item.scopeType ? <span className="badge neutral">{item.scopeType}</span> : null}
                  </div>
                ))}
              </div>
              {selectedPermissionCheck.data.grantJSON ? (
                <>
                  <textarea
                    className="permission-json"
                    readOnly
                    value={selectedPermissionCheck.data.grantJSON}
                    aria-label="权限导入 JSON"
                  />
                  <p className="support-copy">
                    到飞书后台导入后，回到这里重新检查。
                  </p>
                  <div className="button-row">
                    {canComplete ? (
                      <button
                        className="primary-button"
                        type="button"
                        disabled={disabled}
                        onClick={() => void completeRobotConfiguration()}
                      >
                        {actionBusy === "permission-complete" ? "补齐中..." : "自动补齐"}
                      </button>
                    ) : null}
                    <button
                      className="secondary-button"
                      type="button"
                      disabled={disabled}
                      onClick={() => void recheckRobotConfigurationPlan()}
                    >
                      {actionBusy === "permission-auto-config-check"
                        ? "检查中..."
                        : "重新检查配置"}
                    </button>
                    <button
                      className="secondary-button"
                      type="button"
                      disabled={disabled}
                      onClick={() =>
                        void copyPermissionGrantJSON(selectedPermissionCheck.data)
                      }
                    >
                      复制导入 JSON
                    </button>
                  </div>
                </>
              ) : null}
              {checkedAt ? (
                <p className="support-copy">最近检查：{formatTimestamp(checkedAt)}</p>
              ) : null}
            </div>
          )
        ) : null}
      </section>
    );
  }

  function changeArea(area: AdminAreaID) {
    setActiveArea(area);
  }

  function renderAdminChrome(content: ReactNode, options?: { showToast?: boolean }) {
    return (
      <div className="admin-page">
        <aside className="sidebar">
          <BrandLockup subtitle="管理" />
          <nav className="side-nav" aria-label="管理导航">
            {adminAreas.map((area) => (
              <button
                key={area.id}
                className={activeArea === area.id ? "on" : ""}
                type="button"
                onClick={() => changeArea(area.id)}
              >
                {area.name}
              </button>
            ))}
          </nav>
        </aside>
        <header className="mobile-brand">
          <BrandLockup subtitle="管理" compact />
        </header>
        <main className="content">
          {options?.showToast !== false && detailNotice ? (
            <Toast tone={detailNotice.tone} message={detailNotice.message} />
          ) : null}
          {content}
        </main>
        <nav className="bottom-tabs" aria-label="管理导航">
          {adminAreas.map((area) => (
            <button
              key={area.id}
              className={activeArea === area.id ? "on" : ""}
              type="button"
              onClick={() => changeArea(area.id)}
            >
              {area.name}
            </button>
          ))}
        </nav>
      </div>
    );
  }

  function renderCurrentArea() {
    switch (activeArea) {
      case "bots":
        return renderBotsArea();
      case "backends":
        return renderBackendsArea();
      case "system":
        return renderSystemArea();
      default:
        return renderOverviewArea();
    }
  }

  function renderOverviewArea() {
    const connectedCount = apps.filter((app) => describeConnectionState(app) === "连接正常").length;
    const totalBytes =
      previewSummary.bytes +
      (imageStaging?.totalBytes || 0) +
      (logsStorage?.totalBytes || 0);
    const totalFiles =
      previewSummary.fileCount +
      (imageStaging?.fileCount || 0) +
      (logsStorage?.fileCount || 0);
    const todoItems = buildOverviewTodoItems();

    return (
      <>
        <h1 className="area-title">{versionTitle}</h1>
        <div className="stat-row">
          <article className="stat-card">
            <p>机器人</p>
            <strong>{apps.length}</strong>
            <span>
              <span className={`dot ${connectedCount === apps.length ? "good" : "warn"}`} />
              {connectedCount} 个连接正常
            </span>
          </article>
          <article className="stat-card">
            <p>存储占用</p>
            <strong>约 {formatBytes(totalBytes)}</strong>
            <span>{totalFiles} 个文件</span>
          </article>
          <article className="stat-card">
            <p>系统集成</p>
            <div className="status-stack">
              <span className="status-line">
                <span className={`dot ${autostart?.enabled ? "good" : "idle"}`} />
                自动运行{autostart?.enabled ? "已启用" : "未启用"}
              </span>
              <span className="status-line">
                <span className={`dot ${vscode && vscodeIsReady(vscode) ? "good" : "warn"}`} />
                VS Code{vscode && vscodeIsReady(vscode) ? "已接入" : "需要修复"}
              </span>
            </div>
          </article>
        </div>
        <section className="card">
          <h3>需要处理</h3>
          {todoItems.length === 0 ? (
            <div className="empty-state">一切正常，当前没有需要处理的事项。</div>
          ) : (
            <div className="req-group">
              {todoItems.map((item) => (
                <div className="req-item" key={item.id}>
                  <span className={`dot ${item.tone}`} />
                  <div className="label">{item.text}</div>
                  <button
                    className="ghost-button"
                    type="button"
                    onClick={() => {
                      if (item.robotID) {
                        setSelectedRobotID(item.robotID);
                      }
                      setActiveArea(item.area);
                    }}
                  >
                    前往处理
                  </button>
                </div>
              ))}
            </div>
          )}
        </section>
      </>
    );
  }

  function buildOverviewTodoItems(): OverviewTodoItem[] {
    const items: OverviewTodoItem[] = [];
    for (const app of apps) {
      if (app.runtimeApply?.pending) {
        items.push({
          id: `${app.id}:runtime`,
          text: `「${app.name || "未命名机器人"}」正在同步运行设置`,
          tone: "warn",
          area: "bots",
          robotID: app.id,
        });
      }
    }
    if (vscodeError || (vscode && !vscodeIsReady(vscode))) {
      items.push({
        id: "vscode",
        text: "VS Code 集成需要修复",
        tone: "warn",
        area: "system",
      });
    }
    if (!autostartError && autostart?.supported && !autostart.enabled) {
      items.push({
        id: "autostart",
        text: "自动运行未启用",
        tone: "idle",
        area: "system",
      });
    }
    const storageFileCount =
      previewSummary.fileCount +
      (imageStaging?.fileCount || 0) +
      (logsStorage?.fileCount || 0);
    if (storageFileCount > 0) {
      items.push({
        id: "storage",
        text: `存储可清理（${storageFileCount} 个文件）`,
        tone: "idle",
        area: "system",
      });
    }
    return items;
  }

  function renderBotsArea() {
    return (
      <>
        <h1 className="area-title">机器人</h1>
        <div className="split">
          <div className="list-pane">
            {apps.map((app) => (
              <button
                key={app.id}
                className={`list-row${selectedRobotID === app.id ? " on" : ""}`}
                type="button"
                onClick={() => {
                  setDetailNotice(null);
                  setSelectedRobotID(app.id);
                }}
              >
                <span className={`dot ${connectionTone(app)}`} />
                <span className="row-main">
                  <span className="row-title">{app.name || "未命名机器人"}</span>
                  <span className="row-sub">{app.appId || "未填写 App ID"}</span>
                </span>
              </button>
            ))}
            <button
              className={`list-row${selectedRobotID === newRobotID ? " on" : ""}`}
              type="button"
              onClick={() => {
                setDetailNotice(null);
                setSelectedRobotID(newRobotID);
              }}
            >
              <span className="row-main">
                <span className="row-title inline-link">添加机器人</span>
              </span>
            </button>
          </div>
          <div className="detail-pane">{renderRobotDetail()}</div>
        </div>
      </>
    );
  }

  function renderBackendsArea() {
    return (
      <>
        <h1 className="area-title">对话后端</h1>
        <div className="tab-bar">
          <button
            className={backendTab === "claude" ? "on" : ""}
            type="button"
            onClick={() => setBackendTab("claude")}
          >
            Claude
          </button>
          <button
            className={backendTab === "codex" ? "on" : ""}
            type="button"
            onClick={() => setBackendTab("codex")}
          >
            Codex
          </button>
        </div>
        {backendTab === "claude" ? (
          <ClaudeProfileSection
            loadError={claudeProfilesError}
            profiles={claudeProfiles}
            setProfiles={setClaudeProfiles}
            onReload={async () => {
              await loadAdminPage({ preferredRobotID: selectedRobotID });
            }}
          />
        ) : (
          <CodexProviderSection
            loadError={codexProvidersError}
            providers={codexProviders}
            setProviders={setCodexProviders}
            onReload={async () => {
              await loadAdminPage({ preferredRobotID: selectedRobotID });
            }}
          />
        )}
      </>
    );
  }

  function renderSystemArea() {
    return (
      <>
        <h1 className="area-title">系统</h1>
        <section className="card">
          <h3>自动运行</h3>
          <div className="status-line">
            <span className={`dot ${autostart?.enabled ? "good" : "idle"}`} />
            <span>{describeAutostart(autostart, autostartError)}</span>
          </div>
          {autostartError ? (
            <div className="notice-banner warn">{autostartError}</div>
          ) : null}
          {!autostartError && autostart?.supported && !autostart.enabled ? (
            <div className="button-row">
              <button
                className="primary-button"
                type="button"
                disabled={actionBusy === "autostart" || !autostart.canApply}
                onClick={() => void enableAutostart()}
              >
                启用自动运行
              </button>
            </div>
          ) : null}
        </section>
        <section className="card">
          <h3>VS Code 集成</h3>
          <div className="status-line">
            <span className={`dot ${vscode && vscodeIsReady(vscode) ? "good" : "warn"}`} />
            <span>{describeVSCode(vscode, vscodeError)}</span>
          </div>
          {vscodeError ? (
            <div className="notice-banner warn">{vscodeError}</div>
          ) : null}
          <div className="button-row">
            <button
              className="primary-button"
              type="button"
              disabled={actionBusy === "vscode"}
              onClick={() => void repairVSCode()}
            >
              重新检查并修复
            </button>
          </div>
        </section>
        <section className="card">
          <h3>存储维护</h3>
          {previewError ? <div className="notice-banner warn">{previewError}</div> : null}
          {imageStagingError ? (
            <div className="notice-banner warn">{imageStagingError}</div>
          ) : null}
          {logsStorageError ? (
            <div className="notice-banner warn">{logsStorageError}</div>
          ) : null}
          <div className="req-group">
            <StorageCleanupRow
              title="预览文件"
              summary={formatFileSummary(previewSummary.fileCount, previewSummary.bytes)}
              buttonLabel="清理旧预览"
              disabled={actionBusy === "cleanup-preview" || apps.length === 0}
              onClick={() => void cleanupPreviewDrive()}
            />
            <StorageCleanupRow
              title="图片暂存"
              summary={formatFileSummary(imageStaging?.fileCount || 0, imageStaging?.totalBytes || 0)}
              buttonLabel="清理旧图片"
              disabled={actionBusy === "cleanup-image"}
              onClick={() => void cleanupImageStaging()}
            />
            <StorageCleanupRow
              title="日志文件"
              summary={formatFileSummary(logsStorage?.fileCount || 0, logsStorage?.totalBytes || 0)}
              buttonLabel="清理一天前日志"
              disabled={actionBusy === "cleanup-logs"}
              onClick={() => void cleanupLogsStorage()}
            />
          </div>
        </section>
        <section className="card">
          <h3>会话信息</h3>
          <dl className="definition-list">
            <div>
              <dt>访问方式</dt>
              <dd>{describeAdminSession(bootstrap)}</dd>
            </div>
          </dl>
        </section>
      </>
    );
  }

  if (loading) {
    return renderAdminChrome(
      <section className="card">
        <div className="empty-state">
          <div className="loading-dot" />
          <span>正在读取最新状态</span>
        </div>
      </section>,
      { showToast: false },
    );
  }

  if (loadError) {
    return renderAdminChrome(
      <section className="card">
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
      </section>,
      { showToast: false },
    );
  }

  return renderAdminChrome(
    <>
      {renderCurrentArea()}
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
                className="secondary-button"
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
    </>,
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

function noticeFromCompleteView(view?: FeishuAppAutoConfigCompleteView): DetailNotice | null {
  if (!view) {
    return null;
  }
  if (view.result) {
    return {
      tone: autoConfigNoticeTone(view.result.status),
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

function connectionTone(app: FeishuAppSummary): "good" | "warn" | "danger" | "idle" {
  switch (app.status?.state) {
    case "connected":
      return "good";
    case "error":
      return "danger";
    case "disabled":
      return "idle";
    default:
      return "warn";
  }
}

function describeAdminSession(bootstrap: BootstrapState | null): string {
  const session = bootstrap?.session;
  if (!session) {
    return "暂不可用";
  }
  if (session.trustedLoopback) {
    return "本机直连";
  }
  if (!session.authenticated) {
    return "未认证";
  }
  if (session.expiresAt) {
    return `已认证会话，到期时间 ${formatTimestamp(session.expiresAt)}`;
  }
  return "已认证会话";
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

function StorageCleanupRow(props: {
  title: string;
  summary: string;
  buttonLabel: string;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <div className="req-item">
      <div>
        <div className="label">{props.title}</div>
        <div className="detail">{props.summary}</div>
      </div>
      <button
        className="secondary-button storage-row-action"
        type="button"
        disabled={props.disabled}
        onClick={props.onClick}
      >
        {props.buttonLabel}
      </button>
    </div>
  );
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
