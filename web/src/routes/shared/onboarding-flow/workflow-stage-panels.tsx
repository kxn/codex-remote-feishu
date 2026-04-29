import { vscodeIsReady } from "../helpers";
import type { OnboardingFlowController } from "./types";
import {
  isResolvedStageStatus,
  RequirementTable,
  stageAllowsAction,
  workflowStageLabel,
} from "./utils";

export function OnboardingStageRail({
  controller,
}: {
  controller: OnboardingFlowController;
}) {
  return (
    <aside className="panel step-rail">
      <div className="step-stage-head">
        <h2>设置步骤</h2>
        <p>按顺序完成当前安装。</p>
      </div>
      <div className="step-list">
        {controller.displayStages.map((stage) => (
          <button
            key={stage.id}
            className={`step-item${stage.id === controller.stageID ? " active" : ""}${
              isResolvedStageStatus(stage.status) ? " done" : ""
            }`}
            type="button"
            onClick={() => controller.setVisibleStageID(stage.id)}
          >
            <strong>{stage.title}</strong>
            <span>{workflowStageLabel(stage, controller.currentStageID)}</span>
          </button>
        ))}
      </div>
    </aside>
  );
}

export function OnboardingWorkflowOverview({
  controller,
}: {
  controller: OnboardingFlowController;
}) {
  const remainingActions = [
    ...(controller.mode === "setup"
      ? [controller.eventsStage, controller.callbackStage, controller.menuStage]
          .filter((stage) => stage?.status === "pending")
          .map((stage) => `继续处理${stage?.title || ""}。`)
      : []),
    ...(controller.workflow?.guide?.remainingManualActions || []),
  ];
  const showCompleteButton =
    controller.mode === "setup" &&
    controller.workflow?.completion.canComplete &&
    controller.stageID !== "done" &&
    controller.stageID !== "permission";

  return (
    <div className="detail-stack">
      <div
        className={`notice-banner ${
          controller.workflow?.completion.canComplete ? "good" : "warn"
        }`}
      >
        {controller.workflow?.completion.summary}
      </div>
      {controller.workflow?.guide?.autoConfiguredSummary ? (
        <p className="support-copy">{controller.workflow.guide.autoConfiguredSummary}</p>
      ) : null}
      {controller.currentStage?.title ? (
        <p className="support-copy">当前推荐处理：{controller.currentStage.title}</p>
      ) : null}
      {!controller.workflow?.completion.canComplete &&
      controller.workflow?.completion.blockingReason ? (
        <div className="notice-banner warn">
          {controller.workflow.completion.blockingReason}
        </div>
      ) : null}
      {remainingActions.length > 0 ? (
        <div className="panel">
          <div className="section-heading">
            <div>
              <h4>剩余建议项</h4>
              <p>这些项目不会都阻塞完成，但建议按顺序继续处理。</p>
            </div>
          </div>
          <ul className="ordered-checklist">
            {remainingActions.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </div>
      ) : null}
      {showCompleteButton ? (
        <div className="button-row">
          <button
            className="primary-button"
            type="button"
            disabled={controller.actionBusy === "complete-setup"}
            onClick={() => void controller.completeSetup()}
          >
            完成设置并进入管理页面
          </button>
        </div>
      ) : null}
    </div>
  );
}

export function OnboardingCurrentStagePanel({
  controller,
}: {
  controller: OnboardingFlowController;
}) {
  switch (controller.stageID) {
    case "runtime_requirements":
      return <EnvironmentStagePanel controller={controller} />;
    case "permission":
      return <PermissionStagePanel controller={controller} />;
    case "events":
      return <EventsStagePanel controller={controller} />;
    case "callback":
      return <CallbackStagePanel controller={controller} />;
    case "menu":
      return <MenuStagePanel controller={controller} />;
    case "autostart":
      return <AutostartStagePanel controller={controller} />;
    case "vscode":
      return <VSCodeStagePanel controller={controller} />;
    case "done":
      return <DoneStagePanel controller={controller} />;
    default:
      return <EnvironmentStagePanel controller={controller} />;
  }
}

function EnvironmentStagePanel({
  controller,
}: {
  controller: OnboardingFlowController;
}) {
  const runtimeRequirements = controller.workflow?.runtimeRequirements;
  const failingChecks =
    runtimeRequirements?.checks.filter((check) => check.status !== "pass") || [];

  return (
    <section className="step-section">
      <div className="step-stage-head">
        <h2>环境检查</h2>
        <p>先确认这台机器已经具备运行条件。</p>
      </div>
      <div className={`notice-banner ${runtimeRequirements?.ready ? "good" : "warn"}`}>
        {runtimeRequirements?.summary || "当前服务还在检查中，请稍候。"}
      </div>
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
      <div className="button-row">
        <button
          className="secondary-button"
          type="button"
          onClick={() => void controller.retryEnvironmentCheck()}
        >
          重新检查
        </button>
      </div>
    </section>
  );
}

function PermissionStagePanel({
  controller,
}: {
  controller: OnboardingFlowController;
}) {
  const permissionStage = controller.permissionStage;
  if (!permissionStage) {
    return (
      <UnavailableStagePanel
        titleText="权限检查"
        message="当前还没有可用的飞书应用，暂时无法检查权限。"
      />
    );
  }

  const showWarning = permissionStage.status !== "complete";
  const permissionHint =
    permissionStage.status === "complete"
      ? "当前基础权限已经齐全。"
      : permissionStage.status === "deferred"
        ? "你已选择先跳过这一步，后续仍可回到这里重新检查。"
        : "如果当前企业权限暂时申请不到，你也可以先跳过这一步，后面再回来补齐。";

  return (
    <section className="step-section">
      <div className="step-stage-head">
        <h2>权限检查</h2>
        <p>{permissionStage.summary}</p>
      </div>
      <div className={`notice-banner ${showWarning ? "warn" : "good"}`}>
        {permissionHint}
      </div>
      {(permissionStage.missingScopes || []).length > 0 ? (
        <div className="scope-list">
          {(permissionStage.missingScopes || []).map((scope) => (
            <span
              key={`${scope.scopeType || "tenant"}-${scope.scope}`}
              className="scope-pill"
            >
              <code>{scope.scope}</code>
            </span>
          ))}
        </div>
      ) : null}
      {permissionStage.grantJSON ? (
        <div className="panel">
          <div className="section-heading">
            <div>
              <h4>可复制的一次性权限配置</h4>
              <p>补齐后重新检查即可看到最新状态。</p>
            </div>
          </div>
          <textarea
            readOnly
            className="code-textarea"
            value={permissionStage.grantJSON || ""}
          />
          <div className="button-row">
            <button
              className="ghost-button"
              type="button"
              onClick={() => void controller.copyGrantJSON(permissionStage.grantJSON || "")}
            >
              复制配置
            </button>
            {stageAllowsAction(permissionStage, "open_auth") ? (
              <a
                className="ghost-button"
                href={controller.activeConsoleLinks?.auth || "#"}
                rel="noreferrer"
                target="_blank"
              >
                打开飞书后台权限配置
              </a>
            ) : null}
          </div>
        </div>
      ) : null}
      <div className="button-row">
        <button
          className="secondary-button"
          type="button"
          disabled={
            !stageAllowsAction(permissionStage, "recheck") ||
            controller.actionBusy === "permission-recheck"
          }
          onClick={() => void controller.recheckPermissionStage()}
        >
          {permissionStage.status === "pending" ? "检查并继续" : "重新检查"}
        </button>
        {stageAllowsAction(permissionStage, "force_skip") ? (
          <button
            className="ghost-button"
            type="button"
            disabled={controller.actionBusy === "permission-force-skip"}
            onClick={() => void controller.skipPermissionStage()}
          >
            强制跳过这一步
          </button>
        ) : null}
      </div>
    </section>
  );
}

function EventsStagePanel({
  controller,
}: {
  controller: OnboardingFlowController;
}) {
  const eventsStage = controller.eventsStage;
  if (!eventsStage) {
    return (
      <UnavailableStagePanel
        titleText="事件订阅"
        message="当前还没有可用的飞书应用，暂时无法进入事件订阅联调。"
      />
    );
  }

  return (
    <section className="step-section">
      <div className="step-stage-head">
        <h2>事件订阅</h2>
        <p>{eventsStage.summary}</p>
      </div>
      {controller.eventTest.status === "sent" ? (
        <div className="notice-banner good">
          {controller.eventTest.message || "事件订阅测试提示已发送。"}
        </div>
      ) : null}
      {controller.eventTest.status === "error" ? (
        <div className="notice-banner danger">{controller.eventTest.message}</div>
      ) : null}
      <p className="support-copy">
        前往{" "}
        <a
          className="inline-link"
          href={controller.activeConsoleLinks?.events || "#"}
          rel="noreferrer"
          target="_blank"
        >
          飞书后台
        </a>{" "}
        配置事件订阅。
      </p>
      <RequirementTable
        headers={["事件", "用途"]}
        rows={(controller.manifest?.events || []).map((item) => ({
          key: item.event,
          cells: [
            <CopyableRequirementCell
              key={`${item.event}-event`}
              label="事件名"
              value={item.event}
              onCopy={controller.copyRequirementValue}
            />,
            item.purpose || "",
          ],
        }))}
      />
      <div className="button-row">
        {stageAllowsAction(eventsStage, "start_test") ? (
          <button
            className="secondary-button"
            type="button"
            disabled={controller.actionBusy === "test-events" || !controller.activeApp?.id}
            onClick={() =>
              controller.activeApp?.id &&
              void controller.startTest(controller.activeApp.id, "events")
            }
          >
            重新发送测试提示
          </button>
        ) : null}
        {stageAllowsAction(eventsStage, "continue") ? (
          <button
            className="primary-button"
            type="button"
            disabled={controller.actionBusy === "continue-events"}
            onClick={() => void controller.continueSetupStage("events")}
          >
            下一步
          </button>
        ) : null}
      </div>
    </section>
  );
}

function CallbackStagePanel({
  controller,
}: {
  controller: OnboardingFlowController;
}) {
  const callbackStage = controller.callbackStage;
  if (!callbackStage) {
    return (
      <UnavailableStagePanel
        titleText="回调配置"
        message="当前还没有可用的飞书应用，暂时无法进入回调联调。"
      />
    );
  }

  return (
    <section className="step-section">
      <div className="step-stage-head">
        <h2>回调配置</h2>
        <p>{callbackStage.summary}</p>
      </div>
      {controller.callbackTest.status === "sent" ? (
        <div className="notice-banner good">
          {controller.callbackTest.message || "回调测试卡片已发送。"}
        </div>
      ) : null}
      {controller.callbackTest.status === "error" ? (
        <div className="notice-banner danger">{controller.callbackTest.message}</div>
      ) : null}
      <p className="support-copy">
        前往{" "}
        <a
          className="inline-link"
          href={controller.activeConsoleLinks?.callback || "#"}
          rel="noreferrer"
          target="_blank"
        >
          飞书后台
        </a>{" "}
        配置回调。
      </p>
      <RequirementTable
        headers={["回调", "用途"]}
        rows={(controller.manifest?.callbacks || []).map((item) => ({
          key: item.callback,
          cells: [
            <CopyableRequirementCell
              key={`${item.callback}-callback`}
              label="回调名"
              value={item.callback}
              onCopy={controller.copyRequirementValue}
            />,
            item.purpose || "",
          ],
        }))}
      />
      <div className="button-row">
        {stageAllowsAction(callbackStage, "start_test") ? (
          <button
            className="secondary-button"
            type="button"
            disabled={controller.actionBusy === "test-callback" || !controller.activeApp?.id}
            onClick={() =>
              controller.activeApp?.id &&
              void controller.startTest(controller.activeApp.id, "callback")
            }
          >
            重新发送测试提示
          </button>
        ) : null}
        {stageAllowsAction(callbackStage, "continue") ? (
          <button
            className="primary-button"
            type="button"
            disabled={controller.actionBusy === "continue-callback"}
            onClick={() => void controller.continueSetupStage("callback")}
          >
            下一步
          </button>
        ) : null}
      </div>
    </section>
  );
}

function MenuStagePanel({
  controller,
}: {
  controller: OnboardingFlowController;
}) {
  const menuStage = controller.menuStage;
  if (!menuStage) {
    return (
      <UnavailableStagePanel
        titleText="菜单确认"
        message="当前还没有可用的飞书应用，暂时无法确认菜单。"
      />
    );
  }

  return (
    <section className="step-section">
      <div className="step-stage-head">
        <h2>菜单确认</h2>
        <p>{menuStage.summary}</p>
      </div>
      <p className="support-copy">
        前往{" "}
        <a
          className="inline-link"
          href={controller.activeConsoleLinks?.bot || "#"}
          rel="noreferrer"
          target="_blank"
        >
          飞书后台
        </a>{" "}
        完成菜单配置。
      </p>
      <div className="button-row">
        {stageAllowsAction(menuStage, "continue") ? (
          <button
            className="primary-button"
            type="button"
            disabled={controller.actionBusy === "continue-menu"}
            onClick={() => void controller.continueSetupStage("menu")}
          >
            下一步
          </button>
        ) : null}
      </div>
    </section>
  );
}

function AutostartStagePanel({
  controller,
}: {
  controller: OnboardingFlowController;
}) {
  const autostartStage = controller.workflow?.autostart;
  const autostart = autostartStage?.autostart || null;
  if (!autostartStage) {
    return <UnavailableStagePanel titleText="自动启动" message="暂时没有拿到自动启动状态。" />;
  }

  return (
    <section className="step-section">
      <div className="step-stage-head">
        <h2>自动启动</h2>
        <p>{autostartStage.summary}</p>
      </div>
      <div className={`notice-banner ${autostartStage.status === "complete" ? "good" : "warn"}`}>
        {autostartStage.summary}
      </div>
      {autostartStage.error ? (
        <div className="notice-banner warn">{autostartStage.error}</div>
      ) : null}
      <div className="button-row">
        {stageAllowsAction(autostartStage, "apply") && autostart?.canApply ? (
          <button
            className="primary-button"
            type="button"
            disabled={controller.actionBusy === "autostart-apply"}
            onClick={() => void controller.applyAutostart()}
          >
            启用自动启动
          </button>
        ) : null}
        {stageAllowsAction(autostartStage, "record_enabled") ? (
          <button
            className="secondary-button"
            type="button"
            disabled={controller.actionBusy === "autostart-enabled"}
            onClick={() =>
              void controller.recordMachineDecision(
                "autostart",
                "enabled",
                "已记录自动启动决策。",
              )
            }
          >
            已启用
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
                "自动启动已留待稍后处理。",
              )
            }
          >
            稍后处理
          </button>
        ) : null}
      </div>
    </section>
  );
}

