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
import { DefinitionList, ErrorState, LoadingState, Panel, ShellFrame, StatCard, StatGrid, StatusBadge } from "../components/ui";

const newAppID = "__new__";

type AppDraft = {
  isNew: boolean;
  id: string;
  name: string;
  appId: string;
  appSecret: string;
  enabled: boolean;
};

type Notice = {
  tone: "good" | "warn" | "danger";
  message: string;
};

type PreviewMap = Record<string, PreviewDriveStatusResponse>;

const emptyDraft = (): AppDraft => ({
  isNew: true,
  id: "",
  name: "",
  appId: "",
  appSecret: "",
  enabled: true,
});

export function AdminRoute() {
  const [bootstrap, setBootstrap] = useState<BootstrapState | null>(null);
  const [runtime, setRuntime] = useState<RuntimeStatus | null>(null);
  const [apps, setApps] = useState<FeishuAppSummary[]>([]);
  const [manifest, setManifest] = useState<FeishuManifestResponse["manifest"] | null>(null);
  const [vscode, setVSCode] = useState<VSCodeDetectResponse | null>(null);
  const [instances, setInstances] = useState<AdminInstanceSummary[]>([]);
  const [imageStaging, setImageStaging] = useState<ImageStagingStatusResponse | null>(null);
  const [previews, setPreviews] = useState<PreviewMap>({});
  const [selectedAppID, setSelectedAppID] = useState<string>(newAppID);
  const [draft, setDraft] = useState<AppDraft>(emptyDraft);
  const [workspaceRoot, setWorkspaceRoot] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [notice, setNotice] = useState<Notice | null>(null);
  const [error, setError] = useState<string>("");
  const [busyAction, setBusyAction] = useState("");

  async function loadAdminData(preferredAppID?: string) {
    const [bootstrapState, runtimeState, appList, manifestResponse, vscodeResponse, instancesResponse, imageStatus] = await Promise.all([
      requestJSON<BootstrapState>("/api/admin/bootstrap-state"),
      requestJSON<RuntimeStatus>("/api/admin/runtime-status"),
      requestJSON<FeishuAppsResponse>("/api/admin/feishu/apps"),
      requestJSON<FeishuManifestResponse>("/api/admin/feishu/manifest"),
      requestJSON<VSCodeDetectResponse>("/api/admin/vscode/detect"),
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
    setVSCode(vscodeResponse);
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
      setNotice({ tone: "good", message: draft.isNew ? "飞书 App 已创建。" : "飞书 App 配置已更新。" });
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
        setNotice({ tone: "good", message: `验证成功，用时 ${(response.data.result.duration / 1_000_000_000).toFixed(1)}s。` });
        return;
      }
      setNotice({
        tone: "danger",
        message: `验证失败：${response.data.result.errorCode || "verify_failed"} ${response.data.result.errorMessage || ""}`.trim(),
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
      setNotice({ tone: "good", message: "飞书 App 已请求热重连。" });
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
      setNotice({ tone: enabled ? "good" : "warn", message: enabled ? "飞书 App 已启用。" : "飞书 App 已停用。" });
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
      setNotice({ tone: "good", message: "飞书 App 已删除。" });
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
      setNotice({ tone: "good", message: "新的 managed headless instance 已启动。" });
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
        message: `${response.name || response.gatewayId} 对账完成：remote missing ${response.result.remoteMissingFileCount}，drift ${response.result.permissionDriftCount}。`,
      });
      await loadAdminData(gatewayID);
    });
  }

  async function applyVSCode(mode: string) {
    await runAction(`vscode-${mode}`, async () => {
      const response = await sendJSON<VSCodeDetectResponse>("/api/admin/vscode/apply", "POST", { mode });
      setVSCode(response);
      setNotice({ tone: "good", message: `VS Code 集成已切换到 ${mode}。` });
    });
  }

  async function reinstallShim() {
    await runAction("reinstall-shim", async () => {
      const response = await sendJSON<VSCodeDetectResponse>("/api/admin/vscode/reinstall-shim", "POST");
      setVSCode(response);
      setNotice({ tone: "good", message: "已重新安装 managed shim。" });
    });
  }

  const gatewayRows = useMemo(() => {
    const source = runtime?.gateways?.length ? runtime.gateways : bootstrap?.gateways ?? [];
    return source;
  }, [bootstrap?.gateways, runtime?.gateways]);

  return (
    <ShellFrame
      routeLabel="Local Admin"
      title="本地管理控制台"
      subtitle="这里集中放运行状态、多飞书 App、实例、存储和 VS Code 集成。管理页默认按 localhost 管理模型工作。"
      nav={[
        { label: "总览", href: "#overview" },
        { label: "飞书 App", href: "#feishu" },
        { label: "实例", href: "#instances" },
        { label: "存储", href: "#storage" },
        { label: "VS Code", href: "#vscode" },
      ]}
      actions={
        <button className="secondary-button" type="button" onClick={() => void loadAdminData(activeApp?.id)} disabled={busyAction !== ""}>
          立即刷新
        </button>
      }
    >
      {!bootstrap && !error ? <LoadingState title="正在加载管理页" description="读取 admin 概览、Feishu、实例、存储和 VS Code 状态。" /> : null}
      {error ? <ErrorState title="无法加载管理页状态" description="当前 admin shell 已接入，但页面数据读取失败。" detail={error} /> : null}
      {bootstrap && runtime && manifest && vscode && imageStaging ? (
        <>
          <Panel id="overview" title="运行总览" description="这部分用于确认 daemon、queue、surface 和 gateway 的整体健康状态。">
            <StatGrid>
              <StatCard label="Phase" value={bootstrap.phase} tone={bootstrap.phase === "ready" ? "accent" : "warn"} detail={bootstrap.setupRequired ? "setup 未完成" : "setup 已完成"} />
              <StatCard label="Instances" value={instances.length} detail={`${runtime.surfaces.length} surfaces`} />
              <StatCard label="Remote Queue" value={runtime.pendingRemoteTurns.length} detail={`${runtime.activeRemoteTurns.length} active turns`} />
              <StatCard label="Gateways" value={gatewayRows.length} detail={`${apps.length} configured apps`} />
            </StatGrid>
            <DefinitionList
              items={[
                { label: "Config Path", value: bootstrap.config.path },
                { label: "Admin URL", value: bootstrap.admin.url },
                { label: "Admin Listen", value: `${bootstrap.admin.listenHost}:${bootstrap.admin.listenPort}` },
                { label: "Relay Listen", value: `${bootstrap.relay.listenHost}:${bootstrap.relay.listenPort}` },
                { label: "Session Scope", value: bootstrap.session.scope || "unknown" },
                { label: "Session Access", value: bootstrap.session.trustedLoopback ? <StatusBadge value="trusted loopback" tone="good" /> : <StatusBadge value="session cookie" tone="neutral" /> },
              ]}
            />
            <div className="wizard-progress">
              {gatewayRows.map((gateway) => (
                <div key={gateway.gatewayId} className="wizard-step">
                  <StatusBadge value={gateway.state} tone={statusTone(gateway.state)} />
                  <div>
                    <strong>{gateway.name || gateway.gatewayId}</strong>
                    <p>{gateway.lastError || (gateway.lastConnectedAt ? `最近连接于 ${formatDateTime(gateway.lastConnectedAt)}` : "当前没有额外错误。")}</p>
                  </div>
                </div>
              ))}
            </div>
            {notice ? <div className={`notice-banner ${notice.tone}`}>{notice.message}</div> : null}
          </Panel>

          <Panel
            id="feishu"
            title="飞书 App 管理"
            description="这里沿用 setup 的多 App 模型，但更偏运行管理：编辑凭证、verify、reconnect、enable/disable 和查看 wizard 进度。"
            actions={
              <button className="secondary-button" type="button" onClick={beginNewApp} disabled={busyAction !== ""}>
                新建 App
              </button>
            }
          >
            <div className="setup-two-column">
              <div className="app-list-grid">
                {apps.map((app) => (
                  <button key={app.id} type="button" className={`app-card${selectedAppID === app.id ? " selected" : ""}`} onClick={() => selectApp(app)}>
                    <div className="app-card-head">
                      <strong>{app.name || app.id}</strong>
                      <StatusBadge value={app.status?.state || (app.enabled ? "configured" : "disabled")} tone={statusTone(app.status?.state)} />
                    </div>
                    <p>{app.id}</p>
                    <div className="app-card-flags">
                      <StatusBadge value={app.enabled ? "enabled" : "disabled"} tone={app.enabled ? "good" : "warn"} />
                      <StatusBadge value={app.wizard?.connectionVerifiedAt ? "verified" : "unverified"} tone={app.wizard?.connectionVerifiedAt ? "good" : "warn"} />
                    </div>
                  </button>
                ))}
                <button type="button" className={`app-card app-card-create${selectedAppID === newAppID ? " selected" : ""}`} onClick={beginNewApp}>
                  <strong>创建新 App</strong>
                  <p>继续向未来多机器人并行在线的模型扩展。</p>
                </button>
              </div>

              <div className="wizard-editor">
                <div className="form-grid">
                  <label className="field">
                    <span>Gateway ID</span>
                    <input value={draft.id} placeholder="main-bot" disabled={!draft.isNew} onChange={(event) => setDraft((current) => ({ ...current, id: event.target.value }))} />
                  </label>
                  <label className="field">
                    <span>显示名称</span>
                    <input value={draft.name} placeholder="Main Bot" onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} />
                  </label>
                  <label className="field">
                    <span>App ID</span>
                    <input value={draft.appId} placeholder="cli_xxx" onChange={(event) => setDraft((current) => ({ ...current, appId: event.target.value }))} />
                  </label>
                  <label className="field">
                    <span>App Secret</span>
                    <input type="password" value={draft.appSecret} placeholder={activeApp?.hasSecret ? "留空表示保留现有 secret" : "secret_xxx"} onChange={(event) => setDraft((current) => ({ ...current, appSecret: event.target.value }))} />
                  </label>
                </div>
                <label className="checkbox-row">
                  <input type="checkbox" checked={draft.enabled} onChange={(event) => setDraft((current) => ({ ...current, enabled: event.target.checked }))} />
                  <span>启用这个飞书 App</span>
                </label>
                <div className="button-row">
                  <button className="primary-button" type="button" onClick={() => void saveApp()} disabled={busyAction !== ""}>
                    {draft.isNew ? "创建 App" : "保存更改"}
                  </button>
                  <button className="secondary-button" type="button" onClick={() => void verifyApp()} disabled={!activeApp || busyAction !== ""}>
                    验证长连接
                  </button>
                  <button className="secondary-button" type="button" onClick={() => void reconnectApp()} disabled={!activeApp || busyAction !== ""}>
                    热重连
                  </button>
                  <button className="ghost-button" type="button" onClick={() => void toggleAppEnabled(!activeApp?.enabled)} disabled={!activeApp || activeApp.readOnly || busyAction !== ""}>
                    {activeApp?.enabled ? "停用" : "启用"}
                  </button>
                  <button className="danger-button" type="button" onClick={() => void deleteApp()} disabled={!activeApp || activeApp.readOnly || busyAction !== ""}>
                    删除 App
                  </button>
                </div>

                {activeApp ? (
                  <>
                    <div className="wizard-progress">
                      {buildWizardRows(activeApp).map((item) => (
                        <div key={item.label} className="wizard-step">
                          <StatusBadge value={item.done ? "done" : "pending"} tone={item.done ? "good" : "warn"} />
                          <div>
                            <strong>{item.label}</strong>
                            <p>{item.timestamp ? formatDateTime(item.timestamp) : "尚未记录"}</p>
                          </div>
                        </div>
                      ))}
                    </div>
                    {activeApp.readOnly ? <div className="notice-banner warn">{activeApp.readOnlyReason || "当前 App 由运行时环境变量接管，不能从管理页修改。"}</div> : null}
                  </>
                ) : null}

                <div className="manifest-block">
                  <h4>当前 Scopes JSON</h4>
                  <textarea className="code-textarea" readOnly value={scopesJSON} />
                </div>
              </div>
            </div>
          </Panel>

          <Panel id="instances" title="实例管理" description="这里不复用飞书 `/newinstance` 的交互语义，直接走后台 managed headless instance 模型。">
            <div className="form-grid">
              <label className="field">
                <span>Workspace Root</span>
                <input value={workspaceRoot} placeholder="/data/dl/project" onChange={(event) => setWorkspaceRoot(event.target.value)} />
              </label>
              <label className="field">
                <span>显示名称</span>
                <input value={displayName} placeholder="Alpha" onChange={(event) => setDisplayName(event.target.value)} />
              </label>
            </div>
            <div className="button-row">
              <button className="primary-button" type="button" onClick={() => void createInstance()} disabled={busyAction !== ""}>
                新建 Managed Instance
              </button>
            </div>
            <div className="app-list-grid">
              {instances.map((instance) => (
                <div key={instance.instanceId} className="app-card">
                  <div className="app-card-head">
                    <strong>{instance.displayName || instance.instanceId}</strong>
                    <StatusBadge value={instance.status} tone={instance.online ? "good" : instance.status === "error" ? "danger" : "neutral"} />
                  </div>
                  <p>{instance.workspaceRoot || "workspace unknown"}</p>
                  <div className="app-card-flags">
                    <StatusBadge value={instance.source || "unknown"} tone="neutral" />
                    {instance.pid ? <StatusBadge value={`pid ${instance.pid}`} tone="neutral" /> : null}
                    {instance.managed ? <StatusBadge value="managed" tone="good" /> : null}
                  </div>
                  {instance.lastError ? <p>{instance.lastError}</p> : null}
                  {instance.managed && instance.source === "headless" ? (
                    <div className="button-row">
                      <button className="danger-button" type="button" onClick={() => void deleteInstance(instance.instanceId, instance.displayName || instance.instanceId)} disabled={busyAction !== ""}>
                        删除实例
                      </button>
                    </div>
                  ) : null}
                </div>
              ))}
            </div>
          </Panel>

          <Panel id="storage" title="存储管理" description="图片暂存和 Markdown 预览飞书云盘分开管理；preview cleanup 会让旧消息里的预览链接失效。">
            <div className="checklist-grid">
              <div className="checklist-column">
                <div className="manifest-block">
                  <h4>图片暂存区</h4>
                  <DefinitionList
                    items={[
                      { label: "Root Dir", value: imageStaging.rootDir || "not configured" },
                      { label: "Files", value: imageStaging.fileCount },
                      { label: "Total", value: formatBytes(imageStaging.totalBytes) },
                      { label: "Active Files", value: imageStaging.activeFileCount },
                      { label: "Active Bytes", value: formatBytes(imageStaging.activeBytes) },
                    ]}
                  />
                  <div className="button-row">
                    <button className="secondary-button" type="button" onClick={() => void cleanupImageStaging()} disabled={busyAction !== ""}>
                      删除一天前未引用文件
                    </button>
                  </div>
                </div>
              </div>

              <div className="checklist-column">
                {apps.map((app) => {
                  const preview = previews[app.id];
                  return (
                    <div key={app.id} className="manifest-block">
                      <h4>{app.name || app.id} 预览云盘</h4>
                      {preview ? (
                        <>
                          <DefinitionList
                            items={[
                              { label: "Root URL", value: preview.summary.rootURL || "not created" },
                              { label: "Files", value: preview.summary.fileCount },
                              { label: "Scopes", value: preview.summary.scopeCount },
                              { label: "Estimated", value: formatBytes(preview.summary.estimatedBytes) },
                              { label: "Last Used", value: preview.summary.newestLastUsedAt ? formatDateTime(preview.summary.newestLastUsedAt) : "unknown" },
                            ]}
                          />
                          <div className="button-row">
                            <button className="secondary-button" type="button" onClick={() => void cleanupPreview(app.id)} disabled={busyAction !== ""}>
                              清理一天前预览
                            </button>
                            <button className="ghost-button" type="button" onClick={() => void reconcilePreview(app.id)} disabled={busyAction !== ""}>
                              对账 / Reconcile
                            </button>
                          </div>
                        </>
                      ) : (
                        <div className="inline-note">
                          <StatusBadge value="Unavailable" tone="warn" />
                          <span>当前没有拿到这个 App 的 preview drive 摘要。</span>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          </Panel>

          <Panel id="vscode" title="VS Code 集成" description="这里重点关注推荐模式、当前模式以及 shim 是否跟上最新扩展 bundle。">
            <StatGrid>
              <StatCard label="Recommended" value={vscode.recommendedMode} tone="accent" detail={vscode.sshSession ? "ssh session" : "local session"} />
              <StatCard label="Current Mode" value={vscode.currentMode || "unknown"} detail={vscode.currentBinary} />
              <StatCard label="Settings" value={vscode.settings.matchesBinary ? "ready" : "pending"} detail={vscode.settings.path} />
              <StatCard label="Managed Shim" value={vscode.latestShim.matchesBinary ? "ready" : "pending"} detail={vscode.latestBundleEntrypoint || "bundle not detected"} />
            </StatGrid>
            <div className="button-row">
              <button className="primary-button" type="button" onClick={() => void applyVSCode(vscode.recommendedMode)} disabled={busyAction !== ""}>
                应用推荐模式
              </button>
              <button className="secondary-button" type="button" onClick={() => void applyVSCode("editor_settings")} disabled={busyAction !== ""}>
                写入 settings.json
              </button>
              <button className="secondary-button" type="button" onClick={() => void applyVSCode("managed_shim")} disabled={busyAction !== ""}>
                安装 managed shim
              </button>
              <button className="ghost-button" type="button" onClick={() => void reinstallShim()} disabled={!vscode.needsShimReinstall || busyAction !== ""}>
                重新安装 shim
              </button>
            </div>
            <DefinitionList
              items={[
                { label: "Current Binary", value: vscode.currentBinary },
                { label: "Install State Path", value: vscode.installStatePath },
                { label: "Latest Bundle", value: vscode.latestBundleEntrypoint || "not detected" },
                { label: "Recorded Bundle", value: vscode.recordedBundleEntrypoint || "not recorded" },
                { label: "Needs Reinstall", value: vscode.needsShimReinstall ? "yes" : "no" },
                { label: "Readiness", value: vscodeReadinessText(vscode) },
              ]}
            />
          </Panel>
        </>
      ) : null}
    </ShellFrame>
  );
}

function appToDraft(app: FeishuAppSummary): AppDraft {
  return {
    isNew: false,
    id: app.id,
    name: app.name || "",
    appId: app.appId || "",
    appSecret: "",
    enabled: app.enabled,
  };
}

function syncDraftSelection(
  apps: FeishuAppSummary[],
  preferredID: string,
  setSelectedID: (value: string) => void,
  setDraft: (value: AppDraft) => void,
) {
  const preferredApp = apps.find((app) => app.id === preferredID);
  if (preferredApp) {
    setSelectedID(preferredApp.id);
    setDraft(appToDraft(preferredApp));
    return;
  }
  if (apps.length > 0) {
    setSelectedID(apps[0].id);
    setDraft(appToDraft(apps[0]));
    return;
  }
  setSelectedID(newAppID);
  setDraft(emptyDraft());
}

function blankToUndefined(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed ? trimmed : undefined;
}

function buildWizardRows(app: FeishuAppSummary) {
  return [
    { label: "凭证已保存", done: Boolean(app.wizard?.credentialsSavedAt), timestamp: app.wizard?.credentialsSavedAt },
    { label: "连接已验证", done: Boolean(app.wizard?.connectionVerifiedAt), timestamp: app.wizard?.connectionVerifiedAt },
    { label: "Scopes 已导出", done: Boolean(app.wizard?.scopesExportedAt), timestamp: app.wizard?.scopesExportedAt },
    { label: "事件已确认", done: Boolean(app.wizard?.eventsConfirmedAt), timestamp: app.wizard?.eventsConfirmedAt },
    { label: "回调已确认", done: Boolean(app.wizard?.callbacksConfirmedAt), timestamp: app.wizard?.callbacksConfirmedAt },
    { label: "菜单已确认", done: Boolean(app.wizard?.menusConfirmedAt), timestamp: app.wizard?.menusConfirmedAt },
    { label: "机器人已发布", done: Boolean(app.wizard?.publishedAt), timestamp: app.wizard?.publishedAt },
  ];
}

function statusTone(state?: string): "neutral" | "good" | "warn" | "danger" {
  switch (state) {
    case "connected":
      return "good";
    case "connecting":
    case "degraded":
      return "warn";
    case "auth_failed":
      return "danger";
    default:
      return "neutral";
  }
}

function formatDateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let current = value;
  let unitIndex = 0;
  while (current >= 1024 && unitIndex < units.length - 1) {
    current /= 1024;
    unitIndex += 1;
  }
  return `${current.toFixed(current >= 10 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
}

function vscodeIsReady(vscode: VSCodeDetectResponse | null): boolean {
  if (!vscode) {
    return false;
  }
  if (vscode.recommendedMode === "managed_shim") {
    return vscode.latestShim.matchesBinary;
  }
  return vscode.settings.matchesBinary;
}

function vscodeReadinessText(vscode: VSCodeDetectResponse | null): string {
  if (!vscode) {
    return "尚未检测";
  }
  if (vscodeIsReady(vscode)) {
    return "当前推荐模式已就绪。";
  }
  if (vscode.recommendedMode === "managed_shim" && !vscode.latestBundleEntrypoint) {
    return "还没有检测到可替换的 VS Code 扩展 bundle。";
  }
  if (vscode.needsShimReinstall) {
    return "检测到扩展已升级，建议重新安装 shim。";
  }
  return "当前模式还没有指向最新的 wrapper binary。";
}
