import type {
  AutostartDetectResponse,
  BootstrapState,
  ClaudeProfileSummary,
  CodexProviderSummary,
  FeishuAppAutoConfigApplyResponse,
  FeishuAppAutoConfigPlan,
  FeishuAppAutoConfigPlanResponse,
  FeishuAppAutoConfigPublishResponse,
  FeishuAppSummary,
  GatewayStatus,
  ImageStagingStatusResponse,
  LogsStorageStatusResponse,
  OnboardingWorkflowApp,
  OnboardingWorkflowAutoConfig,
  OnboardingWorkflowCompletion,
  OnboardingWorkflowDecision,
  OnboardingWorkflowGuide,
  OnboardingWorkflowMachineStep,
  OnboardingWorkflowResponse,
  OnboardingWorkflowStage,
  PreviewDriveStatusResponse,
  RuntimeRequirementsDetectResponse,
  VSCodeDetectResponse,
} from "../lib/types";

type BootstrapOverrides = Partial<
  Omit<BootstrapState, "session" | "config" | "relay" | "admin" | "feishu">
> & {
  session?: Partial<BootstrapState["session"]>;
  config?: Partial<BootstrapState["config"]>;
  relay?: Partial<BootstrapState["relay"]>;
  admin?: Partial<BootstrapState["admin"]>;
  feishu?: Partial<BootstrapState["feishu"]>;
};

export function makeBootstrap(
  overrides: BootstrapOverrides = {},
): BootstrapState {
  const {
    session: sessionOverrides,
    config: configOverrides,
    relay: relayOverrides,
    admin: adminOverrides,
    feishu: feishuOverrides,
    ...rest
  } = overrides;

  return {
    phase: rest.phase ?? "ready",
    setupRequired: rest.setupRequired ?? true,
    sshSession: rest.sshSession ?? false,
    product: {
      name: "Codex Remote Feishu",
      version: "v1.7.0",
    },
    session: {
      authenticated: sessionOverrides?.authenticated ?? true,
      trustedLoopback: sessionOverrides?.trustedLoopback ?? true,
    },
    config: {
      path: configOverrides?.path ?? "/tmp/codex-remote.json",
      version: configOverrides?.version ?? 1,
    },
    relay: {
      listenHost: relayOverrides?.listenHost ?? "127.0.0.1",
      listenPort: relayOverrides?.listenPort ?? "9500",
      serverURL: relayOverrides?.serverURL ?? "ws://127.0.0.1:9500/ws/agent",
    },
    admin: {
      listenHost: adminOverrides?.listenHost ?? "127.0.0.1",
      listenPort: adminOverrides?.listenPort ?? "9501",
      url: adminOverrides?.url ?? "http://127.0.0.1:9501/admin/",
      setupURL: adminOverrides?.setupURL ?? "/setup",
      setupTokenRequired: adminOverrides?.setupTokenRequired ?? false,
      setupTokenExpiresAt: adminOverrides?.setupTokenExpiresAt,
    },
    feishu: {
      appCount: feishuOverrides?.appCount ?? 1,
      enabledAppCount: feishuOverrides?.enabledAppCount ?? 1,
      configuredAppCount: feishuOverrides?.configuredAppCount ?? 1,
      runtimeConfiguredApps: feishuOverrides?.runtimeConfiguredApps ?? 1,
    },
    gateways: rest.gateways ?? [],
  };
}

export function makeGatewayStatus(
  overrides: Partial<GatewayStatus> = {},
): GatewayStatus {
  return {
    gatewayId: "bot-1",
    name: "Main Bot",
    state: "connected",
    disabled: false,
    ...overrides,
  };
}

export function makeApp(
  overrides: Partial<FeishuAppSummary> = {},
): FeishuAppSummary {
  return {
    id: "bot-1",
    name: "Main Bot",
    appId: "cli_main",
    consoleLinks: {
      auth: "https://open.feishu.cn/app/cli_main/auth",
      events: "https://open.feishu.cn/app/cli_main/event?tab=event",
      callback: "https://open.feishu.cn/app/cli_main/event?tab=callback",
      bot: "https://open.feishu.cn/app/cli_main/bot",
    },
    hasSecret: true,
    enabled: true,
    persisted: true,
    readOnly: false,
    status: makeGatewayStatus(),
    ...overrides,
  };
}

export function makeClaudeProfile(
  overrides: Partial<ClaudeProfileSummary> = {},
): ClaudeProfileSummary {
  return {
    id: "default",
    name: "默认",
    authMode: "inherit",
    hasAuthToken: false,
    builtIn: true,
    persisted: false,
    readOnly: true,
    ...overrides,
  };
}

