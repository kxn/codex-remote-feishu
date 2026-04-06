import type {
  AdminInstanceSummary,
  BootstrapState,
  FeishuAppSummary,
  GatewayStatus,
  ImageStagingStatusResponse,
  PreviewDriveStatusResponse,
  RuntimeStatus,
  VSCodeDetectResponse,
} from "../../lib/types";
import { DefinitionList, Panel, StatCard, StatGrid, StatusBadge } from "../../components/ui";
import { buildWizardRows, formatBytes, formatDateTime, statusTone } from "./helpers";
import type { AppDraft, Notice } from "./types";
import { newAppID } from "./types";

type AdminOverviewPanelProps = {
  bootstrap: BootstrapState;
  runtime: RuntimeStatus;
  apps: FeishuAppSummary[];
  instances: AdminInstanceSummary[];
  gatewayRows: GatewayStatus[];
  notice: Notice | null;
};

type AdminFeishuPanelProps = {
  apps: FeishuAppSummary[];
  selectedAppID: string;
  draft: AppDraft;
  activeApp: FeishuAppSummary | null;
  scopesJSON: string;
  busyAction: string;
  onBeginNewApp: () => void;
  onSelectApp: (app: FeishuAppSummary) => void;
  onDraftChange: React.Dispatch<React.SetStateAction<AppDraft>>;
  onSaveApp: () => void;
  onVerifyApp: () => void;
  onReconnectApp: () => void;
  onToggleAppEnabled: (enabled: boolean) => void;
  onDeleteApp: () => void;
};

type AdminInstancesPanelProps = {
  workspaceRoot: string;
  displayName: string;
  instances: AdminInstanceSummary[];
  busyAction: string;
  onWorkspaceRootChange: (value: string) => void;
  onDisplayNameChange: (value: string) => void;
  onCreateInstance: () => void;
  onDeleteInstance: (instanceID: string, display: string) => void;
};

type AdminStoragePanelProps = {
  apps: FeishuAppSummary[];
  imageStaging: ImageStagingStatusResponse;
  previews: Record<string, PreviewDriveStatusResponse>;
  busyAction: string;
  onCleanupImageStaging: () => void;
  onCleanupPreview: (gatewayID: string) => void;
  onReconcilePreview: (gatewayID: string) => void;
};

type AdminVSCodePanelProps = {
  vscode: VSCodeDetectResponse | null;
  vscodeError: string;
  busyAction: string;
  readinessText: string;
  onApplyVSCode: (mode: string) => void;
  onReinstallShim: () => void;
};

export function AdminOverviewPanel({ bootstrap, runtime, apps, instances, gatewayRows, notice }: AdminOverviewPanelProps) {
  return (
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
  );
}

export function AdminFeishuPanel({
  apps,
  selectedAppID,
  draft,
  activeApp,
  scopesJSON,
  busyAction,
  onBeginNewApp,
  onSelectApp,
  onDraftChange,
  onSaveApp,
  onVerifyApp,
  onReconnectApp,
  onToggleAppEnabled,
  onDeleteApp,
}: AdminFeishuPanelProps) {
  const readOnly = Boolean(activeApp?.readOnly && !draft.isNew);

  return (
    <Panel
      id="feishu"
      title="飞书 App 管理"
      description="这里沿用 setup 的多 App 模型，但更偏运行管理：编辑凭证、verify、reconnect、enable/disable 和查看 wizard 进度。"
      actions={
        <button className="secondary-button" type="button" onClick={onBeginNewApp} disabled={busyAction !== ""}>
          新建 App
        </button>
      }
    >
      <div className="setup-two-column">
        <div className="app-list-grid">
          {apps.map((app) => (
            <button key={app.id} type="button" className={`app-card${selectedAppID === app.id ? " selected" : ""}`} onClick={() => onSelectApp(app)}>
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
          <button type="button" className={`app-card app-card-create${selectedAppID === newAppID ? " selected" : ""}`} onClick={onBeginNewApp}>
            <strong>创建新 App</strong>
            <p>继续向未来多机器人并行在线的模型扩展。</p>
          </button>
        </div>

        <div className="wizard-editor">
          <div className="form-grid">
            <label className="field">
              <span>Gateway ID</span>
              <input value={draft.id} placeholder="main-bot" disabled={!draft.isNew || readOnly} onChange={(event) => onDraftChange((current) => ({ ...current, id: event.target.value }))} />
            </label>
            <label className="field">
              <span>显示名称</span>
              <input value={draft.name} placeholder="Main Bot" disabled={readOnly} onChange={(event) => onDraftChange((current) => ({ ...current, name: event.target.value }))} />
            </label>
            <label className="field">
              <span>App ID</span>
              <input value={draft.appId} placeholder="cli_xxx" disabled={readOnly} onChange={(event) => onDraftChange((current) => ({ ...current, appId: event.target.value }))} />
            </label>
            <label className="field">
              <span>App Secret</span>
              <input
                type="password"
                value={draft.appSecret}
                placeholder={activeApp?.hasSecret ? "留空表示保留现有 secret" : "secret_xxx"}
                disabled={readOnly}
                onChange={(event) => onDraftChange((current) => ({ ...current, appSecret: event.target.value }))}
              />
            </label>
          </div>
          <label className="checkbox-row">
            <input type="checkbox" checked={draft.enabled} disabled={readOnly} onChange={(event) => onDraftChange((current) => ({ ...current, enabled: event.target.checked }))} />
            <span>启用这个飞书 App</span>
          </label>
          <div className="button-row">
            <button className="primary-button" type="button" onClick={onSaveApp} disabled={busyAction !== "" || readOnly}>
              {draft.isNew ? "创建 App" : "保存更改"}
            </button>
            <button className="secondary-button" type="button" onClick={onVerifyApp} disabled={!activeApp || busyAction !== ""}>
              验证长连接
            </button>
            <button className="secondary-button" type="button" onClick={onReconnectApp} disabled={!activeApp || busyAction !== ""}>
              热重连
            </button>
            <button className="ghost-button" type="button" onClick={() => onToggleAppEnabled(!activeApp?.enabled)} disabled={!activeApp || activeApp.readOnly || busyAction !== ""}>
              {activeApp?.enabled ? "停用" : "启用"}
            </button>
            <button className="danger-button" type="button" onClick={onDeleteApp} disabled={!activeApp || activeApp.readOnly || busyAction !== ""}>
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
  );
}

export function AdminInstancesPanel({
  workspaceRoot,
  displayName,
  instances,
  busyAction,
  onWorkspaceRootChange,
  onDisplayNameChange,
  onCreateInstance,
  onDeleteInstance,
}: AdminInstancesPanelProps) {
  return (
    <Panel id="instances" title="实例管理" description="这里不复用飞书 `/newinstance` 的交互语义，直接走后台 managed headless instance 模型。">
      <div className="form-grid">
        <label className="field">
          <span>Workspace Root</span>
          <input value={workspaceRoot} placeholder="/data/dl/project" onChange={(event) => onWorkspaceRootChange(event.target.value)} />
        </label>
        <label className="field">
          <span>显示名称</span>
          <input value={displayName} placeholder="Alpha" onChange={(event) => onDisplayNameChange(event.target.value)} />
        </label>
      </div>
      <div className="button-row">
        <button className="primary-button" type="button" onClick={onCreateInstance} disabled={busyAction !== ""}>
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
                <button className="danger-button" type="button" onClick={() => onDeleteInstance(instance.instanceId, instance.displayName || instance.instanceId)} disabled={busyAction !== ""}>
                  删除实例
                </button>
              </div>
            ) : null}
          </div>
        ))}
      </div>
    </Panel>
  );
}

