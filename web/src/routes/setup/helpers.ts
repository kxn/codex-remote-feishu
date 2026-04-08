import type { BootstrapState, FeishuAppSummary, VSCodeDetectResponse } from "../../lib/types";
import { vscodeIsReady } from "../shared/helpers";
import type { SetupDraft, StepCompletion, StepID } from "./types";
import { newAppID } from "./types";

type StepState = "current" | "done" | "pending" | "locked";

export function emptyDraft(): SetupDraft {
  return {
    isNew: true,
    name: "",
    appId: "",
    appSecret: "",
  };
}

export function chooseAppID(apps: FeishuAppSummary[], preferredID: string): string {
  const preferred = apps.find((app) => app.id === preferredID);
  if (preferred) {
    return preferred.id;
  }
  if (apps.length > 0) {
    return apps[0].id;
  }
  return newAppID;
}

export function preferredSetupAppFromLocation(): string {
  const value = new URLSearchParams(window.location.search).get("app");
  const normalized = value?.trim();
  return normalized ? normalized : newAppID;
}

export function appToDraft(app: FeishuAppSummary | null): SetupDraft {
  if (!app) {
    return emptyDraft();
  }
  return {
    isNew: false,
    name: app.name || "",
    appId: app.appId || "",
    appSecret: "",
  };
}

export function stepState(
  stepID: StepID,
  currentStep: StepID,
  completion: StepCompletion,
  bootstrap: BootstrapState | null,
  activeApp: FeishuAppSummary | null,
): StepState {
  if (stepID === currentStep) {
    return "current";
  }
  if (stepID !== "finish" && completion[stepID as Exclude<StepID, "finish">]) {
    return "done";
  }
  if (isStepReachable(stepID, bootstrap, activeApp)) {
    return "pending";
  }
  return "locked";
}

export function stepStateLabel(state: StepState): string {
  switch (state) {
    case "current":
      return "当前";
    case "done":
      return "已完成";
    case "pending":
      return "未开始";
    default:
      return "已锁定";
  }
}

export function stepStateTone(state: StepState): "neutral" | "good" | "warn" | "danger" {
  switch (state) {
    case "current":
      return "warn";
    case "done":
      return "good";
    case "locked":
      return "neutral";
    default:
      return "neutral";
  }
}

export function defaultStepFor(
  _bootstrap: BootstrapState | null,
  apps: FeishuAppSummary[],
  activeApp: FeishuAppSummary | null,
  vscodeComplete: boolean,
  setupStarted: boolean,
): StepID {
  const started = setupStarted || apps.length > 0;
  if (!started) {
    return "start";
  }
  if (!activeApp || !activeApp.wizard?.connectionVerifiedAt) {
    return "connect";
  }
  if (!activeApp.wizard?.scopesExportedAt) {
    return "permissions";
  }
  if (!activeApp.wizard?.eventsConfirmedAt) {
    return "events";
  }
  if (!activeApp.wizard?.callbacksConfirmedAt) {
    return "longConnection";
  }
  if (!activeApp.wizard?.menusConfirmedAt) {
    return "menus";
  }
  if (!activeApp.wizard?.publishedAt) {
    return "publish";
  }
  if (!vscodeComplete) {
    return "vscode";
  }
  return "finish";
}

export function isStepReachable(stepID: StepID, bootstrap: BootstrapState | null, activeApp: FeishuAppSummary | null): boolean {
  switch (stepID) {
    case "start":
      return true;
    case "connect":
      return true;
    case "permissions":
      return Boolean(activeApp?.wizard?.connectionVerifiedAt);
    case "events":
      return Boolean(activeApp?.wizard?.scopesExportedAt);
    case "longConnection":
      return Boolean(activeApp?.wizard?.eventsConfirmedAt);
    case "menus":
      return Boolean(activeApp?.wizard?.callbacksConfirmedAt);
    case "publish":
      return Boolean(activeApp?.wizard?.menusConfirmedAt);
    case "vscode":
      return Boolean(activeApp?.wizard?.publishedAt);
    case "finish":
      return Boolean(activeApp?.wizard?.publishedAt);
    default:
      return false;
  }
}

export function previousStepFor(stepID: StepID): StepID | null {
  switch (stepID) {
    case "connect":
      return "start";
    case "permissions":
      return "connect";
    case "events":
      return "permissions";
    case "longConnection":
      return "events";
    case "menus":
      return "longConnection";
    case "publish":
      return "menus";
    case "vscode":
      return "publish";
    case "finish":
      return "vscode";
    default:
      return null;
  }
}

export function feishuAppConsoleURL(appId?: string): string {
  const trimmed = (appId || "").trim();
  if (!trimmed) {
    return "https://open.feishu.cn/app?lang=zh-CN";
  }
  return `https://open.feishu.cn/app/${encodeURIComponent(trimmed)}?lang=zh-CN`;
}
