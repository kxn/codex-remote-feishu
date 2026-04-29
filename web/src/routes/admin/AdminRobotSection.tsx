import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import type { FeishuAppSummary } from "../../lib/types";
import { vscodeIsReady } from "../shared/helpers";
import { OnboardingFlowSurface } from "../shared/onboarding-flow";
import { ConnectStagePanel } from "../shared/onboarding-flow/connect-stage";
import type {
  NoticeTone,
  OnboardingFlowController,
} from "../shared/onboarding-flow/types";
import { useOnboardingFlowController } from "../shared/onboarding-flow/use-onboarding-flow-controller";
import {
  stageAllowsAction,
  isResolvedStageStatus,
} from "../shared/onboarding-flow/utils";

export type AdminRobotDetailNotice = {
  tone: NoticeTone;
  message: string;
};

type AdminRobotSectionProps = {
  apps: FeishuAppSummary[];
  selectedRobotID: string;
  newRobotID: string;
  detailNotice: AdminRobotDetailNotice | null;
  onSelectRobot: (robotID: string) => void;
  onDeleteRobotRequest: (robotID: string) => void;
  onConnectedApp: (robotID: string) => Promise<void>;
  onContextRefresh: (robotID?: string) => Promise<void>;
};

export function AdminRobotSection(props: AdminRobotSectionProps) {
  const { apps } = props;
  if (apps.length === 0) {
    return <AdminRobotSectionWithoutWorkflow {...props} />;
  }
  return <AdminRobotSectionWithWorkflow {...props} />;
}

function AdminRobotSectionWithoutWorkflow({
  detailNotice,
  newRobotID,
  onConnectedApp,
  onSelectRobot,
  selectedRobotID,
}: AdminRobotSectionProps) {
  return (
    <>
      <section className="panel">
        <div className="step-stage-head">
          <h2>机器人管理</h2>
          <p>查看所有机器人并处理需要关注的状态。</p>
        </div>
        <div className="robot-layout" style={{ marginTop: "1rem" }}>
          <div className="robot-list">
            <button
              className={`robot-list-button${selectedRobotID === newRobotID ? " active" : ""}`}
              type="button"
              onClick={() => onSelectRobot(newRobotID)}
            >
              <div className="robot-list-head">
                <strong>新增机器人</strong>
                <span className="robot-tag">新增</span>
              </div>
              <p>点击开始接入</p>
            </button>
          </div>
          <div>
            {detailNotice ? (
              <div className={`notice-banner ${detailNotice.tone}`}>
                {detailNotice.message}
              </div>
            ) : null}
            <OnboardingFlowSurface
              mode="admin"
              connectOnly
              connectOnlyTitle="新增机器人"
              connectOnlyDescription="选择扫码创建或手动输入，连接验证通过后会自动加入机器人列表。"
              onConnectedApp={onConnectedApp}
            />
          </div>
        </div>
      </section>

      <section className="panel">
        <div className="step-stage-head">
          <h2>系统集成</h2>
          <p>统一管理自动运行设置与 VS Code 集成。</p>
        </div>
        <div className="empty-state" style={{ marginTop: "1rem" }}>
          <strong>先接入一个机器人</strong>
          <p>接入完成后，这里会显示自动运行和 VS Code 集成状态。</p>
        </div>
      </section>
    </>
  );
}