function VSCodeStagePanel({
  controller,
}: {
  controller: OnboardingFlowController;
}) {
  const vscodeStage = controller.workflow?.vscode;
  const vscode = vscodeStage?.vscode || null;
  if (!vscodeStage) {
    return (
      <UnavailableStagePanel titleText="VS Code 集成" message="暂时没有拿到 VS Code 集成状态。" />
    );
  }

  return (
    <section className="step-section">
      <div className="step-stage-head">
        <h2>VS Code 集成</h2>
        <p>{vscodeStage.summary}</p>
      </div>
      <div className={`notice-banner ${vscodeStage.status === "complete" ? "good" : "warn"}`}>
        {vscodeStage.summary}
      </div>
      {vscodeStage.error ? <div className="notice-banner warn">{vscodeStage.error}</div> : null}
      <div className="button-row">
        {stageAllowsAction(vscodeStage, "apply") ? (
          <button
            className="primary-button"
            type="button"
            disabled={controller.actionBusy === "vscode-apply"}
            onClick={() => void controller.applyVSCode()}
          >
            确认集成
          </button>
        ) : null}
        {stageAllowsAction(vscodeStage, "record_managed_shim") ? (
          <button
            className="secondary-button"
            type="button"
            disabled={controller.actionBusy === "vscode-managed_shim"}
            onClick={() =>
              void controller.recordMachineDecision(
                "vscode",
                "managed_shim",
                "已记录 VS Code 集成决策。",
              )
            }
          >
            已处理
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
                "VS Code 集成已留到目标 SSH 机器上处理。",
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
                "VS Code 集成已留待稍后处理。",
              )
            }
          >
            稍后处理
          </button>
        ) : null}
      </div>
      {vscode ? (
        <p className="support-copy">
          当前检测结果：{vscodeIsReady(vscode) ? "已接入" : "尚未接入"}。
        </p>
      ) : null}
    </section>
  );
}

