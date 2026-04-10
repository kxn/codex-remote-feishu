import { useEffect, useMemo, useState } from "react";
import { APIRequestError, formatError, requestJSON, requestJSONAllowHTTPError, requestVoid, sendJSON } from "../lib/api";
import { relativeLocalPath } from "../lib/paths";
import type {
  AdminInstanceSummary,
  AdminInstancesResponse,
  BootstrapState,
  FeishuAppMutation,
  FeishuAppResponse,
  FeishuAppsResponse,
  FeishuAppSummary,
  FeishuAppVerifyResponse,
  FeishuManifestResponse,
  FeishuOnboardingCompleteResponse,
  FeishuOnboardingSession,
  FeishuOnboardingSessionResponse,
  FeishuRuntimeApplyFailureDetails,
  GatewayStatus,
  ImageStagingCleanupResponse,
  ImageStagingStatusResponse,
  PreviewDriveCleanupResponse,
  PreviewDriveStatusResponse,
  RuntimeStatus,
  VSCodeDetectResponse,
} from "../lib/types";
import { ErrorState, LoadingState, ShellFrame } from "../components/ui";
import {
  AdminFeishuPanel,
  AdminInstancesPanel,
  AdminOverviewPanel,
  AdminStoragePanel,
  AdminTechnicalPanel,
  AdminVSCodePanel,
} from "./admin/AdminPanels";
import {
  appToDraft,
  emptyDraft,
  syncDraftSelection,
  vscodeReadinessText,
} from "./admin/helpers";
import {
  blankToUndefined,
  buildAdminFeishuVerifySuccessMessage,
  loadVSCodeState,
  type VSCodeUsageScenario,
  vscodeApplyModeForScenario,
  vscodeHasDetectedBundle,
  vscodePrimaryActionLabel,
} from "./shared/helpers";
import type { AppDraft, Notice, PreviewMap } from "./admin/types";
import { newAppID } from "./admin/types";
import type { FeishuConnectMode, FeishuConnectStage } from "./setup/types";