function AdminRobotSectionWithWorkflow({
  apps,
  detailNotice,
  newRobotID,
  onConnectedApp,
  onContextRefresh,
  onDeleteRobotRequest,
  onSelectRobot,
  selectedRobotID,
}: AdminRobotSectionProps) {
  const selectedApp = useMemo(
    () => apps.find((app) => app.id === selectedRobotID) ?? null,
    [apps, selectedRobotID],
  );
  const workflowTargetID = selectedApp?.id || apps[0]?.id || "";
  const controller = useOnboardingFlowController({
    mode: "admin",
    preferredAppID: workflowTargetID,
    onContextRefresh,
  });
  const [showConnectionEditor, setShowConnectionEditor] = useState(false);

  useEffect(() => {
    setShowConnectionEditor(false);
  }, [selectedRobotID]);

  const controllerMatchesSelection = selectedApp?.id === controller.activeApp?.id;
  const showControllerNoticeInDetail = Boolean(
    selectedApp && controllerMatchesSelection && controller.notice,
  );
  const showControllerNoticeInIntegration = Boolean(
    controller.notice && !showControllerNoticeInDetail,
  );

  return (
    <>
      <section className="panel">
        <div className="step-stage-head">
          <h2>机器人管理</h2>
          <p>查看所有机器人并处理需要关注的状态。</p>
        </div>
        <div className="robot-layout" style={{ marginTop: "1rem" }}>
          <div className="robot-list">
            {apps.map((app) => (
              <button
                key={app.id}
                className={`robot-list-button${selectedRobotID === app.id ? " active" : ""}`}
                type="button"
                onClick={() => onSelectRobot(app.id)}
              >
                <div className="robot-list-head">
                  <strong>{app.name || "未命名机器人"}</strong>
                  {app.runtimeApply?.pending ? (
                    <span className="robot-tag warn">同步中</span>
                  ) : null}
                </div>
                <p>{app.appId || "未填写 App ID"}</p>
              </button>
            ))}
            <button
              className={`robot-list-button${selectedRobotID === newRobotID ? " active" : ""}`}
              type="button"
              onClick={() => onSelectRobot(newRobotID)}
            >
              <div className="robot-list-head">
                <strong>新增机器人</strong>
                <span className="robot-tag">新增</span>
              </div>
              <p>点击开始接入</p>
            </button>
          </div>

          {selectedApp ? (
            <ExistingRobotDetail
              app={selectedApp}
              controller={controller}
              controllerMatchesSelection={controllerMatchesSelection}
              detailNotice={detailNotice}
              onDeleteRobotRequest={onDeleteRobotRequest}
              showConnectionEditor={showConnectionEditor}
              setShowConnectionEditor={setShowConnectionEditor}
              showWorkflowNotice={showControllerNoticeInDetail}
            />
          ) : (
            <div>
              {detailNotice ? (
                <div className={`notice-banner ${detailNotice.tone}`}>
                  {detailNotice.message}
                </div>
              ) : null}
              <OnboardingFlowSurface
                mode="admin"
                connectOnly
                connectOnlyTitle="新增机器人"
                connectOnlyDescription="选择扫码创建或手动输入，连接验证通过后会自动加入机器人列表。"
                onConnectedApp={onConnectedApp}
              />
            </div>
          )}
        </div>
      </section>

      <section className="panel">
        <div className="step-stage-head">
          <h2>系统集成</h2>
          <p>统一管理自动运行设置与 VS Code 集成。</p>
        </div>
        <AdminSystemIntegrationPanel
          controller={controller}
          showWorkflowNotice={showControllerNoticeInIntegration}
        />
      </section>
    </>
  );
}

