import type { Dispatch, ReactNode, SetStateAction } from "react";
import type {
  FeishuAppSummary,
  FeishuManifestResponse,
  FeishuOnboardingSession,
  OnboardingWorkflowMachineStep,
  OnboardingWorkflowPermission,
  OnboardingWorkflowResponse,
  OnboardingWorkflowStage,
} from "../../../lib/types";

export type OnboardingSurfaceMode = "setup" | "admin";
export type NoticeTone = "good" | "warn" | "danger";

export type Notice = {
  tone: NoticeTone;
  message: string;
};

export type ManualConnectForm = {
  name: string;
  appId: string;
  appSecret: string;
};

export type TestState = {
  status: "idle" | "sending" | "sent" | "error";
  message: string;
};

export type RuntimeApplyFailureDetails = {
  gatewayId?: string;
  app?: FeishuAppSummary;
};

export type RequirementTableRow = {
  key: string;
  cells: ReactNode[];
};

export type SetupOptionalStageID = "events" | "callback" | "menu";

export type OnboardingFlowSurfaceProps = {
  mode: OnboardingSurfaceMode;
  preferredAppID?: string;
  connectOnly?: boolean;
  autoStartTests?: boolean;
  fallbackAdminURL?: string;
  connectOnlyTitle?: string;
  connectOnlyDescription?: string;
  onConnectedApp?: (appID: string) => Promise<void> | void;
  onContextRefresh?: (preferredAppID?: string) => Promise<void> | void;
};

export type OnboardingFlowController = {
  mode: OnboardingSurfaceMode;
  connectOnly: boolean;
  fallbackAdminURL: string;
  connectOnlyTitle: string;
  connectOnlyDescription: string;
  loading: boolean;
  loadError: string;
  notice: Notice | null;
  manifest: FeishuManifestResponse["manifest"] | null;
  workflow: OnboardingWorkflowResponse | null;
  displayStages: OnboardingWorkflowStage[];
  stageID: string;
  currentStageID: string;
  currentStage: OnboardingWorkflowStage | undefined;
  activeApp: FeishuAppSummary | null;
  activeConsoleLinks: FeishuAppSummary["consoleLinks"] | undefined;
  isReadOnlyApp: boolean;
  connectionStage: OnboardingWorkflowStage | undefined;
  permissionStage: OnboardingWorkflowPermission | null;
  eventsStage: OnboardingWorkflowStage | null;
  callbackStage: OnboardingWorkflowStage | null;
  menuStage: OnboardingWorkflowStage | null;
  actionBusy: string;
  onboardingSession: FeishuOnboardingSession | null;
  connectError: string;
  connectMode: "qr" | "manual";
  manualForm: ManualConnectForm;
  eventTest: TestState;
  callbackTest: TestState;
  setVisibleStageID: Dispatch<SetStateAction<string>>;
  setManualForm: Dispatch<SetStateAction<ManualConnectForm>>;
  retryLoad: () => Promise<void> | void;
  retryEnvironmentCheck: () => Promise<void>;
  changeConnectMode: (nextMode: "qr" | "manual") => void;
  resetQRCodeSession: () => void;
  retryQRCodeVerification: () => void;
  submitManualConnect: () => Promise<void>;
  refreshWorkflowFocus: () => Promise<void>;
  recheckPermissionStage: () => Promise<void>;
  skipPermissionStage: () => Promise<void>;
  startTest: (appID: string, kind: "events" | "callback") => Promise<void>;
  continueSetupStage: (step: SetupOptionalStageID) => Promise<void>;
  recordMachineDecision: (
    kind: "autostart" | "vscode",
    decision: string,
    message: string,
  ) => Promise<void>;
  applyAutostart: () => Promise<void>;
  applyVSCode: () => Promise<void>;
  completeSetup: () => Promise<void>;
  copyGrantJSON: (value: string) => Promise<void>;
  copyRequirementValue: (value: string, label: string) => Promise<void>;
};

export type AllowedActionCarrier =
  | OnboardingWorkflowStage
  | OnboardingWorkflowPermission
  | OnboardingWorkflowMachineStep
  | null
  | undefined;
