import { formatError, requestJSON } from "../../lib/api";
import type { FeishuAppSummary, VSCodeDetectResponse } from "../../lib/types";
import type { AppDraft, WizardRow } from "./types";
import { newAppID } from "./types";

export function emptyDraft(): AppDraft {
  return {
    isNew: true,
    id: "",
    name: "",
    appId: "",
    appSecret: "",
    enabled: true,
  };
}

export function appToDraft(app: FeishuAppSummary): AppDraft {
  return {
    isNew: false,
    id: app.id,
    name: app.name || "",
    appId: app.appId || "",
    appSecret: "",
    enabled: app.enabled,
  };
}

export function syncDraftSelection(
  apps: FeishuAppSummary[],
  preferredID: string,
  setSelectedID: (value: string) => void,
  setDraft: (value: AppDraft) => void,
) {
  const preferredApp = apps.find((app) => app.id === preferredID);
  if (preferredApp) {
    setSelectedID(preferredApp.id);
    setDraft(appToDraft(preferredApp));
    return;
  }
  if (apps.length > 0) {
    setSelectedID(apps[0].id);
    setDraft(appToDraft(apps[0]));
    return;
  }
  setSelectedID(newAppID);
  setDraft(emptyDraft());
}

export function blankToUndefined(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed ? trimmed : undefined;
}

export async function loadVSCodeState(path: string): Promise<{ data: VSCodeDetectResponse | null; error: string }> {
  try {
    return {
      data: await requestJSON<VSCodeDetectResponse>(path),
      error: "",
    };
  } catch (err: unknown) {
    return {
      data: null,
      error: formatError(err),
    };
  }
}

export function buildWizardRows(app: FeishuAppSummary): WizardRow[] {
  return [
    { label: "凭证已保存", done: Boolean(app.wizard?.credentialsSavedAt), timestamp: app.wizard?.credentialsSavedAt },
    { label: "连接已验证", done: Boolean(app.wizard?.connectionVerifiedAt), timestamp: app.wizard?.connectionVerifiedAt },
    { label: "Scopes 已导出", done: Boolean(app.wizard?.scopesExportedAt), timestamp: app.wizard?.scopesExportedAt },
    { label: "事件已确认", done: Boolean(app.wizard?.eventsConfirmedAt), timestamp: app.wizard?.eventsConfirmedAt },
    { label: "回调长连接已确认", done: Boolean(app.wizard?.callbacksConfirmedAt), timestamp: app.wizard?.callbacksConfirmedAt },
    { label: "菜单已确认", done: Boolean(app.wizard?.menusConfirmedAt), timestamp: app.wizard?.menusConfirmedAt },
    { label: "机器人已发布", done: Boolean(app.wizard?.publishedAt), timestamp: app.wizard?.publishedAt },
  ];
}

export function statusTone(state?: string): "neutral" | "good" | "warn" | "danger" {
  switch (state) {
    case "connected":
      return "good";
    case "connecting":
    case "degraded":
      return "warn";
    case "auth_failed":
      return "danger";
    default:
      return "neutral";
  }
}

export function formatDateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

export function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let current = value;
  let unitIndex = 0;
  while (current >= 1024 && unitIndex < units.length - 1) {
    current /= 1024;
    unitIndex += 1;
  }
  return `${current.toFixed(current >= 10 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
}

export function vscodeIsReady(vscode: VSCodeDetectResponse | null): boolean {
  if (!vscode) {
    return false;
  }
  if (vscode.recommendedMode === "managed_shim") {
    return vscode.latestShim.matchesBinary;
  }
  if (vscode.recommendedMode === "all") {
    return vscode.settings.matchesBinary && vscode.latestShim.matchesBinary;
  }
  return vscode.settings.matchesBinary;
}

export function vscodeReadinessText(vscode: VSCodeDetectResponse | null): string {
  if (!vscode) {
    return "尚未检测";
  }
  if (vscodeIsReady(vscode)) {
    return "当前推荐模式已就绪。";
  }
  if (vscode.recommendedMode === "managed_shim" && !vscode.latestBundleEntrypoint) {
    return "还没有检测到可替换的 VS Code 扩展 bundle。";
  }
  if (vscode.recommendedMode === "all" && !vscode.latestBundleEntrypoint) {
    return "当前推荐的是 all，但还没有检测到可替换的 VS Code 扩展 bundle。";
  }
  if (vscode.needsShimReinstall) {
    return "检测到扩展已升级，建议重新安装 shim。";
  }
  if (vscode.recommendedMode === "all") {
    return "当前模式还没有同时覆盖 settings.json 和最新的 managed shim。";
  }
  return "当前模式还没有指向最新的 wrapper binary。";
}