function ExistingRobotDetail({
  app,
  controller,
  controllerMatchesSelection,
  detailNotice,
  onDeleteRobotRequest,
  setShowConnectionEditor,
  showConnectionEditor,
  showWorkflowNotice,
}: {
  app: FeishuAppSummary;
  controller: OnboardingFlowController;
  controllerMatchesSelection: boolean;
  detailNotice: AdminRobotDetailNotice | null;
  onDeleteRobotRequest: (robotID: string) => void;
  setShowConnectionEditor: (value: boolean) => void;
  showConnectionEditor: boolean;
  showWorkflowNotice: boolean;
}) {
  const statusBanner = buildRobotStatusBanner(app, controller, controllerMatchesSelection);

  return (
    <section className="panel">
      <div className="step-stage-head">
        <h2>{app.name || "未命名机器人"}</h2>
        <p>机器人状态与当前处理项。</p>
      </div>
      <dl className="definition-list">
        <div>
          <dt>App ID</dt>
          <dd>{app.appId || "未填写"}</dd>
        </div>
        <div>
          <dt>连接状态</dt>
          <dd>{describeConnectionState(app)}</dd>
        </div>
        <div>
          <dt>启用状态</dt>
          <dd>{app.enabled ? "已启用" : "未启用"}</dd>
        </div>
        <div>
          <dt>最近验证</dt>
          <dd>{app.verifiedAt ? formatTimestamp(app.verifiedAt) : "暂未验证"}</dd>
        </div>
      </dl>

      {app.runtimeApply?.pending ? (
        <div className="notice-banner warn">
          当前机器人还在同步设置，请稍后刷新状态后再继续操作。
        </div>
      ) : null}
      {statusBanner ? (
        <div className={`notice-banner ${statusBanner.tone}`}>{statusBanner.message}</div>
      ) : null}
      {detailNotice ? (
        <div className={`notice-banner ${detailNotice.tone}`}>{detailNotice.message}</div>
      ) : null}
      {showWorkflowNotice && controller.notice ? (
        <div className={`notice-banner ${controller.notice.tone}`}>
          {controller.notice.message}
        </div>
      ) : null}

      <div className="button-row" style={{ marginTop: "1rem" }}>
        <button
          className="ghost-button"
          type="button"
          onClick={() => setShowConnectionEditor(!showConnectionEditor)}
        >
          {showConnectionEditor
            ? "收起连接信息"
            : app.readOnly
              ? "重新验证连接"
              : "修改连接信息"}
        </button>
        <button
          className="danger-button"
          type="button"
          disabled={Boolean(app.readOnly)}
          onClick={() => onDeleteRobotRequest(app.id)}
        >
          删除机器人
        </button>
      </div>
      {app.readOnly ? (
        <p className="support-copy">当前机器人由运行环境提供，不能在这里删除。</p>
      ) : null}

      {showConnectionEditor ? (
        <div className="admin-subpanel">
          <ConnectStagePanel controller={controller} />
        </div>
      ) : null}

      {!controllerMatchesSelection && controller.loading ? (
        <div className="empty-state" style={{ marginTop: "1rem" }}>
          <div className="loading-dot" />
          <span>正在读取当前机器人的待处理项</span>
        </div>
      ) : (
        <div className="todo-list">
          <PermissionCard controller={controller} visible={controllerMatchesSelection} />
          <InteractionTestCard controller={controller} visible={controllerMatchesSelection} />
        </div>
      )}
    </section>
  );
}

function PermissionCard({
  controller,
  visible,
}: {
  controller: OnboardingFlowController;
  visible: boolean;
}) {
  const stage = controller.permissionStage;
  if (!visible || !stage || stage.status === "complete") {
    return null;
  }

  return (
    <ActionCard
      title="权限检查"
      summary={stage.summary}
      tone={stage.status === "blocked" ? "danger" : "warn"}
      actions={
        <>
          <button
            className="secondary-button"
            type="button"
            disabled={
              !stageAllowsAction(stage, "recheck") ||
              controller.actionBusy === "permission-recheck"
            }
            onClick={() => void controller.recheckPermissionStage()}
          >
            {stage.status === "pending" ? "检查并继续" : "重新检查"}
          </button>
          {stageAllowsAction(stage, "force_skip") ? (
            <button
              className="ghost-button"
              type="button"
              disabled={controller.actionBusy === "permission-force-skip"}
              onClick={() => void controller.skipPermissionStage()}
            >
              强制跳过这一步
            </button>
          ) : null}
          {stageAllowsAction(stage, "open_auth") && controller.activeConsoleLinks?.auth ? (
            <a
              className="ghost-button"
              href={controller.activeConsoleLinks.auth}
              rel="noreferrer"
              target="_blank"
            >
              打开飞书后台权限配置
            </a>
          ) : null}
        </>
      }
    >
      <p className="support-copy">
        {stage.status === "deferred"
          ? "你已选择先跳过这一步，后续仍可回到这里重新检查。"
          : "如果当前企业权限暂时申请不到，你也可以先跳过这一步，后面再回来补齐。"}
      </p>
      {(stage.missingScopes || []).length > 0 ? (
        <div className="scope-list">
          {(stage.missingScopes || []).map((scope) => (
            <span
              key={`${scope.scopeType || "tenant"}-${scope.scope}`}
              className="scope-pill"
            >
              <code>{scope.scope}</code>
            </span>
          ))}
        </div>
      ) : null}
      {stage.grantJSON ? (
        <details className="compact-detail">
          <summary>查看一次性权限配置</summary>
          <div className="compact-detail-body">
            <textarea readOnly className="code-textarea" value={stage.grantJSON || ""} />
            <div className="button-row">
              <button
                className="ghost-button"
                type="button"
                onClick={() => void controller.copyGrantJSON(stage.grantJSON || "")}
              >
                复制配置
              </button>
            </div>
          </div>
        </details>
      ) : null}
    </ActionCard>
  );
}

