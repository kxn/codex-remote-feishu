import type {
  AdminInstanceSummary,
  BootstrapState,
  FeishuAppSummary,
  FeishuOnboardingCompleteResponse,
  FeishuOnboardingSession,
  GatewayStatus,
  ImageStagingStatusResponse,
  PreviewDriveStatusResponse,
  VSCodeDetectResponse,
} from "../../lib/types";
import { DefinitionList, Panel, StatCard, StatGrid, StatusBadge } from "../../components/ui";
import { FeishuAppFields } from "../shared/FeishuAppFields";
import { type VSCodeUsageScenario, vscodeHasDetectedBundle, vscodeIsReady } from "../shared/helpers";
import { FeishuConnectStep } from "../setup/FeishuConnectStep";
import type { FeishuConnectMode, FeishuConnectStage } from "../setup/types";
import {
  appConnectionLabel,
  appConnectionTone,
  appSetupProgress,
  buildWizardRows,
  emptyDraft,
  formatBytes,
  formatDateTime,
  instanceSourceLabel,
  instanceStatusLabel,
  instanceStatusTone,
  statusTone,
  vscodeModeLabel,
} from "./helpers";
import type { AppDraft, Notice, PreviewMap } from "./types";
import { newAppID } from "./types";

type AdminOverviewPanelProps = {
  bootstrap: BootstrapState;
  apps: FeishuAppSummary[];
  instances: AdminInstanceSummary[];
  imageStaging: ImageStagingStatusResponse;
  previews: PreviewMap;
  vscode: VSCodeDetectResponse | null;
  vscodeError: string;
  notice: Notice | null;
  setupURL: string;
  onInspectApp: (app: FeishuAppSummary) => void;
  setupURLForApp: (appID: string) => string;
};

type AdminFeishuPanelProps = {
  apps: FeishuAppSummary[];
  selectedAppID: string;
  draft: AppDraft;
  activeApp: FeishuAppSummary | null;
  busyAction: string;
  connectStage: FeishuConnectStage;
  connectMode: FeishuConnectMode | null;
  onboardingSession: FeishuOnboardingSession | null;
  onboardingCompletion: FeishuOnboardingCompleteResponse | null;
  onboardingNeedsManualRetry: boolean;
  setupURLForApp: (appID: string) => string;
  onBeginNewApp: () => void;
  onSelectApp: (app: FeishuAppSummary) => void;
  onDraftChange: React.Dispatch<React.SetStateAction<AppDraft>>;
  onConnectModeChange: (value: FeishuConnectMode) => void;
  onContinueModeSelection: () => void;
  onVerifyNewAppManual: () => void;
  onBackToModeSelection: () => void;
  onRefreshOnboarding: () => void;
  onRestartOnboarding: () => void;
  onSwitchToExistingFlow: () => void;
  onRetryOnboardingComplete: () => void;
  onContinueOnboardingNotice: () => void;
  onSaveApp: () => void;
  onVerifyApp: () => void;
  onReconnectApp: () => void;
  onRetryRuntimeApply: () => void;
  onToggleAppEnabled: (enabled: boolean) => void;
  onDeleteApp: () => void;
};

type AdminInstancesPanelProps = {
  instances: AdminInstanceSummary[];
};

type AdminStoragePanelProps = {
  apps: FeishuAppSummary[];
  imageStaging: ImageStagingStatusResponse;
  previews: Record<string, PreviewDriveStatusResponse>;
  busyAction: string;
  onCleanupImageStaging: () => void;
  onCleanupPreview: (gatewayID: string) => void;
};

type AdminVSCodePanelProps = {
  vscode: VSCodeDetectResponse | null;
  vscodeError: string;
  busyAction: string;
  readinessText: string;
  scenario: VSCodeUsageScenario | null;
  primaryActionLabel: string;
  canContinueVSCode: boolean;
  onScenarioChange: (value: VSCodeUsageScenario) => void;
  onContinueVSCode: () => void;
  onApplyVSCode: (mode: string) => void;
  onReinstallShim: () => void;
};

type AdminTechnicalPanelProps = {
  bootstrap: BootstrapState;
  gatewayRows: GatewayStatus[];
  activeApp: FeishuAppSummary | null;
  scopesJSON: string;
  setupURL: string;
};

type AttentionItem = {
  key: string;
  title: string;
  detail: string;
  tone: "warn" | "danger";
  actionLabel: string;
  href?: string;
  onAction?: () => void;
};