export function makeCodexProvider(
  overrides: Partial<CodexProviderSummary> = {},
): CodexProviderSummary {
  return {
    id: "default",
    name: "系统默认",
    hasApiKey: false,
    builtIn: true,
    persisted: false,
    readOnly: true,
    ...overrides,
  };
}

export function makeVSCodeDetect(
  overrides: Partial<VSCodeDetectResponse> = {},
): VSCodeDetectResponse {
  return {
    sshSession: false,
    recommendedMode: "managed_shim",
    currentMode: "managed_shim",
    currentBinary: "/usr/local/bin/codex",
    installStatePath: "/tmp/install-state.json",
    settings: {
      path: "/tmp/settings.json",
      exists: true,
      cliExecutable: "/usr/local/bin/codex",
      matchesBinary: false,
    },
    latestShim: {
      entrypoint: "/tmp/codex-shim.js",
      exists: true,
      realBinaryPath: "/usr/local/bin/codex",
      realBinaryExists: true,
      installed: true,
      matchesBinary: true,
    },
    needsShimReinstall: false,
    ...overrides,
  };
}

export function makeAutostartDetect(
  overrides: Partial<AutostartDetectResponse> = {},
): AutostartDetectResponse {
  return {
    platform: "darwin",
    supported: false,
    status: "unsupported",
    configured: false,
    enabled: false,
    canApply: false,
    ...overrides,
  };
}

export function makeRuntimeRequirementsDetect(
  overrides: Partial<RuntimeRequirementsDetectResponse> = {},
): RuntimeRequirementsDetectResponse {
  return {
    ready: true,
    summary: "当前机器已满足基础运行条件，可以继续后面的可选配置。",
    currentBinary: "/usr/local/bin/codex-remote",
    codexRealBinary: "/usr/local/bin/codex",
    codexRealBinarySource: "config",
    resolvedCodexRealBinary: "/usr/local/bin/codex",
    lookupMode: "absolute",
    checks: [
      {
        id: "headless_launcher",
        title: "服务启动器",
        status: "pass",
        summary: "当前服务已经有可用的 codex-remote 启动器。",
      },
      {
        id: "real_codex_binary",
        title: "Codex 可执行文件",
        status: "pass",
        summary: "当前服务环境下可以解析 Codex 可执行文件。",
      },
      {
        id: "claude_binary",
        title: "Claude 可执行文件",
        status: "pass",
        summary: "当前服务环境下可以解析 Claude 可执行文件。",
      },
    ],
    notes: ["这里只检查基础运行条件，不检查登录状态或 provider 凭据。"],
    ...overrides,
  };
}

export function makeOnboardingStage(
  overrides: Partial<OnboardingWorkflowStage> = {},
): OnboardingWorkflowStage {
  return {
    id: "connect",
    title: "飞书连接",
    status: "blocked",
    summary: "还没有接入可用的飞书应用。",
    blocking: true,
    optional: false,
    allowedActions: [],
    ...overrides,
  };
}

type OnboardingWorkflowAppOverrides = Partial<
  Omit<OnboardingWorkflowApp, "app" | "connection" | "autoConfig" | "menu">
> & {
  app?: Partial<FeishuAppSummary>;
  connection?: Partial<OnboardingWorkflowStage>;
  autoConfig?: Partial<OnboardingWorkflowAutoConfig>;
  menu?: Partial<OnboardingWorkflowStage>;
};

type OnboardingWorkflowMachineStepOverrides = Partial<
  Omit<OnboardingWorkflowMachineStep, "decision" | "autostart" | "vscode">
> & {
  decision?: Partial<OnboardingWorkflowDecision>;
  autostart?: Partial<AutostartDetectResponse>;
  vscode?: Partial<VSCodeDetectResponse>;
};

type OnboardingWorkflowOverrides = Partial<
  Omit<OnboardingWorkflowResponse, "completion" | "runtimeRequirements" | "app" | "autostart" | "vscode" | "guide" | "stages">
> & {
  completion?: Partial<OnboardingWorkflowCompletion>;
  runtimeRequirements?: Partial<RuntimeRequirementsDetectResponse>;
  app?: OnboardingWorkflowAppOverrides | null;
  autostart?: OnboardingWorkflowMachineStepOverrides;
  vscode?: OnboardingWorkflowMachineStepOverrides;
  guide?: Partial<OnboardingWorkflowGuide>;
  stages?: OnboardingWorkflowStage[];
};