function InteractionTestCard({
  controller,
  visible,
}: {
  controller: OnboardingFlowController;
  visible: boolean;
}) {
  if (!visible) {
    return null;
  }

  return (
    <ActionCard
      title="基础对话与交互"
      summary="需要时可直接发送测试提示，确认机器人会话和回调都能正常工作。"
      tone="warn"
      actions={
        <>
          <button
            className="secondary-button"
            type="button"
            disabled={controller.actionBusy === "test-events" || !controller.activeApp?.id}
            onClick={() =>
              controller.activeApp?.id &&
              void controller.startTest(controller.activeApp.id, "events")
            }
          >
            测试事件订阅
          </button>
          <button
            className="secondary-button"
            type="button"
            disabled={controller.actionBusy === "test-callback" || !controller.activeApp?.id}
            onClick={() =>
              controller.activeApp?.id &&
              void controller.startTest(controller.activeApp.id, "callback")
            }
          >
            测试回调
          </button>
          {controller.activeConsoleLinks?.callback ? (
            <a
              className="ghost-button"
              href={controller.activeConsoleLinks.callback}
              rel="noreferrer"
              target="_blank"
            >
              打开回调配置
            </a>
          ) : null}
        </>
      }
    >
      {controller.eventTest.status === "sent" ? (
        <div className="notice-banner good">
          {controller.eventTest.message || "事件订阅测试提示已发送。"}
        </div>
      ) : null}
      {controller.eventTest.status === "error" ? (
        <div className="notice-banner danger">{controller.eventTest.message}</div>
      ) : null}
      {controller.callbackTest.status === "sent" ? (
        <div className="notice-banner good">
          {controller.callbackTest.message || "回调测试卡片已发送。"}
        </div>
      ) : null}
      {controller.callbackTest.status === "error" ? (
        <div className="notice-banner danger">{controller.callbackTest.message}</div>
      ) : null}
      {controller.activeConsoleLinks?.events ? (
        <p className="support-copy">
          如需检查事件订阅配置，可前往{" "}
          <a
            className="inline-link"
            href={controller.activeConsoleLinks.events}
            rel="noreferrer"
            target="_blank"
          >
            飞书后台
          </a>
          。
        </p>
      ) : null}
    </ActionCard>
  );
}