export function AdminStoragePanel({
  apps,
  imageStaging,
  previews,
  busyAction,
  onCleanupImageStaging,
  onCleanupPreview,
  onReconcilePreview,
}: AdminStoragePanelProps) {
  return (
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
              <button className="secondary-button" type="button" onClick={onCleanupImageStaging} disabled={busyAction !== ""}>
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
                      <button className="secondary-button" type="button" onClick={() => onCleanupPreview(app.id)} disabled={busyAction !== ""}>
                        清理一天前预览
                      </button>
                      <button className="ghost-button" type="button" onClick={() => onReconcilePreview(app.id)} disabled={busyAction !== ""}>
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
  );
}

export function AdminVSCodePanel({ vscode, vscodeError, busyAction, readinessText, onApplyVSCode, onReinstallShim }: AdminVSCodePanelProps) {
  return (
    <Panel id="vscode" title="VS Code 集成" description="这里重点关注推荐模式、当前模式以及 shim 是否跟上最新扩展 bundle。">
      {vscodeError ? <div className="notice-banner warn">VS Code 检测暂时不可用：{vscodeError}</div> : null}
      <StatGrid>
        <StatCard label="Recommended" value={vscode?.recommendedMode || "unavailable"} tone="accent" detail={vscode?.sshSession ? "ssh session" : "local session"} />
        <StatCard label="Current Mode" value={vscode?.currentMode || "unknown"} detail={vscode?.currentBinary || "unavailable"} />
        <StatCard label="Settings" value={vscode?.settings.matchesBinary ? "ready" : "pending"} detail={vscode?.settings.path || "unavailable"} />
        <StatCard label="Managed Shim" value={vscode?.latestShim.matchesBinary ? "ready" : "pending"} detail={vscode?.latestBundleEntrypoint || "bundle not detected"} />
      </StatGrid>
      <div className="button-row">
        <button className="primary-button" type="button" onClick={() => onApplyVSCode(vscode?.recommendedMode || "all")} disabled={!vscode || busyAction !== ""}>
          应用推荐模式
        </button>
        <button className="secondary-button" type="button" onClick={() => onApplyVSCode("editor_settings")} disabled={!vscode || busyAction !== ""}>
          写入 settings.json
        </button>
        <button className="secondary-button" type="button" onClick={() => onApplyVSCode("managed_shim")} disabled={!vscode || busyAction !== ""}>
          安装 managed shim
        </button>
        <button className="ghost-button" type="button" onClick={onReinstallShim} disabled={!vscode?.needsShimReinstall || busyAction !== ""}>
          重新安装 shim
        </button>
      </div>
      <DefinitionList
        items={[
          { label: "Current Binary", value: vscode?.currentBinary || "unavailable" },
          { label: "Install State Path", value: vscode?.installStatePath || "unavailable" },
          { label: "Latest Bundle", value: vscode?.latestBundleEntrypoint || "not detected" },
          { label: "Recorded Bundle", value: vscode?.recordedBundleEntrypoint || "not recorded" },
          { label: "Needs Reinstall", value: vscode?.needsShimReinstall ? "yes" : "no" },
          { label: "Readiness", value: readinessText },
        ]}
      />
    </Panel>
  );
}
