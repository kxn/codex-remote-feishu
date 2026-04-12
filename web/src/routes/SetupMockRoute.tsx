import { useEffect, useMemo, useState } from "react";
import { BlockingModal, Panel, ShellScaffold, StatusBadge } from "../components/ui";
import type {
  AutostartDetectResponse,
  BootstrapState,
  FeishuAppSummary,
  FeishuOnboardingCompleteResponse,
  FeishuOnboardingSession,
  RuntimeRequirementsDetectResponse,
  VSCodeDetectResponse,
} from "../lib/types";
import { makeApp, makeAutostartDetect, makeBootstrap, makeManifest, makeRuntimeRequirementsDetect, makeVSCodeDetect } from "../test/fixtures";
import { StepEnvCheck } from "./setup/StepEnvCheck";
import { StepCapabilityCheck } from "./setup/StepCapabilityCheck";
import { StepAutostart } from "./setup/StepAutostart";
import { StepVSCode } from "./setup/StepVSCode";
import { StepFinish } from "./setup/StepFinish";
import { FeishuConnectStep } from "./setup/FeishuConnectStep";
import { appToDraft, autostartIsComplete, defaultStepFor, emptyDraft, isStepReachable, previousStepFor, stepState, stepStateLabel, stepStateTone } from "./setup/helpers";
import type { BlockingErrorState, FeishuConnectMode, FeishuConnectStage, SetupNotice, StepID } from "./setup/types";
import { newAppID, wizardSteps } from "./setup/types";
import { type VSCodeSetupOutcome, type VSCodeUsageScenario, vscodeHasDetectedBundle, vscodeIsReady, vscodeOutcomeSummary, vscodePrimaryActionLabel, vscodeRequiresBundle } from "./shared/helpers";

type MockPreset =
  | "blank"
  | "existing"
  | "readonly"
  | "qr_pending"
  | "qr_ready"
  | "capability"
  | "autostart"
  | "vscode_local"
  | "vscode_remote";

const bootstrapState: BootstrapState = makeBootstrap({ feishu: { appCount: 0, enabledAppCount: 0, configuredAppCount: 0, runtimeConfiguredApps: 0 } });
const manifest = makeManifest({
  scopesImport: {
    scopes: {
      tenant: ["im:message", "im:message.group_at_msg", "im:message.reaction", "contact:user.id:readonly"],
      user: [],
    },
  },
  events: [
    { event: "im.message.receive_v1", purpose: "接收用户消息" },
    { event: "im.message.message_read_v1", purpose: "同步消息已读状态" },
  ],
  callbacks: [{ callback: "card.action.trigger", purpose: "处理卡片按钮点击" }],
  menus: [
    { key: "status", name: "查看状态", description: "快速查看服务是否正常。" },
    { key: "threads", name: "切换会话", description: "切换当前消息会发到哪个会话。" },
  ],
});

const basicScopesJSON = JSON.stringify(
  {
    scopes: {
      tenant: manifest.scopesImport.scopes.tenant.filter((scope) => scope !== "drive:drive" && scope !== "im:datasync.feed_card.time_sensitive:write"),
      user: manifest.scopesImport.scopes.user,
    },
  },
  null,
  2,
);