function AdminSystemIntegrationPanel({
  controller,
  showWorkflowNotice,
}: {
  controller: OnboardingFlowController;
  showWorkflowNotice: boolean;
}) {
  if (controller.loading) {
    return (
      <div className="empty-state" style={{ marginTop: "1rem" }}>
        <div className="loading-dot" />
        <span>正在读取最新状态</span>
      </div>
    );
  }

  if (controller.loadError) {
    return (
      <div className="empty-state error" style={{ marginTop: "1rem" }}>
        <strong>当前还不能读取系统集成状态</strong>
        <p>{controller.loadError}</p>
        <div className="button-row">
          <button
            className="secondary-button"
            type="button"
            onClick={() => void controller.retryLoad()}
          >
            重新加载
          </button>
        </div>
      </div>
    );
  }

  const autostartStage = controller.workflow?.autostart;
  const vscodeStage = controller.workflow?.vscode;

  return (
    <div style={{ marginTop: "1rem" }}>
      {showWorkflowNotice && controller.notice ? (
        <div className={`notice-banner ${controller.notice.tone}`}>
          {controller.notice.message}
        </div>
      ) : null}
      <div className="soft-grid admin-soft-grid">
        <article className="soft-card-v2">
          <h4>自动运行设置</h4>
          <p>{autostartCardSummary(controller)}</p>
          {autostartStage?.error ? (
            <div className="notice-banner warn">{autostartStage.error}</div>
          ) : null}
          <div className="button-row">
            {stageAllowsAction(autostartStage, "apply") &&
            autostartStage?.autostart?.canApply ? (
              <button
                className="secondary-button"
                type="button"
                disabled={controller.actionBusy === "autostart-apply"}
                onClick={() => void controller.applyAutostart()}
              >
                启用自动运行
              </button>
            ) : null}
            {stageAllowsAction(autostartStage, "record_enabled") ? (
              <button
                className="ghost-button"
                type="button"
                disabled={controller.actionBusy === "autostart-enabled"}
                onClick={() =>
                  void controller.recordMachineDecision(
                    "autostart",
                    "enabled",
                    "已记录自动运行设置。",
                  )
                }
              >
                已启用，记为已完成
              </button>
            ) : null}
            {stageAllowsAction(autostartStage, "defer") ? (
              <button
                className="ghost-button"
                type="button"
                disabled={controller.actionBusy === "autostart-deferred"}
                onClick={() =>
                  void controller.recordMachineDecision(
                    "autostart",
                    "deferred",
                    "自动运行已留到稍后处理。",
                  )
                }
              >
                稍后处理
              </button>
            ) : null}
          </div>
        </article>

        <article className="soft-card-v2">
          <h4>VS Code 集成</h4>
          <p>{vscodeCardSummary(controller)}</p>
          {vscodeStage?.error ? (
            <div className="notice-banner warn">{vscodeStage.error}</div>
          ) : null}
          <div className="button-row">
            {stageAllowsAction(vscodeStage, "apply") ? (
              <button
                className="secondary-button"
                type="button"
                disabled={controller.actionBusy === "vscode-apply"}
                onClick={() => void controller.applyVSCode()}
              >
                确认集成
              </button>
            ) : null}
            {stageAllowsAction(vscodeStage, "record_managed_shim") ? (
              <button
                className="ghost-button"
                type="button"
                disabled={controller.actionBusy === "vscode-managed_shim"}
                onClick={() =>
                  void controller.recordMachineDecision(
                    "vscode",
                    "managed_shim",
                    "已记录 VS Code 集成结果。",
                  )
                }
              >
                已处理，记为已完成
              </button>
            ) : null}
            {stageAllowsAction(vscodeStage, "remote_only") ? (
              <button
                className="ghost-button"
                type="button"
                disabled={controller.actionBusy === "vscode-remote_only"}
                onClick={() =>
                  void controller.recordMachineDecision(
                    "vscode",
                    "remote_only",
                    "VS Code 集成已留到目标 SSH 机器处理。",
                  )
                }
              >
                去目标 SSH 机器处理
              </button>
            ) : null}
            {stageAllowsAction(vscodeStage, "defer") ? (
              <button
                className="ghost-button"
                type="button"
                disabled={controller.actionBusy === "vscode-deferred"}
                onClick={() =>
                  void controller.recordMachineDecision(
                    "vscode",
                    "deferred",
                    "VS Code 集成已留到稍后处理。",
                  )
                }
              >
                稍后处理
              </button>
            ) : null}
          </div>
        </article>

        <article className="soft-card-v2">
          <h4>状态提醒</h4>
          <p>{buildIntegrationHint(controller)}</p>
          <span className={`status-badge ${integrationTone(controller)}`}>
            {integrationStatusLabel(controller)}
          </span>
        </article>
      </div>
    </div>
  );
}