export function AdminOverviewPanel({
  bootstrap,
  apps,
  instances,
  imageStaging,
  previews,
  vscode,
  vscodeError,
  notice,
  setupURL,
  onInspectApp,
  setupURLForApp,
}: AdminOverviewPanelProps) {
  const enabledApps = apps.filter((app) => app.enabled).length;
  const connectedApps = apps.filter((app) => app.enabled && app.status?.state === "connected").length;
  const onlineInstances = instances.filter((instance) => instance.online).length;
  const previewFileCount = apps.reduce((sum, app) => sum + (previews[app.id]?.summary.fileCount ?? 0), 0);
  const staleImageCount = Math.max(imageStaging.fileCount - imageStaging.activeFileCount, 0);
  const attentionItems = buildAttentionItems(apps, staleImageCount, vscode, vscodeError, onInspectApp, setupURL, setupURLForApp);

  return (
    <Panel id="overview" title="总览" description="先看这里，确认现在是否有需要处理的事情。">
      <StatGrid>
        <StatCard label="机器人" value={apps.length} tone="accent" detail={`在线 ${connectedApps} / 已启用 ${enabledApps}`} />
        <StatCard label="需要处理" value={attentionItems.length} tone={attentionItems.length > 0 ? "warn" : "accent"} detail={attentionItems.length > 0 ? "建议先处理这些项目" : "当前没有明显待处理项"} />
        <StatCard label="工作实例" value={instances.length} detail={`在线 ${onlineInstances}`} />
        <StatCard label="文档与图片" value={imageStaging.fileCount + previewFileCount} detail={`图片 ${imageStaging.fileCount} · 预览 ${previewFileCount}`} />
      </StatGrid>

      {notice ? <div className={`notice-banner ${notice.tone}`}>{notice.message}</div> : null}

      <div className="section-block">
        <div className="section-heading">
          <div>
            <h4>需要关注</h4>
            <p>这里只列出建议优先处理的项目。</p>
          </div>
          <StatusBadge value={bootstrap.phase === "ready" ? "本机服务正常" : "本机服务未完成准备"} tone={bootstrap.phase === "ready" ? "good" : "warn"} />
        </div>

        {attentionItems.length > 0 ? (
          <div className="attention-list">
            {attentionItems.map((item) => (
              <div key={item.key} className={`attention-row ${item.tone}`}>
                <div className="attention-copy">
                  <strong>{item.title}</strong>
                  <p>{item.detail}</p>
                </div>
                <div className="attention-actions">
                  <StatusBadge value={item.tone === "danger" ? "优先处理" : "建议处理"} tone={item.tone} />
                  {item.href ? (
                    <a className="secondary-button" href={item.href}>
                      {item.actionLabel}
                    </a>
                  ) : (
                    <button className="secondary-button" type="button" onClick={item.onAction}>
                      {item.actionLabel}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="inline-note">
            <StatusBadge value="已就绪" tone="good" />
            <span>当前没有明显异常，可以直接去下面继续管理机器人、实例或文档预览。</span>
          </div>
        )}
      </div>
    </Panel>
  );
}

export function AdminFeishuPanel({
  apps,
  selectedAppID,
  draft,
  activeApp,
  busyAction,
  connectStage,
  connectMode,
  onboardingSession,
  onboardingCompletion,
  onboardingNeedsManualRetry,
  setupURLForApp,
  onBeginNewApp,
  onSelectApp,
  onDraftChange,
  onConnectModeChange,
  onContinueModeSelection,
  onVerifyNewAppManual,
  onBackToModeSelection,
  onRefreshOnboarding,
  onRestartOnboarding,
  onSwitchToExistingFlow,
  onRetryOnboardingComplete,
  onContinueOnboardingNotice,
  onSaveApp,
  onVerifyApp,
  onReconnectApp,
  onRetryRuntimeApply,
  onToggleAppEnabled,
  onDeleteApp,
}: AdminFeishuPanelProps) {
  const pendingRuntimeRemoval = Boolean(activeApp?.runtimeApply?.pending && activeApp.runtimeApply.action === "remove" && !activeApp.persisted);
  const readOnly = Boolean((activeApp?.readOnly || pendingRuntimeRemoval) && !draft.isNew);
  const setupProgress = activeApp ? appSetupProgress(activeApp) : null;
  const needsSetup = Boolean(activeApp && setupProgress && !setupProgress.complete);
  const canToggleEnabled = Boolean(activeApp && !activeApp.readOnly && !pendingRuntimeRemoval);
  const showNewAppWizard = draft.isNew && !activeApp;
  const wizardDraft = showNewAppWizard ? draft : emptyDraft();

  return (
    <Panel
      id="feishu"
      title="飞书机器人"
      description="新增、编辑、停用和重连机器人。首次接入的机器人也可以从这里继续配置。"
      actions={
        <button className="secondary-button" type="button" onClick={onBeginNewApp} disabled={busyAction !== ""}>
          新增机器人
        </button>
      }
    >
      <div className="setup-two-column">
        <div className="app-list-grid">
          {apps.map((app) => {
            const progress = appSetupProgress(app);
            return (
              <button key={app.id} type="button" className={`app-card${selectedAppID === app.id ? " selected" : ""}`} onClick={() => onSelectApp(app)}>
                <div className="app-card-head">
                  <strong>{app.name || app.appId || app.id}</strong>
                  <StatusBadge value={appConnectionLabel(app)} tone={appConnectionTone(app)} />
                </div>
                <p>{app.appId || "还没有填写 App ID"}</p>
                <div className="app-card-flags">
                  <StatusBadge value={app.enabled ? "已启用" : "已停用"} tone={app.enabled ? "good" : "neutral"} />
                  <StatusBadge value={progress.complete ? "已完成首次配置" : "需继续配置"} tone={progress.complete ? "good" : "warn"} />
                  {app.runtimeApply?.pending ? <StatusBadge value={app.runtimeApply.action === "remove" ? "待移除" : "待重试"} tone="warn" /> : null}
                  {app.readOnly ? <StatusBadge value="只读" tone="warn" /> : null}
                </div>
                <p>{buildAppCardDetail(app, progress.remaining)}</p>
              </button>
            );
          })}
          <button type="button" className={`app-card app-card-create${selectedAppID === newAppID ? " selected" : ""}`} onClick={onBeginNewApp}>
            <strong>新增机器人</strong>
            <p>添加一个新的飞书机器人，用于不同团队或不同入口。</p>
          </button>
        </div>

        <div className="wizard-editor">
          {activeApp ? (
            <StatGrid>
              <StatCard label="连接状态" value={appConnectionLabel(activeApp)} tone={appConnectionTone(activeApp) === "good" ? "accent" : "warn"} detail={activeApp.status?.lastConnectedAt ? `最近连接 ${formatDateTime(activeApp.status.lastConnectedAt)}` : "还没有连接记录"} />
              <StatCard label="最近验证" value={activeApp.verifiedAt ? formatDateTime(activeApp.verifiedAt) : "尚未验证"} detail={activeApp.wizard?.connectionVerifiedAt ? "已通过连接测试" : "建议先测试连接"} />
              <StatCard label="首次配置" value={setupProgress?.complete ? "已完成" : "未完成"} tone={setupProgress?.complete ? "accent" : "warn"} detail={setupProgress ? `已完成 ${setupProgress.completed} / ${setupProgress.total}` : "创建后会显示"} />
              <StatCard label="编辑权限" value={readOnly ? "只读" : "可编辑"} detail={pendingRuntimeRemoval ? "本地配置已删除，等待运行时移除" : readOnly ? "当前由启动参数接管" : "可在这里直接修改"} />
            </StatGrid>
          ) : showNewAppWizard ? (
            <div className="wizard-callout">
              <h4>先选择接入方式</h4>
              <p>新增机器人时，可以扫码新建飞书应用，也可以继续手动接入已有应用。</p>
            </div>
          ) : (
            <div className="wizard-callout">
              <h4>先把机器人接进来</h4>
              <p>这里先保存名称、App ID 和 App Secret。保存成功后，就能测试连接并进入首次配置流程。</p>
            </div>
          )}

          {activeApp?.readOnly ? <div className="notice-banner warn">{activeApp.readOnlyReason || "这个机器人由当前启动参数接管，只能查看状态，不能在管理页修改。"}</div> : null}
          {activeApp?.runtimeApply?.pending ? (
            <div className="notice-banner warn">
              {activeApp.runtimeApply.action === "remove" && !activeApp.persisted
                ? "这个机器人已经从本地配置删除，但运行时移除还没成功。请重试应用，直到它从列表里消失。"
                : "最近一次保存已经写入本地配置，但运行时应用失败。请重试应用，直到状态恢复。"}
              {activeApp.runtimeApply.error ? <div>最近错误：{activeApp.runtimeApply.error}</div> : null}
              <div className="button-row">
                <button className="secondary-button" type="button" onClick={onRetryRuntimeApply} disabled={busyAction !== ""}>
                  {activeApp.runtimeApply.action === "remove" ? "重试移除" : "重试应用"}
                </button>
              </div>
            </div>
          ) : null}
          {activeApp && needsSetup ? (
            <div className="notice-banner warn">
              这个机器人还没完成首次配置。完成后才能稳定接收消息、菜单和文档预览。
              <div className="button-row">
                <a className="secondary-button" href={setupURLForApp(activeApp.id)}>
                  继续完成首次配置
                </a>
              </div>
            </div>
          ) : null}
          {activeApp?.status?.lastError ? <div className="notice-banner danger">最近错误：{activeApp.status.lastError}</div> : null}

          {showNewAppWizard ? (
            <FeishuConnectStep
              surface="admin"
              apps={apps}
              activeApp={null}
              draft={wizardDraft}
              connectStage={connectStage}
              connectMode={connectMode}
              onboardingSession={onboardingSession}
              onboardingCompletion={onboardingCompletion}
              onboardingNeedsManualRetry={onboardingNeedsManualRetry}
              busyAction={busyAction}
              onNameChange={(value) => onDraftChange((current) => ({ ...current, name: value }))}
              onAppIDChange={(value) => onDraftChange((current) => ({ ...current, appId: value }))}
              onAppSecretChange={(value) => onDraftChange((current) => ({ ...current, appSecret: value }))}
              onConnectModeChange={onConnectModeChange}
              onContinueModeSelection={onContinueModeSelection}
              onVerifyManual={onVerifyNewAppManual}
              onBackToModeSelection={onBackToModeSelection}
              onRefreshOnboarding={onRefreshOnboarding}
              onRestartOnboarding={onRestartOnboarding}
              onSwitchToExistingFlow={onSwitchToExistingFlow}
              onRetryOnboardingComplete={onRetryOnboardingComplete}
              onContinueOnboardingNotice={onContinueOnboardingNotice}
            />
          ) : (
            <>
              <FeishuAppFields
                className="form-grid"
                values={draft}
                readOnly={readOnly}
                hasSecret={activeApp?.hasSecret}
                nameLabel="机器人名称"
                namePlaceholder="团队机器人"
                nameFieldClassName="field"
                appIDFieldClassName="field"
                appIDHintClassName="form-hint form-grid-span-2"
                secretFieldClassName="field form-grid-span-2"
                onNameChange={(value) => onDraftChange((current) => ({ ...current, name: value }))}
                onAppIDChange={(value) => onDraftChange((current) => ({ ...current, appId: value }))}
                onAppSecretChange={(value) => onDraftChange((current) => ({ ...current, appSecret: value }))}
              />

              <label className="checkbox-row">
                <input type="checkbox" checked={draft.enabled} disabled={readOnly} onChange={(event) => onDraftChange((current) => ({ ...current, enabled: event.target.checked }))} />
                <span>启用这个机器人</span>
              </label>

              <div className="button-row">
                <button className="primary-button" type="button" onClick={onSaveApp} disabled={busyAction !== "" || readOnly}>
                  {draft.isNew ? "保存机器人" : "保存更改"}
                </button>
                <button className="secondary-button" type="button" onClick={onVerifyApp} disabled={!activeApp || busyAction !== "" || pendingRuntimeRemoval}>
                  测试连接
                </button>
                <button className="secondary-button" type="button" onClick={onReconnectApp} disabled={!activeApp || busyAction !== "" || pendingRuntimeRemoval}>
                  重新连接
                </button>
                <button className="ghost-button" type="button" onClick={() => onToggleAppEnabled(!activeApp?.enabled)} disabled={!activeApp || !canToggleEnabled || busyAction !== ""}>
                  {activeApp?.enabled ? "停用机器人" : "启用机器人"}
                </button>
                <button className="danger-button" type="button" onClick={onDeleteApp} disabled={!activeApp || activeApp.readOnly || pendingRuntimeRemoval || busyAction !== ""}>
                  删除机器人
                </button>
              </div>
            </>
          )}

          {draft.isNew ? (
            <details className="wizard-tech-detail">
              <summary>高级选项</summary>
              <div className="detail-stack">
                <label className="field">
                  <span>内部标识（可选）</span>
                  <input value={draft.id} placeholder="main-bot" onChange={(event) => onDraftChange((current) => ({ ...current, id: event.target.value }))} />
                </label>
                <p className="form-hint">一般保持留空即可。只有需要稳定区分多个机器人时，再手动指定这个值。</p>
              </div>
            </details>
          ) : null}

          {activeApp ? (
            <details className="wizard-tech-detail">
              <summary>查看技术详情</summary>
              <div className="detail-stack">
                <DefinitionList
                  items={[
                    { label: "内部标识", value: activeApp.id },
                    { label: "是否已保存 Secret", value: activeApp.hasSecret ? "是" : "否" },
                    { label: "配置来源", value: describeAppStorage(activeApp) },
                    { label: "最近验证时间", value: activeApp.verifiedAt ? formatDateTime(activeApp.verifiedAt) : "尚未记录" },
                  ]}
                />
                <div className="wizard-progress">
                  {buildWizardRows(activeApp).map((item) => (
                    <div key={item.label} className="wizard-step">
                      <StatusBadge value={item.done ? "已完成" : "未完成"} tone={item.done ? "good" : "warn"} />
                      <div>
                        <strong>{item.label}</strong>
                        <p>{item.timestamp ? formatDateTime(item.timestamp) : "尚未记录"}</p>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </details>
          ) : null}
        </div>
      </div>
    </Panel>
  );
}

export function AdminInstancesPanel({
  instances,
}: AdminInstancesPanelProps) {
  return (
    <Panel id="instances" title="工作实例" description="查看本机当前可接入的工作实例。后台恢复实例由系统自动管理，不再在这里手工创建或删除。">
      <div className="manifest-block">
        <h4>当前策略</h4>
        <ul className="wizard-bullet-list">
          <li>这里只显示当前可见的 VS Code 工作实例。</li>
          <li>后台恢复流程会由系统按需自动复用或启动，不再单独暴露成管理页操作。</li>
          <li>如果这里为空，请先在 VS Code 里打开 Codex 会话。</li>
        </ul>
      </div>

      <div className="section-block">
        <div className="section-heading">
          <div>
            <h4>当前实例</h4>
            <p>这里只显示本机当前可见的 VS Code 实例，方便确认哪个在线、哪个可被飞书接管。</p>
          </div>
        </div>

        {instances.length > 0 ? (
          <div className="card-grid">
            {instances.map((instance) => (
              <article key={instance.instanceId} className="info-card">
                <div className="app-card-head">
                  <strong>{instance.displayName || instance.instanceId}</strong>
                  <StatusBadge value={instanceStatusLabel(instance)} tone={instanceStatusTone(instance)} />
                </div>
                <p>{instance.workspaceRoot || "当前没有工作目录信息"}</p>
                <div className="app-card-flags">
                  <StatusBadge value={instanceSourceLabel(instance)} tone="neutral" />
                  {instance.pid ? <StatusBadge value={`PID ${instance.pid}`} tone="neutral" /> : null}
                </div>
                <p>{buildInstanceDetail(instance)}</p>
              </article>
            ))}
          </div>
        ) : (
          <div className="inline-note">
            <StatusBadge value="暂无实例" tone="neutral" />
            <span>本机还没有可显示的 VS Code 实例。请先在 VS Code 里启动 Codex。</span>
          </div>
        )}
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
}: AdminStoragePanelProps) {
  return (
    <Panel id="storage" title="文档与图片" description="查看图片暂存和文档预览占用情况，并按需清理旧内容。">
      <div className="card-grid card-grid-two-column">
        <div className="manifest-block">
          <h4>图片暂存</h4>
          <DefinitionList
            items={[
              { label: "暂存文件", value: imageStaging.fileCount },
              { label: "占用空间", value: formatBytes(imageStaging.totalBytes) },
              { label: "仍在使用", value: imageStaging.activeFileCount },
              { label: "活跃占用", value: formatBytes(imageStaging.activeBytes) },
            ]}
          />
          <p className="form-hint">清理时会保留仍在发送流程中的图片，不会直接影响正在进行中的会话。</p>
          <div className="button-row">
            <button className="secondary-button" type="button" onClick={onCleanupImageStaging} disabled={busyAction !== ""}>
              清理旧图片
            </button>
          </div>
          <details className="wizard-tech-detail">
            <summary>查看技术详情</summary>
            <DefinitionList items={[{ label: "暂存目录", value: imageStaging.rootDir || "未配置" }]} />
          </details>
        </div>

        <div className="stack-list">
          <div className="section-heading">
            <div>
              <h4>文档预览</h4>
              <p>每个机器人会在自己的飞书云盘里维护固定的预览 inventory 根目录，清理只会作用于这个目录内的内容。</p>
            </div>
          </div>

          {apps.length > 0 ? (
            <div className="card-grid">
              {apps.map((app) => {
                const preview = previews[app.id];
                const summary = preview?.summary;
                return (
                  <article key={app.id} className="info-card">
                    <div className="app-card-head">
                      <strong>{app.name || app.appId || app.id}</strong>
                      <StatusBadge value={summary?.rootURL ? "已启用预览" : "尚未生成预览目录"} tone={summary?.rootURL ? "good" : "warn"} />
                    </div>
                    <p>{buildPreviewDetail(summary)}</p>
                    <div className="app-card-flags">
                      <StatusBadge value={`${summary?.fileCount ?? 0} 个文件`} tone="neutral" />
                      <StatusBadge value={formatBytes(summary?.estimatedBytes ?? 0)} tone="neutral" />
                    </div>
                    <div className="button-row">
                      <button className="secondary-button" type="button" onClick={() => onCleanupPreview(app.id)} disabled={!preview || busyAction !== ""}>
                        清理旧预览
                      </button>
                    </div>
                    <details className="wizard-tech-detail">
                      <summary>查看技术详情</summary>
                      {preview ? (
                        <div className="detail-stack">
                          <DefinitionList
                            items={[
                              { label: "预览目录链接", value: summary?.rootURL || "尚未创建" },
                              { label: "inventory 子目录", value: summary?.scopeCount ?? 0 },
                              { label: "最近使用", value: summary?.newestLastUsedAt ? formatDateTime(summary.newestLastUsedAt) : "尚未记录" },
                            ]}
                          />
                        </div>
                      ) : (
                        <div className="inline-note">
                          <StatusBadge value="未获取到状态" tone="warn" />
                          <span>当前还没有拿到这个机器人的预览目录摘要。</span>
                        </div>
                      )}
                    </details>
                  </article>
                );
              })}
            </div>
          ) : (
            <div className="inline-note">
              <StatusBadge value="暂无机器人" tone="neutral" />
              <span>先配置至少一个机器人，后续生成文档预览时才会出现对应的预览目录。</span>
            </div>
          )}
        </div>
      </div>
    </Panel>
  );
}

export function AdminVSCodePanel({
  vscode,
  vscodeError,
  busyAction,
  readinessText,
  scenario,
  primaryActionLabel,
  canContinueVSCode,
  onScenarioChange,
  onContinueVSCode,
  onApplyVSCode,
  onReinstallShim,
}: AdminVSCodePanelProps) {
  const ready = vscodeIsReady(vscode);
  const bundleDetected = vscodeHasDetectedBundle(vscode);
  const currentModeLabel = vscode?.latestShim.matchesBinary
    ? (vscode?.settings.matchesBinary ? "扩展入口（检测到旧 settings 残留）" : "扩展入口")
    : vscode?.settings.matchesBinary
      ? "旧版 settings 接入（待迁移）"
      : "尚未接入";
  const showPrimaryAction = Boolean(vscode?.sshSession || scenario);

  return (
    <Panel id="vscode" title="VS Code" description="当前策略只保留扩展入口接入，不再修改本机 settings.json。">
      {vscodeError ? <div className="notice-banner warn">当前还没拿到 VS Code 检测结果：{vscodeError}</div> : null}

      <StatGrid>
        <StatCard label="当前环境" value={vscode?.sshSession ? "远程 SSH 机器" : "本机"} tone="accent" detail={vscode?.sshSession ? "当前是被 VS Code Remote SSH 连接的机器" : "当前是本机桌面 VS Code 场景"} />
        <StatCard label="当前状态" value={ready ? "已接入" : "待处理"} tone={ready ? "accent" : "warn"} detail={readinessText} />
        <StatCard label="当前接入方式" value={currentModeLabel} detail={vscode?.currentMode ? `当前记录模式：${vscodeModeLabel(vscode.currentMode)}` : "尚未检测"} />
        <StatCard label="扩展更新" value={vscode?.needsShimReinstall ? "需要重装" : "已同步"} detail={vscode?.latestBundleEntrypoint ? "已检测到 VS Code 扩展安装" : "还没检测到可处理的扩展安装"} />
      </StatGrid>

      <div className="detail-stack">
        {vscode?.sshSession ? (
          <>
            <div className="manifest-block">
              <h4>检测到当前是远程 SSH 机器</h4>
              <p>你现在是在被 VS Code Remote SSH 连接的机器上处理 VS Code 接入。这个场景下，只需要处理这台机器上的扩展入口。</p>
            </div>
            <div className="manifest-block">
              <h4>推荐操作</h4>
              <p>我们会把这台远程机器上的 VS Code 扩展入口接到 codex-remote。这不会去写 host 机器的 settings.json。</p>
            </div>
            {!bundleDetected ? <div className="notice-banner warn">还没检测到这台远程机器上的 VS Code 扩展安装。请先在这台机器上打开一次 VS Code Remote 窗口，并确保 Codex 扩展已经安装。</div> : null}
          </>
        ) : vscode ? (
          <>
            <div className="manifest-block">
              <h4>你以后主要怎么使用 VS Code 里的 Codex？</h4>
              <p>先确认当前这台机器是否需要接入。只要这台机器要用 VS Code，就统一只处理扩展入口，不再写 settings.json。</p>
            </div>
            <div className="choice-card-list" role="radiogroup" aria-label="Admin VS Code 使用场景">
              <label className={`choice-card${scenario === "current_machine" ? " selected" : ""}`}>
                <input type="radio" name="admin-vscode-usage-scenario" checked={scenario === "current_machine"} onChange={() => onScenarioChange("current_machine")} />
                <div>
                  <strong>要在当前这台机器上使用</strong>
                  <p>无论是本地 VS Code，还是这台机器被 Remote SSH 连接，都统一只处理扩展入口。</p>
                </div>
              </label>
              <label className={`choice-card${scenario === "remote_only" ? " selected" : ""}`}>
                <input type="radio" name="admin-vscode-usage-scenario" checked={scenario === "remote_only"} onChange={() => onScenarioChange("remote_only")} />
                <div>
                  <strong>主要去别的 SSH 机器上使用</strong>
                  <p>当前机器先不做 VS Code 接入，避免 host 设置影响远端。</p>
                </div>
              </label>
            </div>
            {scenario === "current_machine" ? (
              <div className="manifest-block">
                <h4>当前策略：只处理扩展入口</h4>
                <p>这条路径不会写本机 settings.json，因此不会再把 host 机器上的客户端 override 带进远端会话。</p>
                <p>如果扩展升级导致入口失效，回来重新安装扩展入口即可。</p>
              </div>
            ) : null}
            {scenario === "remote_only" ? (
              <div className="manifest-block">
                <h4>当前这台机器先不用接入</h4>
                <p>如果你主要是在别的 SSH 机器上使用 VS Code Codex，真正需要安装和接入的是目标远程机器，而不是当前这台本机。</p>
              </div>
            ) : null}
            {scenario === "current_machine" && !bundleDetected ? <div className="notice-banner warn">还没检测到这台机器上的 VS Code 扩展安装。请先在这台机器上打开一次 VS Code，并确保 Codex 扩展已经安装。</div> : null}
          </>
        ) : null}
      </div>

      {showPrimaryAction ? (
        <div className="button-row">
          <button className="primary-button" type="button" onClick={onContinueVSCode} disabled={!vscode || !canContinueVSCode || busyAction !== ""}>
            {primaryActionLabel}
          </button>
        </div>
      ) : null}

      <DefinitionList
        items={[
          { label: "settings.json", value: vscode?.settings.matchesBinary ? "检测到旧版客户端 override，建议迁移" : "当前策略不依赖 settings.json" },
          { label: "扩展入口", value: vscode?.latestShim.matchesBinary ? "已指向当前 relay" : vscode?.latestBundleEntrypoint ? "还没有同步到当前 relay" : "未检测到可处理的扩展安装" },
          { label: "当前会话", value: vscode?.sshSession ? "远程 VS Code" : "本机 VS Code" },
          { label: "建议", value: readinessText },
        ]}
      />

      <details className="wizard-tech-detail">
        <summary>高级处理</summary>
        <div className="button-row">
          <button className="secondary-button" type="button" onClick={() => onApplyVSCode("managed_shim")} disabled={!vscode || busyAction !== ""}>
            只处理扩展入口
          </button>
          <button className="ghost-button" type="button" onClick={onReinstallShim} disabled={!vscode?.needsShimReinstall || busyAction !== ""}>
            重新安装扩展入口
          </button>
        </div>
        <DefinitionList
          items={[
            { label: "当前可执行文件", value: vscode?.currentBinary || "尚未检测" },
            { label: "安装状态文件", value: vscode?.installStatePath || "尚未检测" },
            { label: "settings.json 路径", value: vscode?.settings.path || "尚未检测" },
            { label: "记录中的扩展入口", value: vscode?.recordedBundleEntrypoint || "尚未记录" },
            { label: "最新检测到的扩展入口", value: vscode?.latestBundleEntrypoint || "尚未检测" },
          ]}
        />
      </details>
    </Panel>
  );
}

export function AdminTechnicalPanel({ bootstrap, gatewayRows, activeApp, scopesJSON, setupURL }: AdminTechnicalPanelProps) {
  return (
    <Panel id="technical" title="技术详情" description="默认操作不需要看这里。排障时再展开查看即可。">
      <div className="detail-stack">
        <details className="wizard-tech-detail">
          <summary>本机路径与地址</summary>
          <DefinitionList
            items={[
              { label: "配置文件", value: bootstrap.config.path },
              { label: "管理页地址", value: bootstrap.admin.url },
              { label: "配置页地址", value: setupURL },
              { label: "Relay 地址", value: bootstrap.relay.serverURL },
              { label: "本机访问方式", value: bootstrap.session.trustedLoopback ? "可信本机访问" : "需要会话登录" },
            ]}
          />
        </details>

        {activeApp ? (
          <details className="wizard-tech-detail">
            <summary>当前选中机器人详情</summary>
            <DefinitionList
              items={[
                { label: "内部标识", value: activeApp.id },
                { label: "App ID", value: activeApp.appId || "未配置" },
                { label: "状态来源", value: describeAppStorage(activeApp) },
                { label: "只读原因", value: activeApp.readOnlyReason || "无" },
              ]}
            />
          </details>
        ) : null}

        <details className="wizard-tech-detail">
          <summary>权限导入 JSON</summary>
          <textarea className="code-textarea" readOnly value={scopesJSON} />
        </details>

        <details className="wizard-tech-detail">
          <summary>机器人连接原始状态</summary>
          {gatewayRows.length > 0 ? (
            <div className="wizard-progress">
              {gatewayRows.map((gateway) => (
                <div key={gateway.gatewayId} className="wizard-step">
                  <StatusBadge value={gateway.state || "unknown"} tone={statusTone(gateway.state)} />
                  <div>
                    <strong>{gateway.name || gateway.gatewayId}</strong>
                    <p>{gateway.lastError || (gateway.lastConnectedAt ? `最近连接于 ${formatDateTime(gateway.lastConnectedAt)}` : "当前没有额外错误。")}</p>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="inline-note">
              <StatusBadge value="暂无原始状态" tone="neutral" />
              <span>当前没有可显示的机器人连接状态。</span>
            </div>
          )}
        </details>
      </div>
    </Panel>
  );
}

function buildAttentionItems(
  apps: FeishuAppSummary[],
  staleImageCount: number,
  vscode: VSCodeDetectResponse | null,
  vscodeError: string,
  onInspectApp: (app: FeishuAppSummary) => void,
  setupURL: string,
  setupURLForApp: (appID: string) => string,
): AttentionItem[] {
  const items: AttentionItem[] = [];

  if (apps.length === 0) {
    items.push({
      key: "no-apps",
      title: "还没有配置任何飞书机器人",
      detail: "先接入至少一个机器人，后续才能在飞书里附着实例、切换线程和发送文档预览。",
      tone: "warn",
      actionLabel: "打开配置页",
      href: setupURL,
    });
  }

  apps.forEach((app) => {
    const progress = appSetupProgress(app);
    if (app.enabled && app.status?.state === "auth_failed") {
      items.push({
        key: `app-auth-${app.id}`,
        title: `${app.name || app.appId || app.id} 连接失败`,
        detail: app.status.lastError || "请检查 App ID、App Secret，以及飞书平台上的机器人能力配置。",
        tone: "danger",
        actionLabel: "查看机器人",
        onAction: () => onInspectApp(app),
      });
      return;
    }
    if (app.enabled && !app.wizard?.connectionVerifiedAt) {
      items.push({
        key: `app-verify-${app.id}`,
        title: `${app.name || app.appId || app.id} 还没完成连接测试`,
        detail: "建议先测试连接，确认机器人已经可以和本机服务建立连接。",
        tone: "warn",
        actionLabel: "查看机器人",
        onAction: () => onInspectApp(app),
      });
      return;
    }
    if (app.enabled && !progress.complete) {
      items.push({
        key: `app-setup-${app.id}`,
        title: `${app.name || app.appId || app.id} 还没完成首次配置`,
        detail: `还差 ${progress.remaining} 步。建议继续完成权限、事件订阅、菜单和发布等设置。`,
        tone: "warn",
        actionLabel: "继续配置",
        href: setupURLForApp(app.id),
      });
      return;
    }
    if (app.enabled && app.status?.state === "degraded") {
      items.push({
        key: `app-degraded-${app.id}`,
        title: `${app.name || app.appId || app.id} 当前状态异常`,
        detail: app.status.lastError || "最近连接状态不稳定，建议查看机器人详情并尝试重新连接。",
        tone: "warn",
        actionLabel: "查看机器人",
        onAction: () => onInspectApp(app),
      });
    }
  });

  if (vscodeError) {
    items.push({
      key: "vscode-error",
      title: "VS Code 状态暂时不可用",
      detail: "当前还没拿到 VS Code 检测结果。如果你依赖 VS Code 共享实例，建议稍后检查一次。",
      tone: "warn",
      actionLabel: "查看 VS Code",
      href: "#vscode",
    });
  } else if (vscode && !vscodeIsReady(vscode)) {
    items.push({
      key: "vscode-ready",
      title: "VS Code 还没完全接入当前 relay",
      detail: "如果你需要和 VS Code 共用实例或线程，建议先选择使用场景，再完成当前机器上的接入。",
      tone: "warn",
      actionLabel: "查看 VS Code",
      href: "#vscode",
    });
  }

  if (staleImageCount > 0) {
    items.push({
      key: "stale-images",
      title: `有 ${staleImageCount} 个旧图片暂存文件可清理`,
      detail: "这些文件已经不在活跃发送流程里，可以按需释放本地存储空间。",
      tone: "warn",
      actionLabel: "查看文档与图片",
      href: "#storage",
    });
  }

  return items;
}

function buildAppCardDetail(app: FeishuAppSummary, remainingSetupSteps: number): string {
  if (app.runtimeApply?.pending && app.runtimeApply.error) {
    if (app.runtimeApply.action === "remove" && !app.persisted) {
      return `已从本地配置删除，运行时移除失败：${app.runtimeApply.error}`;
    }
    return `已保存到本地配置，但运行时应用失败：${app.runtimeApply.error}`;
  }
  if (app.status?.lastError) {
    return app.status.lastError;
  }
  if (!app.enabled) {
    return "当前已停用，不会继续接收飞书消息。";
  }
  if (!app.wizard?.connectionVerifiedAt) {
    return "还没有完成连接测试。";
  }
  if (remainingSetupSteps > 0) {
    return `首次配置还差 ${remainingSetupSteps} 步。`;
  }
  if (app.status?.lastConnectedAt) {
    return `最近连接于 ${formatDateTime(app.status.lastConnectedAt)}。`;
  }
  return "当前没有额外问题。";
}

function buildInstanceDetail(instance: AdminInstanceSummary): string {
  if (instance.lastError) {
    return instance.lastError;
  }
  if (instance.refreshInFlight) {
    if (instance.lastRefreshRequestedAt) {
      return `后台线程目录正在刷新，最近请求于 ${formatDateTime(instance.lastRefreshRequestedAt)}。`;
    }
    return "后台线程目录正在刷新。";
  }
  if (instance.lastRefreshCompletedAt) {
    return `最近完成线程目录刷新于 ${formatDateTime(instance.lastRefreshCompletedAt)}。`;
  }
  if (instance.idleSince) {
    return `最近空闲于 ${formatDateTime(instance.idleSince)}。`;
  }
  if (instance.lastHelloAt) {
    return `最近连回 relay 于 ${formatDateTime(instance.lastHelloAt)}。`;
  }
  if (instance.startedAt) {
    return `启动于 ${formatDateTime(instance.startedAt)}。`;
  }
  if (instance.requestedAt) {
    return `创建请求于 ${formatDateTime(instance.requestedAt)}。`;
  }
  return "当前没有额外状态信息。";
}

function buildPreviewDetail(summary: PreviewDriveStatusResponse["summary"] | undefined): string {
  if (!summary) {
    return "当前还没有拿到这个机器人的预览目录状态。";
  }
  if (!summary.rootURL) {
    return "这个机器人还没有生成过可打开的文档预览。";
  }
  if (summary.newestLastUsedAt) {
    return `固定 inventory 根目录最近使用于 ${formatDateTime(summary.newestLastUsedAt)}。`;
  }
  return "固定 inventory 根目录已建立，暂时还没有最近使用记录。";
}

function describeAppStorage(app: FeishuAppSummary): string {
  if (app.runtimeApply?.pending && app.runtimeApply.action === "remove" && !app.persisted) {
    return "本地已删除，待运行时移除";
  }
  if (app.runtimeOverride) {
    return "启动参数覆盖";
  }
  if (app.runtimeOnly) {
    return "仅运行时存在";
  }
  if (app.persisted) {
    return "本地配置";
  }
  return "未说明";
}