export function SetupMockRoute() {
  const [apps, setApps] = useState<FeishuAppSummary[]>([]);
  const [selectedID, setSelectedID] = useState<string>(newAppID);
  const [draft, setDraft] = useState(emptyDraft());
  const [setupStarted, setSetupStarted] = useState(false);
  const [permissionsConfirmed, setPermissionsConfirmed] = useState(false);
  const [eventsConfirmed, setEventsConfirmed] = useState(false);
  const [longConnectionConfirmed, setLongConnectionConfirmed] = useState(false);
  const [menusConfirmed, setMenusConfirmed] = useState(false);
  const [autostartSkipped, setAutostartSkipped] = useState(false);
  const [vscodeScenario, setVSCodeScenario] = useState<VSCodeUsageScenario | null>(null);
  const [vscodeOutcome, setVSCodeOutcome] = useState<VSCodeSetupOutcome | null>(null);
  const [currentStepHint, setCurrentStepHint] = useState<StepID>("start");
  const [notice, setNotice] = useState<SetupNotice | null>(null);
  const [blockingError, setBlockingError] = useState<BlockingErrorState>(null);
  const [feishuConnectStage, setFeishuConnectStage] = useState<FeishuConnectStage>("mode_select");
  const [feishuConnectMode, setFeishuConnectMode] = useState<FeishuConnectMode | null>("new");
  const [onboardingSession, setOnboardingSession] = useState<FeishuOnboardingSession | null>(null);
  const [onboardingCompletion, setOnboardingCompletion] = useState<FeishuOnboardingCompleteResponse | null>(null);
  const [onboardingNeedsManualRetry, setOnboardingNeedsManualRetry] = useState(false);
  const [runtimeRequirements, setRuntimeRequirements] = useState<RuntimeRequirementsDetectResponse | null>(makeRuntimeRequirementsDetect());
  const [autostart, setAutostart] = useState<AutostartDetectResponse | null>(
    makeAutostartDetect({
      platform: "linux",
      supported: true,
      manager: "systemd_user",
      currentManager: "detached",
      status: "disabled",
      configured: false,
      enabled: false,
      canApply: true,
    }),
  );
  const [vscode, setVSCode] = useState<VSCodeDetectResponse | null>(
    makeVSCodeDetect({
      sshSession: false,
      latestBundleEntrypoint: "/Users/demo/.vscode/extensions/openai.chatgpt-remote/dist/extension.js",
      recordedBundleEntrypoint: "/Users/demo/.vscode/extensions/openai.chatgpt-remote/dist/extension.js",
      candidateBundleEntrypoints: ["/Users/demo/.vscode/extensions/openai.chatgpt-remote/dist/extension.js"],
    }),
  );
  const [activePreset, setActivePreset] = useState<MockPreset>("blank");

  const activeApp = useMemo(() => apps.find((app) => app.id === selectedID) ?? null, [apps, selectedID]);

  useEffect(() => {
    setDraft(appToDraft(activeApp));
    setPermissionsConfirmed(Boolean(activeApp?.wizard?.scopesExportedAt));
    setEventsConfirmed(Boolean(activeApp?.wizard?.eventsConfirmedAt));
    setLongConnectionConfirmed(Boolean(activeApp?.wizard?.callbacksConfirmedAt));
    setMenusConfirmed(Boolean(activeApp?.wizard?.menusConfirmedAt));
  }, [activeApp]);

  const runtimeRequirementsReady = Boolean(runtimeRequirements?.ready);
  const autostartComplete = autostartIsComplete(autostart, autostartSkipped);
  const vscodeComplete = Boolean(vscodeOutcome) || vscodeIsReady(vscode);
  const resolvedCurrentStep = useMemo(
    () =>
      isStepReachable(currentStepHint, bootstrapState, activeApp, runtimeRequirementsReady)
        ? currentStepHint
        : defaultStepFor(bootstrapState, apps, activeApp, runtimeRequirementsReady, autostartComplete, vscodeComplete, setupStarted),
    [activeApp, apps, autostartComplete, currentStepHint, runtimeRequirementsReady, setupStarted, vscodeComplete],
  );
  const currentStepIndex = wizardSteps.findIndex((step) => step.id === resolvedCurrentStep);
  const currentStepMeta = wizardSteps[currentStepIndex >= 0 ? currentStepIndex : 0];
  const stepCompletion = {
    start: runtimeRequirementsReady,
    connect: Boolean(activeApp?.wizard?.connectionVerifiedAt),
    capability: Boolean(activeApp?.wizard?.publishedAt),
    autostart: autostartComplete,
    vscode: vscodeComplete,
  };
  const autostartSummary = useMemo(() => {
    if (autostartSkipped) {
      return "已跳过，可稍后在管理页启用";
    }
    if (!autostart) {
      return "暂未处理";
    }
    if (!autostart.supported) {
      return "当前平台暂不支持";
    }
    return autostart.status === "enabled" ? "已为当前用户启用登录后自动启动" : "当前未启用";
  }, [autostart, autostartSkipped]);
  const vscodeSummary = vscodeOutcomeSummary(vscode, vscodeOutcome);
  const vscodePrimaryLabel = vscodePrimaryActionLabel(vscode, vscodeScenario);
  const vscodeBundleDetected = vscodeHasDetectedBundle(vscode);
  const vscodeNeedsBundle = vscodeRequiresBundle(vscode, vscodeScenario);
  const vscodeCanContinue = Boolean(vscode) && (vscode?.sshSession ? vscodeBundleDetected : vscodeScenario !== null && (!vscodeNeedsBundle || vscodeBundleDetected));

  function showBlockingError(message: string) {
    setBlockingError({ title: "这一步还没有完成", message });
  }

  function nowStamp(): string {
    return "2026-04-11T12:00:00Z";
  }

  function setSingleApp(app: FeishuAppSummary | null) {
    if (!app) {
      setApps([]);
      setSelectedID(newAppID);
      setDraft(emptyDraft());
      return;
    }
    setApps([app]);
    setSelectedID(app.id);
    setDraft(appToDraft(app));
  }

  function createConnectedApp(overrides: Partial<FeishuAppSummary> = {}): FeishuAppSummary {
    const base = activeApp ?? makeApp({ id: "bot-mock", name: "设计评审 Bot", appId: "cli_mock" });
    return {
      ...base,
      ...overrides,
      wizard: {
        ...base.wizard,
        ...overrides.wizard,
      },
    };
  }

  function resetConnectFlow(nextMode?: FeishuConnectMode | null) {
    setFeishuConnectStage("mode_select");
    setFeishuConnectMode(nextMode === undefined ? (apps.length > 0 ? "existing" : "new") : nextMode);
    setOnboardingSession(null);
    setOnboardingCompletion(null);
    setOnboardingNeedsManualRetry(false);
  }

  function applyPreset(preset: MockPreset) {
    const appBase = makeApp({ id: "bot-mock", name: "设计评审 Bot", appId: "cli_mock", wizard: {} });
    setActivePreset(preset);
    setNotice(null);
    setBlockingError(null);
    setSetupStarted(true);
    setAutostartSkipped(false);
    setVSCodeScenario(null);
    setVSCodeOutcome(null);
    setOnboardingNeedsManualRetry(false);
    setOnboardingCompletion(null);
    setRuntimeRequirements(makeRuntimeRequirementsDetect());
    setAutostart(
      makeAutostartDetect({
        platform: "linux",
        supported: true,
        manager: "systemd_user",
        currentManager: "detached",
        status: "disabled",
        configured: false,
        enabled: false,
        canApply: true,
      }),
    );
    setVSCode(
      makeVSCodeDetect({
        sshSession: false,
        latestBundleEntrypoint: "/Users/demo/.vscode/extensions/openai.chatgpt-remote/dist/extension.js",
        recordedBundleEntrypoint: "/Users/demo/.vscode/extensions/openai.chatgpt-remote/dist/extension.js",
        candidateBundleEntrypoints: ["/Users/demo/.vscode/extensions/openai.chatgpt-remote/dist/extension.js"],
      }),
    );

    switch (preset) {
      case "blank":
        setSetupStarted(false);
        setSingleApp(null);
        setCurrentStepHint("start");
        setFeishuConnectStage("mode_select");
        setFeishuConnectMode("new");
        setOnboardingSession(null);
        return;
      case "existing":
        setSingleApp(appBase);
        setCurrentStepHint("connect");
        setFeishuConnectStage("existing_manual");
        setFeishuConnectMode("existing");
        setOnboardingSession(null);
        return;
      case "readonly":
        setSingleApp(
          makeApp({
            id: "bot-readonly",
            name: "现网机器人",
            appId: "cli_readonly",
            readOnly: true,
            persisted: false,
            runtimeOnly: true,
            hasSecret: true,
            wizard: {},
          }),
        );
        setCurrentStepHint("connect");
        setFeishuConnectStage("mode_select");
        setFeishuConnectMode("existing");
        setOnboardingSession(null);
        return;
      case "qr_pending":
        setSingleApp(null);
        setCurrentStepHint("connect");
        setFeishuConnectMode("new");
        setFeishuConnectStage("new_qr");
        setOnboardingSession({
          id: "mock-session",
          status: "pending",
          qrCodeDataUrl: "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='280' height='280'%3E%3Crect width='280' height='280' fill='white'/%3E%3Crect x='18' y='18' width='70' height='70' fill='black'/%3E%3Crect x='192' y='18' width='70' height='70' fill='black'/%3E%3Crect x='18' y='192' width='70' height='70' fill='black'/%3E%3Crect x='122' y='122' width='36' height='36' fill='black'/%3E%3C/svg%3E",
          expiresAt: "2026-04-11T12:30:00Z",
          pollIntervalSeconds: 5,
        });
        return;
      case "qr_ready":
        setSingleApp(null);
        setCurrentStepHint("connect");
        setFeishuConnectMode("new");
        setFeishuConnectStage("new_qr");
        setOnboardingSession({
          id: "mock-session",
          status: "ready",
          qrCodeDataUrl: "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='280' height='280'%3E%3Crect width='280' height='280' fill='white'/%3E%3Crect x='18' y='18' width='70' height='70' fill='black'/%3E%3Crect x='192' y='18' width='70' height='70' fill='black'/%3E%3Crect x='18' y='192' width='70' height='70' fill='black'/%3E%3Crect x='122' y='122' width='36' height='36' fill='black'/%3E%3C/svg%3E",
          appId: "cli_qr_mock",
          displayName: "扫码创建 Bot",
          pollIntervalSeconds: 5,
        });
        return;
      case "capability":
        setSingleApp(makeApp({ ...appBase, wizard: { connectionVerifiedAt: nowStamp() } }));
        setCurrentStepHint("capability");
        resetConnectFlow("existing");
        return;
      case "autostart":
        setSingleApp(
          makeApp({
            ...appBase,
            wizard: {
              connectionVerifiedAt: nowStamp(),
              scopesExportedAt: nowStamp(),
              eventsConfirmedAt: nowStamp(),
              callbacksConfirmedAt: nowStamp(),
              menusConfirmedAt: nowStamp(),
              publishedAt: nowStamp(),
            },
          }),
        );
        setCurrentStepHint("autostart");
        resetConnectFlow("existing");
        return;
      case "vscode_local":
        setSingleApp(
          makeApp({
            ...appBase,
            wizard: {
              connectionVerifiedAt: nowStamp(),
              scopesExportedAt: nowStamp(),
              eventsConfirmedAt: nowStamp(),
              callbacksConfirmedAt: nowStamp(),
              menusConfirmedAt: nowStamp(),
              publishedAt: nowStamp(),
            },
          }),
        );
        setCurrentStepHint("vscode");
        setAutostartSkipped(true);
        setVSCodeScenario("current_machine");
        return;
      case "vscode_remote":
        setSingleApp(
          makeApp({
            ...appBase,
            wizard: {
              connectionVerifiedAt: nowStamp(),
              scopesExportedAt: nowStamp(),
              eventsConfirmedAt: nowStamp(),
              callbacksConfirmedAt: nowStamp(),
              menusConfirmedAt: nowStamp(),
              publishedAt: nowStamp(),
            },
          }),
        );
        setCurrentStepHint("vscode");
        setAutostartSkipped(true);
        setVSCode(makeVSCodeDetect({ sshSession: true, latestBundleEntrypoint: "", recordedBundleEntrypoint: "", candidateBundleEntrypoints: [] }));
        return;
    }
  }

  function beginOnboarding() {
    setSetupStarted(true);
    setFeishuConnectMode("new");
    setFeishuConnectStage("new_qr");
    setOnboardingSession({
      id: "mock-session",
      status: "pending",
      qrCodeDataUrl: "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='280' height='280'%3E%3Crect width='280' height='280' fill='white'/%3E%3Crect x='18' y='18' width='70' height='70' fill='black'/%3E%3Crect x='192' y='18' width='70' height='70' fill='black'/%3E%3Crect x='18' y='192' width='70' height='70' fill='black'/%3E%3Crect x='122' y='122' width='36' height='36' fill='black'/%3E%3C/svg%3E",
      expiresAt: "2026-04-11T12:30:00Z",
      pollIntervalSeconds: 5,
    });
  }

  function verifyAndAdvance() {
    if (activeApp?.readOnly) {
      setSingleApp(createConnectedApp({ wizard: { ...activeApp.wizard, connectionVerifiedAt: nowStamp() } }));
      setCurrentStepHint("capability");
      resetConnectFlow("existing");
      setNotice({ tone: "good", message: "这个飞书应用已经验证通过，继续下一步。" });
      return;
    }
    if (draft.appId.trim() === "") {
      showBlockingError("请先填写 App ID。");
      return;
    }
    if (draft.appSecret.trim() === "" && !activeApp?.hasSecret) {
      showBlockingError("请先填写 App Secret。");
      return;
    }
    setSingleApp(
      createConnectedApp({
        id: activeApp?.id ?? "bot-mock",
        name: draft.name || "设计评审 Bot",
        appId: draft.appId,
        hasSecret: true,
        wizard: { ...activeApp?.wizard, connectionVerifiedAt: nowStamp() },
      }),
    );
    setSetupStarted(true);
    resetConnectFlow("existing");
    setCurrentStepHint("capability");
    setNotice({ tone: "good", message: "飞书应用连接成功，已进入下一步。" });
  }

  function continueModeSelection() {
    if (!feishuConnectMode) {
      showBlockingError("请先选择你想怎么接入飞书应用。");
      return;
    }
    if (feishuConnectMode === "existing") {
      setFeishuConnectStage("existing_manual");
      return;
    }
    beginOnboarding();
  }

  function refreshOnboarding() {
    if (!onboardingSession) {
      beginOnboarding();
      return;
    }
    if (onboardingSession.status === "pending") {
      setOnboardingSession({ ...onboardingSession, status: "ready", appId: "cli_qr_mock", displayName: "扫码创建 Bot" });
      return;
    }
    setNotice({ tone: "good", message: "当前二维码状态已经是最新。" });
  }

  function completeOnboarding() {
    if (onboardingSession?.status !== "ready") {
      showBlockingError("当前还没有拿到扫码结果，请先完成扫码。");
      return;
    }
    const app = makeApp({
      id: "bot-qr-mock",
      name: onboardingSession.displayName || "扫码创建 Bot",
      appId: onboardingSession.appId || "cli_qr_mock",
      wizard: { connectionVerifiedAt: nowStamp() },
    });
    setOnboardingCompletion({
      app,
      result: { connected: true, duration: 600_000_000 },
      session: { ...onboardingSession, status: "completed" },
      guide: {
        autoConfiguredSummary: "扫码创建已经完成，基础接入已经准备好。",
        remainingManualActions: ["接下来继续确认权限、事件和菜单这些基础能力。"],
        recommendedNextStep: "capability",
      },
    });
    setFeishuConnectStage("new_qr_notice");
  }

  function continueOnboardingNotice() {
    if (!onboardingCompletion) {
      showBlockingError("当前没有可继续的扫码结果。");
      return;
    }
    setSingleApp(onboardingCompletion.app);
    setCurrentStepHint("capability");
    resetConnectFlow("existing");
    setNotice({ tone: "good", message: "扫码创建的飞书应用已经接好，继续下一步。" });
  }

  function updateWizard(patch: Partial<NonNullable<FeishuAppSummary["wizard"]>>) {
    if (!activeApp) {
      showBlockingError("当前还没有可用的飞书应用。");
      return;
    }
    setSingleApp(createConnectedApp({ wizard: { ...activeApp.wizard, ...patch } }));
  }

  function goToPreviousStep() {
    if (resolvedCurrentStep === "connect" && feishuConnectStage !== "mode_select") {
      resetConnectFlow(null);
      return;
    }
    const previous = previousStepFor(resolvedCurrentStep);
    if (previous) {
      setCurrentStepHint(previous);
    }
  }

  return (
    <>
      <ShellScaffold
        routeLabel="Setup Mock"
        subtitle="这是一个纯前端可点击的 websetup 原型，用来复盘流程、截图录屏，并作为后续 Gemini 评审输入。"
        railToggleLabel="步骤导航"
        railClassName="wizard-rail"
        mainClassName="wizard-stage"
        railContent={
          <div className="wizard-step-nav" aria-label="Setup Mock Steps">
            {wizardSteps.map((step) => {
              const state = stepState(step.id, resolvedCurrentStep, stepCompletion, bootstrapState, activeApp, runtimeRequirementsReady);
              return (
                <button key={step.id} type="button" className={`wizard-step-link${step.id === resolvedCurrentStep ? " current" : ""}`} onClick={() => setCurrentStepHint(step.id)}>
                  <div>
                    <strong>{step.label}</strong>
                    <p>{step.summary}</p>
                  </div>
                  <StatusBadge value={stepStateLabel(state)} tone={stepStateTone(state)} />
                </button>
              );
            })}
          </div>
        }
      >
        <header className="page-hero wizard-hero">
          <div>
            <p className="page-kicker">Setup Mock Step {currentStepIndex + 1}/{wizardSteps.length}</p>
            <h2>{currentStepMeta.label}</h2>
            <p className="wizard-hero-copy">{currentStepMeta.summary}</p>
          </div>
          <div className="hero-actions">
            <button className="secondary-button" type="button" onClick={() => applyPreset("blank")}>重置原型</button>
          </div>
        </header>

        <Panel title="场景预设" description="一键切到关键分支，后面给 Gemini 评审时可以直接录同一套页面，不需要每次从头走。" className="wizard-panel">
          <div className="mock-control-grid">
            <div className="mock-control-card">
              <strong>接入飞书应用</strong>
              <div className="mock-chip-row">
                <button className={`mock-chip${activePreset === "blank" ? " active" : ""}`} type="button" onClick={() => applyPreset("blank")}>空白开始</button>
                <button className={`mock-chip${activePreset === "existing" ? " active" : ""}`} type="button" onClick={() => applyPreset("existing")}>已有应用</button>
                <button className={`mock-chip${activePreset === "readonly" ? " active" : ""}`} type="button" onClick={() => applyPreset("readonly")}>只读应用</button>
                <button className={`mock-chip${activePreset === "qr_pending" ? " active" : ""}`} type="button" onClick={() => applyPreset("qr_pending")}>扫码等待中</button>
                <button className={`mock-chip${activePreset === "qr_ready" ? " active" : ""}`} type="button" onClick={() => applyPreset("qr_ready")}>扫码已完成</button>
              </div>
            </div>
            <div className="mock-control-card">
              <strong>后续步骤</strong>
              <div className="mock-chip-row">
                <button className={`mock-chip${activePreset === "capability" ? " active" : ""}`} type="button" onClick={() => applyPreset("capability")}>能力检查</button>
                <button className={`mock-chip${activePreset === "autostart" ? " active" : ""}`} type="button" onClick={() => applyPreset("autostart")}>自动启动</button>
                <button className={`mock-chip${activePreset === "vscode_local" ? " active" : ""}`} type="button" onClick={() => applyPreset("vscode_local")}>VS Code 本机</button>
                <button className={`mock-chip${activePreset === "vscode_remote" ? " active" : ""}`} type="button" onClick={() => applyPreset("vscode_remote")}>VS Code 远程</button>
              </div>
            </div>
          </div>
        </Panel>

        {notice ? <div className={`notice-banner ${notice.tone}`}>{notice.message}</div> : null}

        <Panel title={currentStepMeta.label} description={currentStepMeta.summary} className="wizard-panel">
          {resolvedCurrentStep === "start" && (
             <StepEnvCheck runtimeRequirements={runtimeRequirements} runtimeRequirementsError="" />
          )}
          {resolvedCurrentStep === "connect" && (
             <FeishuConnectStep
              apps={apps}
              activeApp={activeApp}
              draft={draft}
              connectStage={feishuConnectStage}
              connectMode={feishuConnectMode}
              onboardingSession={onboardingSession}
              onboardingCompletion={onboardingCompletion}
              onboardingNeedsManualRetry={onboardingNeedsManualRetry}
              busyAction=""
              onNameChange={(value) => setDraft((current) => ({ ...current, name: value }))}
              onAppIDChange={(value) => setDraft((current) => ({ ...current, appId: value }))}
              onAppSecretChange={(value) => setDraft((current) => ({ ...current, appSecret: value }))}
              onConnectModeChange={setFeishuConnectMode}
              onContinueModeSelection={continueModeSelection}
              onVerifyManual={verifyAndAdvance}
              onBackToModeSelection={() => resetConnectFlow(null)}
              onRefreshOnboarding={refreshOnboarding}
              onRestartOnboarding={beginOnboarding}
              onSwitchToExistingFlow={() => {
                setFeishuConnectMode("existing");
                setFeishuConnectStage("existing_manual");
                setOnboardingSession(null);
                setOnboardingCompletion(null);
              }}
              onRetryOnboardingComplete={completeOnboarding}
              onContinueOnboardingNotice={continueOnboardingNotice}
             />
          )}
          {resolvedCurrentStep === "capability" && (
             <StepCapabilityCheck
              activeApp={activeApp}
              manifest={manifest}
              scopesJSON={basicScopesJSON}
              permissionsConfirmed={permissionsConfirmed}
              eventsConfirmed={eventsConfirmed}
              longConnectionConfirmed={longConnectionConfirmed}
              menusConfirmed={menusConfirmed}
              busyAction=""
              onPermissionsConfirmedChange={setPermissionsConfirmed}
              onEventsConfirmedChange={setEventsConfirmed}
              onLongConnectionConfirmedChange={setLongConnectionConfirmed}
              onMenusConfirmedChange={setMenusConfirmed}
              onCopyScopes={() => setNotice({ tone: "good", message: "这里是静态 mock，复制动作已模拟完成。" })}
              onConfirmPermissions={() => {
                if (!permissionsConfirmed) {
                  showBlockingError("请先勾选“我已经完成基础权限导入”。");
                  return;
                }
                updateWizard({ scopesExportedAt: nowStamp() });
              }}
              onConfirmEvents={() => {
                if (!eventsConfirmed) {
                  showBlockingError("请先勾选“我已经完成事件订阅”。");
                  return;
                }
                updateWizard({ eventsConfirmedAt: nowStamp() });
              }}
              onConfirmLongConnection={() => {
                if (!longConnectionConfirmed) {
                  showBlockingError("请先勾选“我已经完成卡片回调配置”。");
                  return;
                }
                updateWizard({ callbacksConfirmedAt: nowStamp() });
              }}
              onConfirmMenus={() => {
                if (!menusConfirmed) {
                  showBlockingError("请先勾选“我已经完成飞书应用菜单配置”。");
                  return;
                }
                updateWizard({ menusConfirmedAt: nowStamp() });
              }}
              onCheckPublish={() => {
                updateWizard({ publishedAt: nowStamp() });
                setCurrentStepHint("autostart");
              }}
             />
          )}
          {resolvedCurrentStep === "autostart" && (
             <StepAutostart autostart={autostart} autostartError="" autostartSummary={autostartSummary} />
          )}
          {resolvedCurrentStep === "vscode" && (
             <StepVSCode 
               vscode={vscode} 
               vscodeError="" 
               vscodeScenario={vscodeScenario} 
               vscodeBundleDetected={vscodeBundleDetected} 
               onVSCodeScenarioChange={setVSCodeScenario} 
             />
          )}
          {resolvedCurrentStep === "finish" && (
             <StepFinish activeApp={activeApp} autostartSummary={autostartSummary} vscodeSummary={vscodeSummary} />
          )}

          <div className="wizard-footer">
            <div className="wizard-footer-left">
              {resolvedCurrentStep !== "start" ? <button className="ghost-button" type="button" onClick={goToPreviousStep}>上一步</button> : null}
            </div>
            <div className="wizard-footer-right">
              {/* Secondary Actions */}
              {resolvedCurrentStep === "autostart" && (
                <button className="secondary-button" type="button" onClick={() => { setAutostartSkipped(true); setCurrentStepHint("vscode"); }}>
                  跳过这一步
                </button>
              )}
              {resolvedCurrentStep === "vscode" && (
                <button className="secondary-button" type="button" onClick={() => { setVSCodeOutcome("deferred"); setCurrentStepHint("finish"); }}>
                  跳过 VS Code
                </button>
              )}
              
              {/* Primary Actions */}
              {resolvedCurrentStep === "start" && (
                <button className="primary-button" type="button" onClick={() => {
                  setSetupStarted(true);
                  if (!runtimeRequirementsReady) {
                    showBlockingError("当前机器还不能继续安装。");
                    return;
                  }
                  setCurrentStepHint("connect");
                }}>
                  {runtimeRequirementsReady ? "继续" : "重新检查"}
                </button>
              )}
              {resolvedCurrentStep === "autostart" && (
                <button className="primary-button" type="button" onClick={() => {
                  if (!autostart?.supported || autostart.status === "enabled") {
                    setCurrentStepHint("vscode");
                    return;
                  }
                  setAutostart({ ...autostart, status: "enabled", configured: true, enabled: true, currentManager: "systemd_user" });
                  setCurrentStepHint("vscode");
                }}>
                  {!autostart?.supported || autostart.status === "enabled" ? "继续" : "启用自动启动"}
                </button>
              )}
              {resolvedCurrentStep === "vscode" && (
                <button className="primary-button" type="button" onClick={() => {
                  if (!vscode) {
                    showBlockingError("当前还没有 VS Code 检测结果。");
                    return;
                  }
                  if (vscode.sshSession) {
                    if (!vscodeBundleDetected) {
                      showBlockingError("请先在这台远程机器上打开一次远程 VS Code 窗口，并确保 Codex 扩展已经安装。");
                      return;
                    }
                    setVSCodeOutcome("managed_shim");
                    setCurrentStepHint("finish");
                    return;
                  }
                  if (!vscodeScenario) {
                    showBlockingError("请先选择以后主要怎么使用 VS Code。");
                    return;
                  }
                  if (vscodeScenario === "remote_only") {
                    setVSCodeOutcome("remote_only_skip");
                    setCurrentStepHint("finish");
                    return;
                  }
                  setVSCodeOutcome("managed_shim");
                  setCurrentStepHint("finish");
                }} disabled={!vscodeCanContinue}>
                  {vscodePrimaryLabel}
                </button>
              )}
              {resolvedCurrentStep === "finish" && (
                <button className="primary-button" type="button" onClick={() => setNotice({ tone: "good", message: "这个 mock 已经走到完成态，可以直接拿去截图或录屏。" })}>
                  完成并进入本地管理页
                </button>
              )}
            </div>
          </div>
        </Panel>
      </ShellScaffold>

      <BlockingModal open={Boolean(blockingError)} title={blockingError?.title || ""} message={blockingError?.message || ""} onConfirm={() => setBlockingError(null)} />
    </>
  );
}
