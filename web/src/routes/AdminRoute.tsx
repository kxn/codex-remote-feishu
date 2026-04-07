import { useEffect, useMemo, useState } from "react";
import { formatError, requestJSON, requestJSONAllowHTTPError, requestVoid, sendJSON } from "../lib/api";
import type {
  AdminInstanceSummary,
  AdminInstancesResponse,
  BootstrapState,
  FeishuAppResponse,
  FeishuAppSummary,
  FeishuAppVerifyResponse,
  FeishuAppsResponse,
  FeishuManifestResponse,
  GatewayStatus,
  ImageStagingCleanupResponse,
  ImageStagingStatusResponse,
  PreviewDriveCleanupResponse,
  PreviewDriveReconcileResponse,
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
  blankToUndefined,
  emptyDraft,
  loadVSCodeState,
  syncDraftSelection,
  vscodeReadinessText,
} from "./admin/helpers";
import type { AppDraft, Notice, PreviewMap } from "./admin/types";
import { newAppID } from "./admin/types";

export function AdminRoute() {
  const [bootstrap, setBootstrap] = useState<BootstrapState | null>(null);
  const [runtime, setRuntime] = useState<RuntimeStatus | null>(null);
  const [apps, setApps] = useState<FeishuAppSummary[]>([]);
  const [manifest, setManifest] = useState<FeishuManifestResponse["manifest"] | null>(null);
  const [vscode, setVSCode] = useState<VSCodeDetectResponse | null>(null);
  const [vscodeError, setVSCodeError] = useState<string>("");
  const [instances, setInstances] = useState<AdminInstanceSummary[]>([]);
  const [imageStaging, setImageStaging] = useState<ImageStagingStatusResponse | null>(null);
  const [previews, setPreviews] = useState<PreviewMap>({});
  const [selectedAppID, setSelectedAppID] = useState<string>(newAppID);
  const [draft, setDraft] = useState<AppDraft>(emptyDraft());
  const [workspaceRoot, setWorkspaceRoot] = useState("");
  const [displayName, setDisplayName] = useState("");
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
  const setupURL = bootstrap?.admin.setupURL || "/setup";
  const setupURLForApp = (appID: string) => buildAppSetupURL(setupURL, appID);

  async function runAction(label: string, work: () => Promise<void>) {
    setBusyAction(label);
    setNotice(null);
    try {
      await work();
    } catch (err: unknown) {
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

  async function saveApp() {
    if (activeApp?.readOnly && !draft.isNew) {
      return;
    }
    await runAction(draft.isNew ? "create-app" : "save-app", async () => {
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
      setNotice({ tone: "good", message: draft.isNew ? "飞书机器人已创建。" : "飞书机器人配置已更新。" });
    });
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
        setNotice({ tone: "good", message: `连接测试成功，用时 ${(response.data.result.duration / 1_000_000_000).toFixed(1)}s。` });
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
    });
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
    });
  }

  async function createInstance() {
    await runAction("create-instance", async () => {
      await sendJSON<{ instance: AdminInstanceSummary }>("/api/admin/instances", "POST", {
        workspaceRoot,
        displayName: blankToUndefined(displayName),
      });
      setWorkspaceRoot("");
      setDisplayName("");
      await loadAdminData(activeApp?.id);
      setNotice({ tone: "good", message: "新的工作实例已启动。" });
    });
  }

  async function deleteInstance(instanceID: string, display: string) {
    if (!window.confirm(`删除实例 “${display}”？`)) {
      return;
    }
    await runAction("delete-instance", async () => {
      await requestVoid(`/api/admin/instances/${encodeURIComponent(instanceID)}`, { method: "DELETE" });
      await loadAdminData(activeApp?.id);
      setNotice({ tone: "warn", message: "实例已删除。" });
    });
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
      setNotice({ tone: "good", message: `${response.name || response.gatewayId} 预览文件已清理 ${response.result.deletedFileCount} 项。` });
      await loadAdminData(gatewayID);
    });
  }

  async function reconcilePreview(gatewayID: string) {
    await runAction(`reconcile-preview-${gatewayID}`, async () => {
      const response = await sendJSON<PreviewDriveReconcileResponse>(`/api/admin/storage/preview-drive/${encodeURIComponent(gatewayID)}/reconcile`, "POST");
      setNotice({
        tone: response.result.rootMissing || response.result.permissionDriftCount > 0 ? "warn" : "good",
        message: `${response.name || response.gatewayId} 预览目录检查完成：远端缺失 ${response.result.remoteMissingFileCount}，权限不一致 ${response.result.permissionDriftCount}。`,
      });
      await loadAdminData(gatewayID);
    });
  }

  async function applyVSCode(mode: string) {
    if (!vscode) {
      return;
    }
    await runAction(`vscode-${mode}`, async () => {
      const response = await sendJSON<VSCodeDetectResponse>("/api/admin/vscode/apply", "POST", { mode });
      setVSCode(response);
      setVSCodeError("");
      setNotice({ tone: "good", message: "VS Code 接入方式已更新。" });
    });
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
            setupURLForApp={setupURLForApp}
            onBeginNewApp={beginNewApp}
            onSelectApp={selectApp}
            onDraftChange={setDraft}
            onSaveApp={() => void saveApp()}
            onVerifyApp={() => void verifyApp()}
            onReconnectApp={() => void reconnectApp()}
            onToggleAppEnabled={(enabled) => void toggleAppEnabled(enabled)}
            onDeleteApp={() => void deleteApp()}
          />
          <AdminInstancesPanel
            workspaceRoot={workspaceRoot}
            displayName={displayName}
            instances={instances}
            busyAction={busyAction}
            onWorkspaceRootChange={setWorkspaceRoot}
            onDisplayNameChange={setDisplayName}
            onCreateInstance={() => void createInstance()}
            onDeleteInstance={(instanceID, display) => void deleteInstance(instanceID, display)}
          />
          <AdminStoragePanel
            apps={apps}
            imageStaging={imageStaging}
            previews={previews}
            busyAction={busyAction}
            onCleanupImageStaging={() => void cleanupImageStaging()}
            onCleanupPreview={(gatewayID) => void cleanupPreview(gatewayID)}
            onReconcilePreview={(gatewayID) => void reconcilePreview(gatewayID)}
          />
          <AdminVSCodePanel
            vscode={vscode}
            vscodeError={vscodeError}
            busyAction={busyAction}
            readinessText={vscodeReadinessText(vscode)}
            onApplyVSCode={(mode) => void applyVSCode(mode)}
            onReinstallShim={() => void reinstallShim()}
          />
          <AdminTechnicalPanel bootstrap={bootstrap} gatewayRows={gatewayRows} activeApp={activeApp} scopesJSON={scopesJSON} setupURL={setupURL} />
        </>
      ) : null}
    </ShellFrame>
  );
}

function buildAppSetupURL(baseURL: string, appID: string): string {
  const url = new URL(baseURL, window.location.origin);
  url.searchParams.set("app", appID);
  return url.toString();
}