export function makeOnboardingWorkflow(
  overrides: OnboardingWorkflowOverrides = {},
): OnboardingWorkflowResponse {
  const {
    autostart: autostartOverridesInput,
    vscode: vscodeOverridesInput,
    ...workflowOverrides
  } = overrides;
  const currentApp = makeApp({
    id: "bot-1",
    name: "Main Bot",
    appId: "cli_main",
    verifiedAt: "2026-04-25T08:10:00Z",
    ...(workflowOverrides.app?.app || {}),
  });
  const connection = makeOnboardingStage({
    id: "connect",
    title: "飞书连接",
    status: "complete",
    summary: "当前飞书应用连接验证已通过。",
    allowedActions: ["verify"],
    ...(workflowOverrides.app?.connection || {}),
  });
  const autoConfig: OnboardingWorkflowAutoConfig = {
    ...makeOnboardingStage({
      id: "auto_config",
      title: "飞书自动配置",
      status: "pending",
      summary: "当前还需要自动补齐飞书配置。",
      optional: false,
      blocking: false,
      allowedActions: ["apply", "retry", "defer"],
    }),
    plan: makeAutoConfigPlan({
      status: "apply_required",
      summary: "存在待写入的飞书自动配置差异。",
      blockingRequirements: [],
      degradableRequirements: [
        {
          kind: "scope",
          key: "im:message:send_as_bot",
          feature: "core_message_flow",
          required: false,
          present: false,
          degradeMessage: "机器人可能无法主动回消息。",
        },
      ],
    }),
    ...(workflowOverrides.app?.autoConfig || {}),
  };
  const menu = makeOnboardingStage({
    id: "menu",
    title: "菜单确认",
    status: "blocked",
    summary: "请先完成飞书自动配置。",
    blocking: true,
    optional: false,
    allowedActions: [],
    ...(workflowOverrides.app?.menu || {}),
  });
  const app =
    workflowOverrides.app === null
      ? undefined
      : {
          app: currentApp,
          connection,
          autoConfig,
          menu,
        };
  const {
    decision: autostartDecisionOverrides,
    autostart: autostartDetectOverrides,
    vscode: _unusedAutostartVSCodeOverrides,
    ...autostartStageOverrides
  } = autostartOverridesInput || {};
  const autostart: OnboardingWorkflowMachineStep = {
    ...makeOnboardingStage({
      id: "autostart",
      title: "自动启动",
      status: "pending",
      summary: "当前还没有完成自动启动决策。",
      optional: true,
      blocking: false,
      allowedActions: ["apply", "defer"],
    }),
    autostart: makeAutostartDetect({
      platform: "linux",
      supported: true,
      status: "disabled",
      configured: false,
      enabled: false,
      canApply: true,
      ...(autostartDetectOverrides || {}),
    }),
    decision: autostartDecisionOverrides
      ? {
          value: autostartDecisionOverrides.value,
          decidedAt: autostartDecisionOverrides.decidedAt,
        }
      : undefined,
    error: autostartStageOverrides.error,
    ...autostartStageOverrides,
  };
  const {
    decision: vscodeDecisionOverrides,
    vscode: vscodeDetectOverrides,
    autostart: _unusedVSCodeAutostartOverrides,
    ...vscodeStageOverrides
  } = vscodeOverridesInput || {};
  const vscode: OnboardingWorkflowMachineStep = {
    ...makeOnboardingStage({
      id: "vscode",
      title: "VS Code 集成",
      status: "pending",
      summary: "当前还没有完成 VS Code 集成决策。",
      optional: true,
      blocking: false,
      allowedActions: ["apply", "defer", "remote_only"],
    }),
    vscode: makeVSCodeDetect(vscodeDetectOverrides || {}),
    decision: vscodeDecisionOverrides
      ? {
          value: vscodeDecisionOverrides.value,
          decidedAt: vscodeDecisionOverrides.decidedAt,
        }
      : undefined,
    error: vscodeStageOverrides.error,
    ...vscodeStageOverrides,
  };

  return {
    apps: workflowOverrides.apps ?? (app ? [currentApp] : []),
    selectedAppId: workflowOverrides.selectedAppId ?? app?.app.id,
    currentStage: workflowOverrides.currentStage ?? "auto_config",
    machineState: workflowOverrides.machineState ?? "usable_with_pending_items",
    completion: {
      setupRequired: workflowOverrides.completion?.setupRequired ?? true,
      canComplete: workflowOverrides.completion?.canComplete ?? false,
      summary:
        workflowOverrides.completion?.summary ??
        "当前 setup 还不能完成，请先处理阻塞项。",
      blockingReason:
        workflowOverrides.completion?.blockingReason ?? "当前还需要完成飞书自动配置。",
    },
    runtimeRequirements: makeRuntimeRequirementsDetect(
      workflowOverrides.runtimeRequirements || {},
    ),
    app,
    autostart,
    vscode,
    guide: {
      autoConfiguredSummary:
        workflowOverrides.guide?.autoConfiguredSummary ??
        "当前飞书应用已经接入，下面请先完成飞书自动配置。",
      remainingManualActions:
        workflowOverrides.guide?.remainingManualActions ?? [
          "完成飞书自动配置，确认缺失项与后果。",
          "在飞书后台确认机器人菜单配置。",
          "决定是否在这台机器上启用自动启动。",
          "决定如何处理这台机器上的 VS Code 集成。",
        ],
      recommendedNextStep:
        workflowOverrides.guide?.recommendedNextStep ??
        (workflowOverrides.currentStage || "auto_config"),
    },
    stages:
      workflowOverrides.stages ?? [
        makeOnboardingStage({
          id: "runtime_requirements",
          title: "环境检查",
          status: "complete",
          summary: "当前机器已满足基础运行条件，可以继续后面的可选配置。",
          blocking: false,
          allowedActions: ["retry"],
        }),
        connection,
        autoConfig,
        menu,
        autostart,
        vscode,
      ],
  };
}

