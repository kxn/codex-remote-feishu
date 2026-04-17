export interface GatewayStatus {
  gatewayId: string;
  name?: string;
  state: string;
  disabled: boolean;
  lastError?: string;
  lastConnectedAt?: string;
  lastVerifiedAt?: string;
}

export interface BootstrapState {
  phase: string;
  setupRequired: boolean;
  sshSession: boolean;
  session: {
    authenticated: boolean;
    trustedLoopback: boolean;
    scope?: string;
    expiresAt?: string;
  };
  config: {
    path: string;
    version: number;
  };
  relay: {
    listenHost: string;
    listenPort: string;
    serverURL: string;
  };
  admin: {
    listenHost: string;
    listenPort: string;
    url: string;
    setupURL?: string;
    setupTokenRequired: boolean;
    setupTokenExpiresAt?: string;
  };
  feishu: {
    appCount: number;
    enabledAppCount: number;
    configuredAppCount: number;
    runtimeConfiguredApps: number;
  };
  gateways?: GatewayStatus[];
}

export interface RuntimeStatus {
  instances: Array<Record<string, unknown>>;
  surfaces: Array<Record<string, unknown>>;
  gateways?: GatewayStatus[];
  pendingRemoteTurns: Array<Record<string, unknown>>;
  activeRemoteTurns: Array<Record<string, unknown>>;
}

export interface FeishuAppWizardState {
  credentialsSavedAt?: string;
  connectionVerifiedAt?: string;
  scopesExportedAt?: string;
  eventsConfirmedAt?: string;
  callbacksConfirmedAt?: string;
  menusConfirmedAt?: string;
  publishedAt?: string;
}

export interface FeishuAppSummary {
  id: string;
  name?: string;
  appId?: string;
  hasSecret: boolean;
  enabled: boolean;
  verifiedAt?: string;
  wizard?: FeishuAppWizardState;
  persisted: boolean;
  runtimeOnly?: boolean;
  runtimeOverride?: boolean;
  readOnly?: boolean;
  readOnlyReason?: string;
  status?: GatewayStatus;
  runtimeApply?: FeishuRuntimeApplyState;
}

export interface FeishuRuntimeApplyState {
  pending: boolean;
  action?: string;
  error?: string;
  updatedAt?: string;
  retryAvailable?: boolean;
}

export interface FeishuAppMutation {
  kind?: string;
  message?: string;
  reconnectRequested?: boolean;
  requiresNewChat?: boolean;
}

export interface FeishuAppsResponse {
  apps: FeishuAppSummary[];
}

export interface FeishuAppResponse {
  app: FeishuAppSummary;
  mutation?: FeishuAppMutation;
}

export interface FeishuRuntimeApplyFailureDetails {
  gatewayId?: string;
  app?: FeishuAppSummary;
}

export interface VerifyResult {
  connected: boolean;
  errorCode?: string;
  errorMessage?: string;
  duration: number;
}

export interface FeishuAppVerifyResponse {
  app: FeishuAppSummary;
  result: VerifyResult;
}

export interface FeishuOnboardingSession {
  id: string;
  status: string;
  verificationUrl?: string;
  qrCodeDataUrl?: string;
  expiresAt?: string;
  pollIntervalSeconds?: number;
  appId?: string;
  displayName?: string;
  errorCode?: string;
  errorMessage?: string;
}

export interface FeishuOnboardingSessionResponse {
  session: FeishuOnboardingSession;
}

export interface FeishuOnboardingGuide {
  autoConfiguredSummary?: string;
  remainingManualActions?: string[];
  recommendedNextStep?: string;
}

export interface FeishuOnboardingCompleteResponse {
  app: FeishuAppSummary;
  mutation?: FeishuAppMutation;
  result: VerifyResult;
  session: FeishuOnboardingSession;
  guide?: FeishuOnboardingGuide;
}

export interface FeishuAppPublishCheckResponse {
  app: FeishuAppSummary;
  ready: boolean;
  issues?: string[];
}

export interface FeishuManifestResponse {
  manifest: FeishuManifest;
}