function DoneStagePanel({
  controller,
}: {
  controller: OnboardingFlowController;
}) {
  return (
    <section className="step-section">
      <div className="step-stage-head">
        <h2>{controller.mode === "setup" ? "欢迎使用" : "流程收口"}</h2>
        <p>
          {controller.mode === "setup"
            ? "当前设置已经可以完成。"
            : "当前已经没有阻塞项，剩余项按建议补齐即可。"}
        </p>
      </div>
      <div className="completed-card">
        <h3>
          {controller.mode === "setup"
            ? "欢迎，设置已经完成。"
            : "当前机器人 onboarding 已经收口。"}
        </h3>
        <p>
          {controller.mode === "setup"
            ? "你现在可以进入管理页面，继续维护机器人、系统集成和存储清理。"
            : "后续如需再次检查权限、联调或机器决策，可以继续在这里处理。"}
        </p>
      </div>
      {controller.mode === "setup" ? (
        <div className="button-row">
          <button
            className="primary-button"
            type="button"
            disabled={controller.actionBusy === "complete-setup"}
            onClick={() => void controller.completeSetup()}
          >
            完成设置并进入管理页面
          </button>
          <a className="ghost-button" href={controller.fallbackAdminURL}>
            直接查看管理页面
          </a>
        </div>
      ) : null}
    </section>
  );
}

function UnavailableStagePanel({
  titleText,
  message,
}: {
  titleText: string;
  message: string;
}) {
  return (
    <section className="step-section">
      <div className="step-stage-head">
        <h2>{titleText}</h2>
        <p>{message}</p>
      </div>
      <div className="notice-banner warn">{message}</div>
    </section>
  );
}

function CopyableRequirementCell({
  value,
  label,
  onCopy,
}: {
  value: string;
  label: string;
  onCopy: (value: string, label: string) => Promise<void>;
}) {
  return (
    <div className="requirement-copy-cell">
      <code>{value}</code>
      <button
        className="table-copy-button"
        type="button"
        aria-label={`复制${label} ${value}`}
        onClick={() => void onCopy(value, label)}
      >
        复制
      </button>
    </div>
  );
}
