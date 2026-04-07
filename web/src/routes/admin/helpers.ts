import { formatError, requestJSON } from "../../lib/api";
import type { AdminInstanceSummary, FeishuAppSummary, VSCodeDetectResponse } from "../../lib/types";
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
    { label: "连接测试已通过", done: Boolean(app.wizard?.connectionVerifiedAt), timestamp: app.wizard?.connectionVerifiedAt },
    { label: "权限已导入", done: Boolean(app.wizard?.scopesExportedAt), timestamp: app.wizard?.scopesExportedAt },
    { label: "事件订阅已确认", done: Boolean(app.wizard?.eventsConfirmedAt), timestamp: app.wizard?.eventsConfirmedAt },
    { label: "长连接已确认", done: Boolean(app.wizard?.callbacksConfirmedAt), timestamp: app.wizard?.callbacksConfirmedAt },
    { label: "菜单已确认", done: Boolean(app.wizard?.menusConfirmedAt), timestamp: app.wizard?.menusConfirmedAt },
    { label: "应用已发布", done: Boolean(app.wizard?.publishedAt), timestamp: app.wizard?.publishedAt },
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

export function appConnectionTone(app: FeishuAppSummary): "neutral" | "good" | "warn" | "danger" {
  if (!app.enabled) {
    return "neutral";
  }
  if (app.status?.state === "auth_failed") {
    return "danger";
  }
  if (app.status?.state === "degraded") {
    return "warn";
  }
  if (app.status?.state === "connected") {
    return "good";
  }
  if (app.status?.state === "connecting") {
    return "warn";
  }
  if (app.wizard?.connectionVerifiedAt) {
    return "warn";
  }
  return "neutral";
}

export function appConnectionLabel(app: FeishuAppSummary): string {
  if (!app.enabled) {
    return "已停用";
  }
  switch (app.status?.state) {
    case "connected":
      return "在线";
    case "connecting":
      return "连接中";
    case "degraded":
      return "需要关注";
    case "auth_failed":
      return "凭证异常";
    default:
      if (app.wizard?.connectionVerifiedAt) {
        return "未连接";
      }
      if (app.appId || app.hasSecret) {
        return "待测试";
      }
      return "待配置";
  }
}

export function appSetupProgress(app: FeishuAppSummary): { completed: number; total: number; complete: boolean; remaining: number } {
  const steps = [
    Boolean(app.wizard?.connectionVerifiedAt),
    Boolean(app.wizard?.scopesExportedAt),
    Boolean(app.wizard?.eventsConfirmedAt),
    Boolean(app.wizard?.callbacksConfirmedAt),
    Boolean(app.wizard?.menusConfirmedAt),
    Boolean(app.wizard?.publishedAt),
  ];
  const completed = steps.filter(Boolean).length;
  const total = steps.length;
  return {
    completed,
    total,
    complete: completed === total,
    remaining: total - completed,
  };
}

export function instanceStatusTone(instance: AdminInstanceSummary): "neutral" | "good" | "warn" | "danger" {
  if (instance.status === "error" || instance.lastError) {
    return "danger";
  }
  if (instance.status === "starting") {
    return "warn";
  }
  if (instance.status === "busy" || instance.status === "idle" || instance.status === "online") {
    return "good";
  }
  if (instance.online) {
    return "good";
  }
  return "neutral";
}

export function instanceStatusLabel(instance: AdminInstanceSummary): string {
  if (instance.status === "error" || instance.lastError) {
    return "异常";
  }
  if (instance.status === "starting") {
    return "启动中";
  }
  if (instance.status === "busy") {
    return "使用中";
  }
  if (instance.status === "idle") {
    return "空闲";
  }
  if (instance.online) {
    return "在线";
  }
  return "离线";
}

export function instanceSourceLabel(instance: AdminInstanceSummary): string {
  if (instance.source === "headless" && instance.managed) {
    return "受管 headless";
  }
  if (instance.source === "vscode") {
    return "来自 VS Code";
  }
  if (instance.source === "headless") {
    return "后台实例";
  }
  return "本机实例";
}

export function vscodeModeLabel(mode?: string): string {
  switch (mode) {
    case "all":
      return "同时配置两处";
    case "editor_settings":
      return "只写入 settings";
    case "managed_shim":
      return "只处理扩展入口";
    default:
      return "未知";
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
    return "还没有检测到可处理的 VS Code 扩展安装。";
  }
  if (vscode.recommendedMode === "all" && !vscode.latestBundleEntrypoint) {
    return "还没有检测到可处理的 VS Code 扩展安装，暂时无法完成完整接入。";
  }
  if (vscode.needsShimReinstall) {
    return "检测到 VS Code 扩展已升级，建议重新安装扩展入口。";
  }
  if (vscode.recommendedMode === "all") {
    return "当前还没有同时完成 settings.json 和扩展入口配置。";
  }
  return "当前执行入口还没有指向本机 relay。";
}
