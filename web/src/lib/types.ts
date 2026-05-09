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
  product: {
    name: string;
    version?: string;
  };
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

export interface FeishuAppSummary {
  id: string;
  name?: string;
  appId?: string;
  consoleLinks?: {
    auth?: string;
    events?: string;
    callback?: string;
    bot?: string;
  };
  hasSecret: boolean;
  enabled: boolean;
  verifiedAt?: string;
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

export interface ClaudeProfileSummary {
  id: string;
  name?: string;
  authMode?: string;
  baseURL?: string;
  hasAuthToken: boolean;
  model?: string;
  smallModel?: string;
  reasoningEffort?: string;
  builtIn?: boolean;
  persisted: boolean;
  readOnly?: boolean;
}

export interface ClaudeProfilesResponse {
  profiles: ClaudeProfileSummary[];
}

export interface ClaudeProfileResponse {
  profile: ClaudeProfileSummary;
}

export interface ClaudeProfileWriteRequest {
  name?: string;
  baseURL?: string;
  authToken?: string;
  model?: string;
  smallModel?: string;
  reasoningEffort?: string;
}

export interface CodexProviderSummary {
  id: string;
  name?: string;
  baseURL?: string;
  hasApiKey: boolean;
  model?: string;
  reasoningEffort?: string;
  builtIn?: boolean;
  persisted: boolean;
  readOnly?: boolean;
}

export interface CodexProvidersResponse {
  providers: CodexProviderSummary[];
}

export interface CodexProviderResponse {
  provider: CodexProviderSummary;
}

export interface CodexProviderWriteRequest {
  name?: string;
  baseURL?: string;
  apiKey?: string;
  model?: string;
  reasoningEffort?: string;
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

export interface FeishuAppPermissionCheckItem {
  scope: string;
  scopeType?: string;
}

export interface FeishuAppPermissionCheckResponse {
  app: FeishuAppSummary;
  ready: boolean;
  missingScopes?: FeishuAppPermissionCheckItem[];
  grantJSON?: string;
  lastCheckedAt?: string;
}

export interface FeishuAppAutoConfigScopeRef {
  scope: string;
  scopeType?: string;
}

export interface FeishuAppAutoConfigRequirementStatus {
  kind: string;
  key: string;
  scopeType?: string;
  feature?: string;
  purpose?: string;
  required: boolean;
  degradeMessage?: string;
  present: boolean;
}

export interface FeishuAppAutoConfigObservedState {
  configuredScopes?: FeishuAppAutoConfigScopeRef[];
  grantedScopes?: FeishuAppAutoConfigScopeRef[];
  eventSubscriptionType?: string;
  eventRequestUrl?: string;
  configuredEvents?: string[];
  callbackType?: string;
  callbackRequestUrl?: string;
  configuredCallbacks?: string[];
  onlineVersionId?: string;
  onlineVersion?: string;
  onlineVersionStatus?: string;
  unauditVersionId?: string;
  unauditVersion?: string;
  unauditVersionStatus?: string;
  activeVersionId?: string;
  activeVersion?: string;
  activeVersionStatus?: string;
  activeVersionEvents?: string[];
  botEnabled?: boolean;
  messageCardCallbackUrl?: string;
  mobileDefaultAbility?: string;
  pcDefaultAbility?: string;
  encryptionKeyConfigured?: boolean;
  verificationTokenConfigured?: boolean;
}

export interface FeishuAppAutoConfigTargetScopeRequirement {
  scope: string;
  scopeType?: string;
  feature?: string;
  required: boolean;
  degradeMessage?: string;
}

export interface FeishuAppAutoConfigTargetEventRequirement {
  event: string;
  purpose?: string;
  feature?: string;
  required: boolean;
  degradeMessage?: string;
}

export interface FeishuAppAutoConfigTargetCallbackRequirement {
  callback: string;
  purpose?: string;
  feature?: string;
  required: boolean;
  degradeMessage?: string;
}

export interface FeishuAppAutoConfigTargetState {
  scopeRequirements?: FeishuAppAutoConfigTargetScopeRequirement[];
  events?: FeishuAppAutoConfigTargetEventRequirement[];
  callbacks?: FeishuAppAutoConfigTargetCallbackRequirement[];
  policy?: Record<string, unknown>;
}

export interface FeishuAppAutoConfigDiff {
  configPatchRequired: boolean;
  abilityPatchRequired: boolean;
  missingScopes?: FeishuAppAutoConfigScopeRef[];
  extraScopes?: FeishuAppAutoConfigScopeRef[];
  missingEvents?: string[];
  extraEvents?: string[];
  missingCallbacks?: string[];
  extraCallbacks?: string[];
  eventSubscriptionTypeMismatch?: boolean;
  eventRequestUrlMismatch?: boolean;
  callbackTypeMismatch?: boolean;
  callbackRequestUrlMismatch?: boolean;
  publishRequired: boolean;
}

export interface FeishuAppAutoConfigPublishState {
  onlineVersionId?: string;
  onlineVersion?: string;
  onlineVersionStatus?: string;
  unauditVersionId?: string;
  unauditVersion?: string;
  unauditVersionStatus?: string;
  activeVersionId?: string;
  activeVersion?: string;
  activeVersionStatus?: string;
  needsPublish: boolean;
  awaitingReview: boolean;
}

export interface FeishuAppAutoConfigPlan {
  status: string;
  summary?: string;
  blockingReason?: string;
  blockingRequirements?: FeishuAppAutoConfigRequirementStatus[];
  degradableRequirements?: FeishuAppAutoConfigRequirementStatus[];
  current: FeishuAppAutoConfigObservedState;
  target: FeishuAppAutoConfigTargetState;
  diff: FeishuAppAutoConfigDiff;
  publish: FeishuAppAutoConfigPublishState;
}

export interface FeishuAppAutoConfigAction {
  name: string;
  outcome: string;
  details?: string;
}

export interface FeishuAppAutoConfigPlanResponse {
  app: FeishuAppSummary;
  plan: FeishuAppAutoConfigPlan;
}

export interface FeishuAppAutoConfigApplyResult {
  status: string;
  summary?: string;
  blockingReason?: string;
  actions?: FeishuAppAutoConfigAction[];
  plan: FeishuAppAutoConfigPlan;
}

export interface FeishuAppAutoConfigApplyResponse {
  app: FeishuAppSummary;
  result: FeishuAppAutoConfigApplyResult;
}

export interface FeishuAppAutoConfigPublishResult {
  status: string;
  summary?: string;
  blockingReason?: string;
  versionId?: string;
  version?: string;
  actions?: FeishuAppAutoConfigAction[];
  plan: FeishuAppAutoConfigPlan;
}

export interface FeishuAppAutoConfigPublishResponse {
  app: FeishuAppSummary;
  result: FeishuAppAutoConfigPublishResult;
}

export interface FeishuAppTestStartResponse {
  gatewayId: string;
  startedAt: string;
  expiresAt: string;
  phrase?: string;
  message: string;
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

export interface FeishuManifestResponse {
  manifest: {
    events: Array<{
      event: string;
      purpose?: string;
    }>;
    callbacks: Array<{
      callback: string;
      purpose?: string;
    }>;
  };
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

export interface OnboardingWorkflowDecision {
  value?: string;
  decidedAt?: string;
}

export interface OnboardingWorkflowStage {
  id: string;
  title: string;
  status: string;
  summary: string;
  blocking?: boolean;
  optional?: boolean;
  allowedActions?: string[];
}

export interface OnboardingWorkflowPermission extends OnboardingWorkflowStage {
  missingScopes?: FeishuAppPermissionCheckItem[];
  grantJSON?: string;
  lastCheckedAt?: string;
}

export interface OnboardingWorkflowMachineStep extends OnboardingWorkflowStage {
  decision?: OnboardingWorkflowDecision;
  autostart?: AutostartDetectResponse;
  vscode?: VSCodeDetectResponse;
  error?: string;
}

export interface OnboardingWorkflowAutoConfig extends OnboardingWorkflowStage {
  decision?: OnboardingWorkflowDecision;
  plan?: FeishuAppAutoConfigPlan;
  error?: string;
}

export interface OnboardingWorkflowApp {
  app: FeishuAppSummary;
  connection: OnboardingWorkflowStage;
  autoConfig: OnboardingWorkflowAutoConfig;
  menu: OnboardingWorkflowStage;
}

export interface OnboardingWorkflowGuide {
  autoConfiguredSummary?: string;
  remainingManualActions?: string[];
  recommendedNextStep?: string;
}

export interface OnboardingWorkflowCompletion {
  setupRequired: boolean;
  canComplete: boolean;
  summary: string;
  blockingReason?: string;
}

export interface OnboardingWorkflowResponse {
  apps: FeishuAppSummary[];
  selectedAppId?: string;
  currentStage: string;
  machineState: string;
  completion: OnboardingWorkflowCompletion;
  runtimeRequirements: RuntimeRequirementsDetectResponse;
  app?: OnboardingWorkflowApp;
  autostart: OnboardingWorkflowMachineStep;
  vscode: OnboardingWorkflowMachineStep;
  guide?: OnboardingWorkflowGuide;
  stages: OnboardingWorkflowStage[];
}

export interface SetupCompleteResponse {
  setupRequired: boolean;
  adminURL: string;
  message: string;
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

export interface LogsStorageStatusResponse {
  rootDir: string;
  fileCount: number;
  totalBytes: number;
  latestFileAt?: string;
}

export interface LogsStorageCleanupResponse {
  rootDir: string;
  olderThanHours: number;
  deletedFiles: number;
  deletedBytes: number;
  remainingFileCount: number;
  remainingBytes: number;
}
