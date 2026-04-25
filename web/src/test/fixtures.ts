import type {
  AutostartDetectResponse,
  BootstrapState,
  FeishuAppPermissionCheckResponse,
  FeishuAppSummary,
  GatewayStatus,
  ImageStagingStatusResponse,
  LogsStorageStatusResponse,
  PreviewDriveStatusResponse,
  RuntimeRequirementsDetectResponse,
  RuntimeStatus,
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
        title: "Headless 启动器",
        status: "pass",
        summary: "当前服务已经有可用的 codex-remote 启动器。",
      },
      {
        id: "real_codex_binary",
        title: "真实 Codex 二进制",
        status: "pass",
        summary: "当前服务环境下可以解析到真实 codex。",
      },
    ],
    notes: ["这里只检查基础运行条件，不检查 Codex 登录状态。"],
    ...overrides,
  };
}

export function makeRuntimeStatus(
  overrides: Partial<RuntimeStatus> = {},
): RuntimeStatus {
  return {
    instances: [],
    surfaces: [],
    gateways: [],
    pendingRemoteTurns: [],
    activeRemoteTurns: [],
    ...overrides,
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

export function makePermissionCheck(
  overrides: Partial<FeishuAppPermissionCheckResponse> = {},
): FeishuAppPermissionCheckResponse {
  return {
    app: makeApp(),
    ready: true,
    missingScopes: [],
    grantJSON: `{
  "scopes": {
    "tenant": [],
  "user": []
  }
}`,
    lastCheckedAt: "2026-04-25T08:00:00Z",
    ...overrides,
  };
}

export function makeFeishuManifest() {
  return {
    manifest: {
      events: [
        {
          event: "im.message.receive_v1",
          purpose: "接收用户发给机器人的文本和图片消息",
        },
        {
          event: "im.message.recalled_v1",
          purpose: "处理用户撤回消息",
        },
        {
          event: "im.message.reaction.created_v1",
          purpose: "处理用户对消息的反馈动作",
        },
        {
          event: "application.bot.menu_v6",
          purpose: "处理机器人菜单点击",
        },
      ],
      callbacks: [
        {
          callback: "card.action.trigger",
          purpose: "处理卡片按钮和卡片交互回调",
        },
      ],
    },
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
