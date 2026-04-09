import type {
  AdminInstanceSummary,
  AutostartDetectResponse,
  BootstrapState,
  FeishuAppSummary,
  FeishuManifest,
  GatewayStatus,
  ImageStagingStatusResponse,
  PreviewDriveStatusResponse,
  RuntimeStatus,
  VSCodeDetectResponse,
} from "../lib/types";

export function makeBootstrap(overrides: Partial<BootstrapState> = {}): BootstrapState {
  return {
    phase: "ready",
    setupRequired: true,
    sshSession: false,
    session: {
      authenticated: true,
      trustedLoopback: true,
      ...(overrides.session ?? {}),
    },
    config: {
      path: "/tmp/codex-remote.json",
      version: 1,
      ...(overrides.config ?? {}),
    },
    relay: {
      listenHost: "127.0.0.1",
      listenPort: "4317",
      serverURL: "http://127.0.0.1:4317",
      ...(overrides.relay ?? {}),
    },
    admin: {
      listenHost: "127.0.0.1",
      listenPort: "4300",
      url: "http://127.0.0.1:4300",
      setupURL: "/setup",
      setupTokenRequired: false,
      ...(overrides.admin ?? {}),
    },
    feishu: {
      appCount: 1,
      enabledAppCount: 1,
      configuredAppCount: 1,
      runtimeConfiguredApps: 1,
      ...(overrides.feishu ?? {}),
    },
    gateways: overrides.gateways ?? [],
    ...overrides,
  };
}

export function makeGatewayStatus(overrides: Partial<GatewayStatus> = {}): GatewayStatus {
  return {
    gatewayId: "bot-1",
    name: "Main Bot",
    state: "connected",
    disabled: false,
    ...overrides,
  };
}

export function makeApp(overrides: Partial<FeishuAppSummary> = {}): FeishuAppSummary {
  return {
    id: "bot-1",
    name: "Main Bot",
    appId: "cli_main",
    hasSecret: true,
    enabled: true,
    persisted: true,
    readOnly: false,
    wizard: {},
    status: makeGatewayStatus(),
    ...overrides,
  };
}

export function makeManifest(overrides: Partial<FeishuManifest> = {}): FeishuManifest {
  return {
    scopesImport: {
      scopes: {
        tenant: ["im:message"],
        user: [],
      },
    },
    events: [{ event: "im.message.receive_v1", purpose: "接收消息" }],
    callbacks: [{ callback: "card.action.trigger", purpose: "处理卡片交互" }],
    menus: [{ key: "status", name: "状态", description: "查看状态" }],
    checklist: [],
    ...overrides,
  };
}

export function makeVSCodeDetect(overrides: Partial<VSCodeDetectResponse> = {}): VSCodeDetectResponse {
  return {
    sshSession: false,
    recommendedMode: "all",
    currentMode: "all",
    currentBinary: "/usr/local/bin/codex",
    installStatePath: "/tmp/install-state.json",
    settings: {
      path: "/tmp/settings.json",
      exists: true,
      cliExecutable: "/usr/local/bin/codex",
      matchesBinary: true,
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

export function makeAutostartDetect(overrides: Partial<AutostartDetectResponse> = {}): AutostartDetectResponse {
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

export function makeRuntimeStatus(overrides: Partial<RuntimeStatus> = {}): RuntimeStatus {
  return {
    instances: [],
    surfaces: [],
    gateways: [],
    pendingRemoteTurns: [],
    activeRemoteTurns: [],
    ...overrides,
  };
}

export function makeImageStagingStatus(overrides: Partial<ImageStagingStatusResponse> = {}): ImageStagingStatusResponse {
  return {
    rootDir: "/tmp/image-staging",
    fileCount: 0,
    totalBytes: 0,
    activeFileCount: 0,
    activeBytes: 0,
    ...overrides,
  };
}

export function makePreviewDriveStatus(overrides: Partial<PreviewDriveStatusResponse> = {}): PreviewDriveStatusResponse {
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

export function makeInstance(overrides: Partial<AdminInstanceSummary> = {}): AdminInstanceSummary {
  return {
    instanceId: "inst-1",
    displayName: "Workspace",
    workspaceRoot: "/tmp/workspace",
    source: "headless",
    managed: true,
    online: true,
    status: "online",
    refreshInFlight: false,
    ...overrides,
  };
}