function ActionCard({
  actions,
  children,
  summary,
  title,
  tone,
}: {
  actions?: ReactNode;
  children?: ReactNode;
  summary: string;
  title: string;
  tone: "warn" | "danger";
}) {
  return (
    <article className={`todo-card ${tone}`}>
      <div className="todo-card-head">
        <div>
          <strong>{title}</strong>
          <p>{summary}</p>
        </div>
        <span className={`status-badge ${tone}`}>{tone === "danger" ? "需处理" : "待处理"}</span>
      </div>
      {children}
      {actions ? <div className="button-row">{actions}</div> : null}
    </article>
  );
}

function buildRobotStatusBanner(
  app: FeishuAppSummary,
  controller: OnboardingFlowController,
  controllerMatchesSelection: boolean,
): AdminRobotDetailNotice | null {
  if (app.runtimeApply?.pending) {
    return null;
  }
  if (!controllerMatchesSelection || controller.loading) {
    return null;
  }
  if (controller.loadError) {
    return {
      tone: "warn",
      message: "机器人详情暂时没有刷新成功，你可以稍后重试。",
    };
  }
  if (app.status?.state === "error") {
    return {
      tone: "danger",
      message: "当前连接状态需要处理，请检查连接信息后重新验证。",
    };
  }
  if (hasPendingRobotActions(controller)) {
    return {
      tone: "warn",
      message: "当前还有待处理项，完成后这里会恢复为正常状态。",
    };
  }
  return {
    tone: "good",
    message: "当前状态正常。",
  };
}

function hasPendingRobotActions(controller: OnboardingFlowController): boolean {
  const permissionStage = controller.permissionStage;
  return Boolean(permissionStage && !isResolvedStageStatus(permissionStage.status));
}

function buildIntegrationHint(controller: OnboardingFlowController): string {
  const autostartStage = controller.workflow?.autostart;
  const vscodeStage = controller.workflow?.vscode;
  if (autostartStage?.error) {
    return autostartStage.error;
  }
  if (vscodeStage?.error) {
    return vscodeStage.error;
  }
  if (autostartStage && !isResolvedStageStatus(autostartStage.status)) {
    return "检测到自动运行设置还没有完成，请先处理。";
  }
  if (vscodeStage && !isResolvedStageStatus(vscodeStage.status) && !vscodeIsReady(vscodeStage.vscode || null)) {
    return "检测到 VS Code 集成未完成，请先处理。";
  }
  return "当前没有需要处理的集成异常。";
}

function autostartCardSummary(controller: OnboardingFlowController): string {
  const stage = controller.workflow?.autostart;
  const autostart = stage?.autostart || null;
  if (!stage) {
    return "暂时没有拿到自动运行状态。";
  }
  if (stage.status === "deferred") {
    return "当前已留到稍后处理。";
  }
  if (stage.status === "not_applicable") {
    return "当前环境不需要处理。";
  }
  if (autostart?.enabled || stage.status === "complete") {
    return "当前已启用。";
  }
  return "当前未启用。";
}

function vscodeCardSummary(controller: OnboardingFlowController): string {
  const stage = controller.workflow?.vscode;
  const vscode = stage?.vscode || null;
  if (!stage) {
    return "暂时没有拿到 VS Code 集成状态。";
  }
  if (stage.decision?.value === "remote_only") {
    return "将到目标 SSH 机器处理。";
  }
  if (stage.status === "deferred") {
    return "当前已留到稍后处理。";
  }
  if (vscodeIsReady(vscode) || stage.status === "complete") {
    return "当前已接入。";
  }
  return "当前未完成接入。";
}

function integrationTone(controller: OnboardingFlowController): "good" | "warn" {
  return buildIntegrationHint(controller) === "当前没有需要处理的集成异常。"
    ? "good"
    : "warn";
}

function integrationStatusLabel(controller: OnboardingFlowController): string {
  return integrationTone(controller) === "good" ? "正常" : "需处理";
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

function formatTimestamp(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "暂不可用";
  }
  return date.toLocaleString();
}