export interface FeishuManifest {
  scopesImport: {
    scopes: {
      tenant: string[];
      user: string[];
    };
  };
  events: Array<{
    event: string;
    purpose?: string;
  }>;
  callbacks: Array<{
    callback: string;
    purpose?: string;
  }>;
  menus: Array<{
    key: string;
    name: string;
    description?: string;
  }>;
  checklist: Array<{
    area: string;
    items: string[];
  }>;
}

export interface VSCodeSettingsStatus {
  path: string;
  exists: boolean;
  cliExecutable?: string;
  matchesBinary: boolean;
}

export interface ManagedShimStatus {
  entrypoint: string;
  exists: boolean;
  realBinaryPath?: string;
  realBinaryExists: boolean;
  installed: boolean;
  matchesBinary: boolean;
}

export interface VSCodeDetectResponse {
  sshSession: boolean;
  recommendedMode: string;
  currentMode: string;
  currentBinary: string;
  installStatePath: string;
  installState?: {
    configPath?: string;
    vscodeSettingsPath?: string;
    bundleEntrypoint?: string;
  };
  settings: VSCodeSettingsStatus;
  candidateBundleEntrypoints?: string[];
  latestBundleEntrypoint?: string;
  recordedBundleEntrypoint?: string;
  latestShim: ManagedShimStatus;
  recordedShim?: ManagedShimStatus;
  needsShimReinstall: boolean;
}

export interface AutostartDetectResponse {
  platform: string;
  supported: boolean;
  manager?: string;
  currentManager?: string;
  status: string;
  configured: boolean;
  enabled: boolean;
  installStatePath?: string;
  serviceUnitPath?: string;
  canApply: boolean;
  warning?: string;
  lingerHint?: string;
}

export interface RuntimeRequirementCheck {
  id: string;
  title: string;
  status: string;
  summary: string;
  detail?: string;
}

export interface RuntimeRequirementsDetectResponse {
  ready: boolean;
  summary: string;
  currentBinary?: string;
  codexRealBinary?: string;
  codexRealBinarySource?: string;
  resolvedCodexRealBinary?: string;
  lookupMode?: string;
  checks: RuntimeRequirementCheck[];
  notes?: string[];
}

export interface SetupCompleteResponse {
  setupRequired: boolean;
  adminURL: string;
  message: string;
}

export interface AdminInstancesResponse {
  instances: AdminInstanceSummary[];
}

export interface AdminInstanceSummary {
  instanceId: string;
  displayName?: string;
  workspaceRoot?: string;
  source?: string;
  managed: boolean;
  online: boolean;
  pid?: number;
  status: string;
  requestedAt?: string;
  startedAt?: string;
  idleSince?: string;
  lastHelloAt?: string;
  lastRefreshRequestedAt?: string;
  lastRefreshCompletedAt?: string;
  refreshInFlight: boolean;
  lastError?: string;
}

export interface ImageStagingStatusResponse {
  rootDir: string;
  fileCount: number;
  totalBytes: number;
  activeFileCount: number;
  activeBytes: number;
}

export interface ImageStagingCleanupResponse {
  rootDir: string;
  olderThanHours: number;
  deletedFiles: number;
  deletedBytes: number;
  skippedActiveCount: number;
  remainingFileCount: number;
  remainingBytes: number;
}

export interface PreviewDriveSummary {
  statePath?: string;
  status?: string;
  statusMessage?: string;
  rootToken?: string;
  rootURL?: string;
  fileCount: number;
  scopeCount: number;
  estimatedBytes: number;
  unknownSizeFileCount: number;
  oldestLastUsedAt?: string;
  newestLastUsedAt?: string;
}

export interface PreviewDriveStatusResponse {
  gatewayId: string;
  name?: string;
  summary: PreviewDriveSummary;
}

export interface PreviewDriveCleanupResponse {
  gatewayId: string;
  name?: string;
  olderThanHours: number;
  result: {
    deletedFileCount: number;
    deletedEstimatedBytes: number;
    skippedUnknownLastUsedCount: number;
    summary: PreviewDriveSummary;
  };
}