export function makeImageStagingStatus(
  overrides: Partial<ImageStagingStatusResponse> = {},
): ImageStagingStatusResponse {
  return {
    rootDir: "/tmp/image-staging",
    fileCount: 0,
    totalBytes: 0,
    activeFileCount: 0,
    activeBytes: 0,
    ...overrides,
  };
}

export function makePreviewDriveStatus(
  overrides: Partial<PreviewDriveStatusResponse> = {},
): PreviewDriveStatusResponse {
  return {
    gatewayId: "bot-1",
    name: "Main Bot",
    summary: {
      fileCount: 0,
      scopeCount: 0,
      estimatedBytes: 0,
      unknownSizeFileCount: 0,
    },
    ...overrides,
  };
}

export function makeAutoConfigPlan(
  overrides: Partial<FeishuAppAutoConfigPlan> = {},
): FeishuAppAutoConfigPlan {
  return {
    status: "clean",
    summary: "当前自动配置已完成。",
    blockingReason: "",
    blockingRequirements: [],
    degradableRequirements: [],
    current: {
      configuredScopes: [],
      grantedScopes: [],
      configuredEvents: [],
      configuredCallbacks: [],
      botEnabled: true,
      encryptionKeyConfigured: true,
      verificationTokenConfigured: true,
    },
    target: {
      scopeRequirements: [],
      events: [],
      callbacks: [],
      policy: {},
    },
    diff: {
      configPatchRequired: false,
      abilityPatchRequired: false,
      missingScopes: [],
      extraScopes: [],
      missingEvents: [],
      extraEvents: [],
      missingCallbacks: [],
      extraCallbacks: [],
      eventSubscriptionTypeMismatch: false,
      eventRequestUrlMismatch: false,
      callbackTypeMismatch: false,
      callbackRequestUrlMismatch: false,
      publishRequired: false,
    },
    publish: {
      needsPublish: false,
      awaitingReview: false,
    },
    ...overrides,
  };
}

export function makeAutoConfigPlanResponse(
  overrides: Partial<FeishuAppAutoConfigPlanResponse> = {},
): FeishuAppAutoConfigPlanResponse {
  return {
    app: makeApp(),
    plan: makeAutoConfigPlan(),
    ...overrides,
  };
}

export function makeAutoConfigApplyResponse(
  overrides: Partial<FeishuAppAutoConfigApplyResponse> = {},
): FeishuAppAutoConfigApplyResponse {
  return {
    app: makeApp(),
    result: {
      status: "clean",
      summary: "当前自动配置已完成。",
      blockingReason: "",
      actions: [],
      plan: makeAutoConfigPlan(),
    },
    ...overrides,
  };
}

export function makeAutoConfigPublishResponse(
  overrides: Partial<FeishuAppAutoConfigPublishResponse> = {},
): FeishuAppAutoConfigPublishResponse {
  return {
    app: makeApp(),
    result: {
      status: "awaiting_review",
      summary: "飞书应用变更已进入审核流程，正在等待审核结果。",
      blockingReason: "",
      versionId: "oav_1",
      version: "1.8.1",
      actions: [],
      plan: makeAutoConfigPlan({
        status: "awaiting_review",
        summary: "飞书应用变更已进入审核流程，正在等待审核结果。",
        publish: {
          needsPublish: false,
          awaitingReview: true,
        },
      }),
    },
    ...overrides,
  };
}

export function makeLogsStorageStatus(
  overrides: Partial<LogsStorageStatusResponse> = {},
): LogsStorageStatusResponse {
  return {
    rootDir: "/tmp/logs",
    fileCount: 0,
    totalBytes: 0,
    ...overrides,
  };
}