export function AdminRoute() {
  const [bootstrap, setBootstrap] = useState<BootstrapState | null>(null);
  const [runtime, setRuntime] = useState<RuntimeStatus | null>(null);
  const [apps, setApps] = useState<FeishuAppSummary[]>([]);
  const [manifest, setManifest] = useState<FeishuManifestResponse["manifest"] | null>(null);
  const [vscode, setVSCode] = useState<VSCodeDetectResponse | null>(null);
  const [vscodeError, setVSCodeError] = useState<string>("");
  const [vscodeScenario, setVSCodeScenario] = useState<VSCodeUsageScenario | null>(null);
  const [instances, setInstances] = useState<AdminInstanceSummary[]>([]);
  const [imageStaging, setImageStaging] = useState<ImageStagingStatusResponse | null>(null);
  const [previews, setPreviews] = useState<PreviewMap>({});
  const [selectedAppID, setSelectedAppID] = useState<string>(newAppID);
  const [draft, setDraft] = useState<AppDraft>(emptyDraft());
  const [feishuConnectStage, setFeishuConnectStage] = useState<FeishuConnectStage>("mode_select");
  const [feishuConnectMode, setFeishuConnectMode] = useState<FeishuConnectMode | null>(null);
  const [onboardingSession, setOnboardingSession] = useState<FeishuOnboardingSession | null>(null);
  const [onboardingNeedsManualRetry, setOnboardingNeedsManualRetry] = useState(false);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [error, setError] = useState<string>("");
  const [busyAction, setBusyAction] = useState("");

  async function loadAdminData(preferredAppID?: string) {
    const [bootstrapState, runtimeState, appList, manifestResponse, vscodeState, instancesResponse, imageStatus] = await Promise.all([
      requestJSON<BootstrapState>("/api/admin/bootstrap-state"),
      requestJSON<RuntimeStatus>("/api/admin/runtime-status"),
      requestJSON<FeishuAppsResponse>("/api/admin/feishu/apps"),
      requestJSON<FeishuManifestResponse>("/api/admin/feishu/manifest"),
      loadVSCodeState("/api/admin/vscode/detect"),
      requestJSON<AdminInstancesResponse>("/api/admin/instances"),
      requestJSON<ImageStagingStatusResponse>("/api/admin/storage/image-staging"),
    ]);

    const previewResults = await Promise.allSettled(
      appList.apps.map((app) => requestJSON<PreviewDriveStatusResponse>(`/api/admin/storage/preview-drive/${encodeURIComponent(app.id)}`)),
    );
    const previewMap: PreviewMap = {};
    previewResults.forEach((result) => {
      if (result.status === "fulfilled") {
        previewMap[result.value.gatewayId] = result.value;
      }
    });

    setBootstrap(bootstrapState);
    setRuntime(runtimeState);
    setApps(appList.apps);
    setManifest(manifestResponse.manifest);
    setVSCode(vscodeState.data);
    setVSCodeError(vscodeState.error);
    setInstances(instancesResponse.instances);
    setImageStaging(imageStatus);
    setPreviews(previewMap);
    syncDraftSelection(appList.apps, preferredAppID ?? selectedAppID, setSelectedAppID, setDraft);
  }

  useEffect(() => {
    let cancelled = false;
    void loadAdminData()
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

  const activeApp = selectedAppID === newAppID ? null : apps.find((app) => app.id === selectedAppID) ?? null;
  const scopesJSON = useMemo(() => JSON.stringify(manifest?.scopesImport ?? { scopes: { tenant: [], user: [] } }, null, 2), [manifest]);
  const gatewayRows = useMemo(() => {
    const source = runtime?.gateways?.length ? runtime.gateways : bootstrap?.gateways ?? [];
    return source;
  }, [bootstrap?.gateways, runtime?.gateways]);
  const setupURL = relativeLocalPath(bootstrap?.admin.setupURL || "/setup");
  const setupURLForApp = (appID: string) => buildAppSetupURL(setupURL, appID);
  const vscodePrimaryLabel = vscodePrimaryActionLabel(vscode, vscodeScenario);
  const vscodeCanContinue = Boolean(vscode) && (vscode?.sshSession ? vscodeHasDetectedBundle(vscode) : vscodeScenario !== null && (vscodeScenario === "remote_only" || vscodeHasDetectedBundle(vscode)));

  useEffect(() => {
    if (vscode?.sshSession) {
      setVSCodeScenario(null);
    }
  }, [vscode?.sshSession]);

  useEffect(() => {
    resetFeishuConnectFlow(apps.length > 0 ? "existing" : "new");
  }, [selectedAppID, apps.length]);

  async function runAction(label: string, work: () => Promise<void>, onError?: (err: unknown) => Promise<boolean>) {
    setBusyAction(label);
    setNotice(null);
    try {
      await work();
    } catch (err: unknown) {
      if (onError && (await onError(err))) {
        return;
      }
      setNotice({ tone: "danger", message: formatError(err) });
    } finally {
      setBusyAction("");
    }
  }

  function selectApp(app: FeishuAppSummary) {
    setSelectedAppID(app.id);
    setDraft(appToDraft(app));
    setNotice(null);
  }

  function beginNewApp() {
    setSelectedAppID(newAppID);
    setDraft(emptyDraft());
    setNotice(null);
  }

  function resetFeishuConnectFlow(nextMode?: FeishuConnectMode) {
    setFeishuConnectStage("mode_select");
    setFeishuConnectMode(nextMode ?? (apps.length > 0 ? "existing" : "new"));
    setOnboardingSession(null);
    setOnboardingNeedsManualRetry(false);
  }

  async function handleFeishuRuntimeApplyFailure(err: unknown, fallbackAppID: string): Promise<boolean> {
    if (!(err instanceof APIRequestError) || err.code !== "gateway_apply_failed") {
      return false;
    }
    const details = parseFeishuRuntimeApplyFailureDetails(err.details);
    const preferredAppID = details?.app?.id || details?.gatewayId || fallbackAppID || newAppID;
    try {
      await loadAdminData(preferredAppID);
    } catch (reloadErr: unknown) {
      setNotice({ tone: "danger", message: formatError(reloadErr) });
      return true;
    }
    setNotice({
      tone: "warn",
      message:
        details?.app?.runtimeApply?.action === "remove" && !details.app.persisted
          ? "删除已保存，但运行时移除还没成功。页面已刷新为“待移除”状态，请重试移除。"
          : "更改已保存到本地配置，但运行时还没应用成功。页面已刷新为“未生效”状态，请重试应用。",
    });
    return true;
  }

  async function continueFeishuConnectModeSelection() {
    if (!feishuConnectMode) {
      setNotice({ tone: "danger", message: "请先选择你想怎么接入飞书应用。" });
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
      const response = await sendJSON<FeishuOnboardingSessionResponse>("/api/admin/feishu/onboarding/sessions", "POST");
      setFeishuConnectMode("new");
      setFeishuConnectStage("new_qr");
      setOnboardingSession(response.session);
      setOnboardingNeedsManualRetry(false);
    });
  }

  async function refreshFeishuOnboarding(options?: { silent?: boolean }) {
    if (!onboardingSession?.id) {
      return;
    }
    try {
      const response = await requestJSON<FeishuOnboardingSessionResponse>(`/api/admin/feishu/onboarding/sessions/${encodeURIComponent(onboardingSession.id)}`);
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
      setNotice({ tone: "danger", message: "当前还没有可用的扫码会话。请重新开始扫码。" });
      return;
    }
    await runAction("complete-feishu-onboarding", async () => {
      const response = await requestJSONAllowHTTPError<FeishuOnboardingCompleteResponse>(
        `/api/admin/feishu/onboarding/sessions/${encodeURIComponent(onboardingSession.id)}/complete`,
        { method: "POST" },
      );
      setOnboardingSession(response.data.session);
      await loadAdminData(response.data.app.id);
      if (!response.ok) {
        const detail = `${response.data.result.errorCode || "verify_failed"} ${response.data.result.errorMessage || ""}`.trim();
        setOnboardingNeedsManualRetry(true);
        setNotice({ tone: "danger", message: `扫码创建的飞书应用已经拿到凭据，但连接测试失败：${detail}`.trim() });
        return;
      }
      setOnboardingNeedsManualRetry(false);
      setNotice({
        tone: response.data.app.status?.state === "connected" ? "good" : "warn",
        message: buildAdminFeishuVerifySuccessMessage(response.data.app, response.data.result.duration),
      });
      resetFeishuConnectFlow("existing");
    });
  }

  async function saveAndVerifyNewApp() {
    if (draft.appId.trim() === "") {
      setNotice({ tone: "danger", message: "请先填写 App ID。" });
      return;
    }
    if (draft.appSecret.trim() === "") {
      setNotice({ tone: "danger", message: "请先填写 App Secret。" });
      return;
    }
    await runAction("create-app", async () => {
      const response = await sendJSON<FeishuAppResponse>("/api/admin/feishu/apps", "POST", {
        id: blankToUndefined(draft.id),
        name: blankToUndefined(draft.name),
        appId: blankToUndefined(draft.appId),
        appSecret: blankToUndefined(draft.appSecret),
        enabled: draft.enabled,
      });
      const verifyResponse = await requestJSONAllowHTTPError<FeishuAppVerifyResponse>(`/api/admin/feishu/apps/${encodeURIComponent(response.app.id)}/verify`, {
        method: "POST",
      });
      await loadAdminData(response.app.id);
      if (!verifyResponse.ok) {
        const detail = `${verifyResponse.data.result.errorCode || "verify_failed"} ${verifyResponse.data.result.errorMessage || ""}`.trim();
        setNotice({ tone: "danger", message: `飞书应用已保存，但连接测试失败：${detail}`.trim() });
        return;
      }
      resetFeishuConnectFlow("existing");
      setNotice({
        tone: verifyResponse.data.app.status?.state === "connected" ? "good" : "warn",
        message: buildAdminFeishuVerifySuccessMessage(verifyResponse.data.app, verifyResponse.data.result.duration),
      });
    });
  }

  async function saveApp() {
    if (activeApp?.readOnly && !draft.isNew) {
      return;
    }
    await runAction(
      draft.isNew ? "create-app" : "save-app",
      async () => {
        const payload = {
          id: draft.isNew ? blankToUndefined(draft.id) : undefined,
          name: blankToUndefined(draft.name),
          appId: blankToUndefined(draft.appId),
          appSecret: blankToUndefined(draft.appSecret),
          enabled: draft.enabled,
        };
        const path = draft.isNew ? "/api/admin/feishu/apps" : `/api/admin/feishu/apps/${encodeURIComponent(selectedAppID)}`;
        const method = draft.isNew ? "POST" : "PUT";
        const response = await sendJSON<FeishuAppResponse>(path, method, payload);
        await loadAdminData(response.app.id);
        setNotice({
          tone: feishuMutationTone(response.mutation),
          message: response.mutation?.message || (draft.isNew ? "飞书机器人已创建。" : "飞书机器人配置已更新。"),
        });
      },
      async (err) => handleFeishuRuntimeApplyFailure(err, draft.isNew ? blankToUndefined(draft.id) || selectedAppID : selectedAppID),
    );
  }

  async function verifyApp() {
    if (!activeApp) {
      return;
    }
    await runAction("verify-app", async () => {
      const response = await requestJSONAllowHTTPError<FeishuAppVerifyResponse>(`/api/admin/feishu/apps/${encodeURIComponent(activeApp.id)}/verify`, {
        method: "POST",
      });
      await loadAdminData(activeApp.id);
      if (response.ok) {
        setNotice({
          tone: response.data.app.status?.state === "connected" ? "good" : "warn",
          message: buildAdminFeishuVerifySuccessMessage(response.data.app, response.data.result.duration),
        });
        return;
      }
      setNotice({
        tone: "danger",
        message: `连接测试失败：${response.data.result.errorCode || "verify_failed"} ${response.data.result.errorMessage || ""}`.trim(),
      });
    });
  }

  async function reconnectApp() {
    if (!activeApp) {
      return;
    }
    await runAction("reconnect-app", async () => {
      await sendJSON<FeishuAppResponse>(`/api/admin/feishu/apps/${encodeURIComponent(activeApp.id)}/reconnect`, "POST");
      await loadAdminData(activeApp.id);
      setNotice({ tone: "good", message: "机器人已请求重新连接。" });
    });
  }

  async function toggleAppEnabled(enabled: boolean) {
    if (!activeApp) {
      return;
    }
    await runAction(enabled ? "enable-app" : "disable-app", async () => {
      const endpoint = enabled ? "enable" : "disable";
      await sendJSON<FeishuAppResponse>(`/api/admin/feishu/apps/${encodeURIComponent(activeApp.id)}/${endpoint}`, "POST");
      await loadAdminData(activeApp.id);
      setNotice({ tone: enabled ? "good" : "warn", message: enabled ? "机器人已启用。" : "机器人已停用。" });
    }, async (err) => handleFeishuRuntimeApplyFailure(err, activeApp.id));
  }

  async function deleteApp() {
    if (!activeApp) {
      return;
    }
    if (!window.confirm(`删除飞书 App “${activeApp.name || activeApp.id}”？`)) {
      return;
    }
    await runAction("delete-app", async () => {
      await requestVoid(`/api/admin/feishu/apps/${encodeURIComponent(activeApp.id)}`, { method: "DELETE" });
      await loadAdminData(newAppID);
      setNotice({ tone: "good", message: "机器人已删除。" });
    }, async (err) => handleFeishuRuntimeApplyFailure(err, activeApp.id));
  }

  async function retryRuntimeApply() {
    if (!activeApp?.runtimeApply?.pending) {
      return;
    }
    const pendingRemoval = activeApp.runtimeApply.action === "remove" && !activeApp.persisted;
    await runAction("retry-runtime-apply", async () => {
      await requestVoid(`/api/admin/feishu/apps/${encodeURIComponent(activeApp.id)}/retry-apply`, { method: "POST" });
      await loadAdminData(pendingRemoval ? newAppID : activeApp.id);
      setNotice({
        tone: "good",
        message: pendingRemoval ? "运行时移除已重试成功。" : "保存的运行时配置已重新应用。",
      });
    }, async (err) => handleFeishuRuntimeApplyFailure(err, activeApp.id));
  }

  async function cleanupImageStaging() {
    await runAction("cleanup-images", async () => {
      const response = await sendJSON<ImageStagingCleanupResponse>("/api/admin/storage/image-staging/cleanup", "POST", {
        olderThanHours: 24,
      });
      setImageStaging({
        rootDir: response.rootDir,
        fileCount: response.remainingFileCount,
        totalBytes: response.remainingBytes,
        activeFileCount: imageStaging?.activeFileCount ?? 0,
        activeBytes: imageStaging?.activeBytes ?? 0,
      });
      await loadAdminData(activeApp?.id);
      setNotice({ tone: "good", message: `图片暂存区已清理，删除 ${response.deletedFiles} 个文件。` });
    });
  }

  async function cleanupPreview(gatewayID: string) {
    await runAction(`cleanup-preview-${gatewayID}`, async () => {
      const response = await sendJSON<PreviewDriveCleanupResponse>(`/api/admin/storage/preview-drive/${encodeURIComponent(gatewayID)}/cleanup`, "POST", {
        olderThanHours: 24,
      });
      if (response.result.deletedFileCount > 0) {
        setNotice({ tone: "good", message: `${response.name || response.gatewayId} 预览文件已清理 ${response.result.deletedFileCount} 项。` });
      } else {
        setNotice({ tone: "warn", message: `${response.name || response.gatewayId} 本次未命中可清理的预览文件。` });
      }
      await loadAdminData(gatewayID);
    });
  }

  async function applyVSCode(mode: string, successMessage = "VS Code 接入方式已更新。") {
    if (!vscode) {
      return;
    }
    await runAction(`vscode-${mode}`, async () => {
      const response = await sendJSON<VSCodeDetectResponse>("/api/admin/vscode/apply", "POST", { mode });
      setVSCode(response);
      setVSCodeError("");
      setNotice({ tone: "good", message: successMessage });
    });
  }

  async function continueVSCode() {
    if (!vscode) {
      return;
    }
    if (vscode.sshSession) {
      if (!vscodeHasDetectedBundle(vscode)) {
        setNotice({ tone: "warn", message: "还没检测到这台远程机器上的 VS Code 扩展安装。请先在这台机器上打开一次 VS Code Remote 窗口，并确保 Codex 扩展已经安装。" });
        return;
      }
      await applyVSCode("managed_shim", "已接管这台远程机器上的 VS Code 扩展入口。以后如果扩展升级，回到管理页重新安装扩展入口即可。");
      return;
    }
    if (!vscodeScenario) {
      setNotice({ tone: "warn", message: "请先选择你以后主要怎么使用 VS Code 里的 Codex。" });
      return;
    }
    if (vscodeScenario === "remote_only") {
      setNotice({ tone: "warn", message: "当前机器先不做 VS Code 接入。等你在目标 SSH 机器上安装 codex-remote 后，再在那里完成 VS Code 接入即可。" });
      return;
    }
    const mode = vscodeApplyModeForScenario(vscode, vscodeScenario);
    if (!mode) {
      setNotice({ tone: "danger", message: "当前选择还不能映射到可执行的 VS Code 接入方式。" });
      return;
    }
    if (mode === "managed_shim" && !vscodeHasDetectedBundle(vscode)) {
      setNotice({ tone: "warn", message: "还没检测到这台机器上的 VS Code 扩展安装。请先在这台机器上打开一次 VS Code，并确保 Codex 扩展已经安装。" });
      return;
    }
    await applyVSCode("managed_shim", "已接管这台机器上的 VS Code 扩展入口。当前策略不会写本机 settings.json；如果扩展升级，回到管理页重新安装扩展入口即可。");
  }

  async function reinstallShim() {
    if (!vscode) {
      return;
    }
    await runAction("reinstall-shim", async () => {
      const response = await sendJSON<VSCodeDetectResponse>("/api/admin/vscode/reinstall-shim", "POST");
      setVSCode(response);
      setVSCodeError("");
      setNotice({ tone: "good", message: "已重新安装 VS Code 扩展入口。" });
    });
  }

  useEffect(() => {
    if (selectedAppID !== newAppID || feishuConnectStage !== "new_qr" || !onboardingSession?.id) {
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
    selectedAppID,
  ]);

  return (
    <ShellFrame
      routeLabel="Admin"
      title="本地管理页"
      subtitle="在这里管理飞书机器人、工作实例、文档预览和 VS Code 接入。"
      nav={[
        { label: "总览", href: "#overview" },
        { label: "飞书机器人", href: "#feishu" },
        { label: "工作实例", href: "#instances" },
        { label: "文档与图片", href: "#storage" },
        { label: "VS Code", href: "#vscode" },
        { label: "技术详情", href: "#technical" },
      ]}
      actions={
        <button className="secondary-button" type="button" onClick={() => void loadAdminData(activeApp?.id)} disabled={busyAction !== ""}>
          立即刷新
        </button>
      }
    >
      {!bootstrap && !error ? <LoadingState title="正在加载管理页" description="读取机器人、实例、文档预览和 VS Code 状态。" /> : null}
      {error ? <ErrorState title="无法加载管理页状态" description="页面已经打开，但后台状态读取失败。" detail={error} /> : null}
      {bootstrap && runtime && manifest && imageStaging ? (
        <>
          <AdminOverviewPanel
            bootstrap={bootstrap}
            apps={apps}
            instances={instances}
            imageStaging={imageStaging}
            previews={previews}
            vscode={vscode}
            vscodeError={vscodeError}
            notice={notice}
            setupURL={setupURL}
            setupURLForApp={setupURLForApp}
            onInspectApp={(app) => {
              selectApp(app);
              window.location.hash = "feishu";
            }}
          />
          <AdminFeishuPanel
            apps={apps}
            selectedAppID={selectedAppID}
            draft={draft}
            activeApp={activeApp}
            busyAction={busyAction}
            connectStage={feishuConnectStage}
            connectMode={feishuConnectMode}
            onboardingSession={onboardingSession}
            onboardingNeedsManualRetry={onboardingNeedsManualRetry}
            setupURLForApp={setupURLForApp}
            onBeginNewApp={beginNewApp}
            onSelectApp={selectApp}
            onDraftChange={setDraft}
            onConnectModeChange={setFeishuConnectMode}
            onContinueModeSelection={() => void continueFeishuConnectModeSelection()}
            onVerifyNewAppManual={() => void saveAndVerifyNewApp()}
            onBackToModeSelection={() => resetFeishuConnectFlow(feishuConnectMode ?? undefined)}
            onRefreshOnboarding={() => void refreshFeishuOnboarding()}
            onRestartOnboarding={() => void startFeishuOnboarding()}
            onSwitchToExistingFlow={() => {
              setFeishuConnectMode("existing");
              setFeishuConnectStage("existing_manual");
              setOnboardingSession(null);
              setOnboardingNeedsManualRetry(false);
            }}
            onRetryOnboardingComplete={() => void retryOnboardingComplete()}
            onSaveApp={() => void saveApp()}
            onVerifyApp={() => void verifyApp()}
            onReconnectApp={() => void reconnectApp()}
            onRetryRuntimeApply={() => void retryRuntimeApply()}
            onToggleAppEnabled={(enabled) => void toggleAppEnabled(enabled)}
            onDeleteApp={() => void deleteApp()}
          />
          <AdminInstancesPanel
            instances={instances}
          />
          <AdminStoragePanel
            apps={apps}
            imageStaging={imageStaging}
            previews={previews}
            busyAction={busyAction}
            onCleanupImageStaging={() => void cleanupImageStaging()}
            onCleanupPreview={(gatewayID) => void cleanupPreview(gatewayID)}
          />
          <AdminVSCodePanel
            vscode={vscode}
            vscodeError={vscodeError}
            busyAction={busyAction}
            readinessText={vscodeReadinessText(vscode)}
            scenario={vscodeScenario}
            primaryActionLabel={vscodePrimaryLabel}
            canContinueVSCode={vscodeCanContinue}
            onScenarioChange={setVSCodeScenario}
            onContinueVSCode={() => void continueVSCode()}
            onApplyVSCode={(mode) => void applyVSCode(mode)}
            onReinstallShim={() => void reinstallShim()}
          />
          <AdminTechnicalPanel bootstrap={bootstrap} gatewayRows={gatewayRows} activeApp={activeApp} scopesJSON={scopesJSON} setupURL={setupURL} />
        </>
      ) : null}
    </ShellFrame>
  );
}

function feishuMutationTone(mutation?: FeishuAppMutation): Notice["tone"] {
  switch (mutation?.kind) {
    case "identity_changed":
    case "credentials_changed":
      return "warn";
    default:
      return "good";
  }
}

function buildAppSetupURL(baseURL: string, appID: string): string {
  const url = new URL(baseURL, window.location.href);
  url.searchParams.set("app", appID);
  return relativeLocalPath(url.toString());
}

function parseFeishuRuntimeApplyFailureDetails(value: unknown): FeishuRuntimeApplyFailureDetails | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  return value as FeishuRuntimeApplyFailureDetails;
}
