import type {
  AdminInstanceSummary,
  FeishuAppSummary,
  PreviewDriveStatusResponse,
  VSCodeDetectResponse,
} from "../../lib/types";
import { vscodeHasDetectedBundle, vscodeIsReady } from "../shared/helpers";
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

export function buildWizardRows(app: FeishuAppSummary): WizardRow[] {
  return [
    {
      label: "凭证已保存",
      done: Boolean(app.wizard?.credentialsSavedAt),
      timestamp: app.wizard?.credentialsSavedAt,
    },
    {
      label: "连接测试已通过",
      done: Boolean(app.wizard?.connectionVerifiedAt),
      timestamp: app.wizard?.connectionVerifiedAt,
    },
    {
      label: "权限已导入",
      done: Boolean(app.wizard?.scopesExportedAt),
      timestamp: app.wizard?.scopesExportedAt,
    },
    {
      label: "事件订阅已确认",
      done: Boolean(app.wizard?.eventsConfirmedAt),
      timestamp: app.wizard?.eventsConfirmedAt,
    },
    {
      label: "长连接已确认",
      done: Boolean(app.wizard?.callbacksConfirmedAt),
      timestamp: app.wizard?.callbacksConfirmedAt,
    },
    {
      label: "菜单已确认",
      done: Boolean(app.wizard?.menusConfirmedAt),
      timestamp: app.wizard?.menusConfirmedAt,
    },
    {
      label: "应用已发布",
      done: Boolean(app.wizard?.publishedAt),
      timestamp: app.wizard?.publishedAt,
    },
  ];
}

export type SurfaceTone = "neutral" | "good" | "warn" | "danger";

export function appPendingCount(
  app: FeishuAppSummary,
  previewSummary?: PreviewDriveStatusResponse["summary"],
): number {
  const missingCoreItems = [
    !app.wizard?.connectionVerifiedAt,
    !app.wizard?.scopesExportedAt,
    !app.wizard?.eventsConfirmedAt,
    !app.wizard?.callbacksConfirmedAt,
    !app.wizard?.menusConfirmedAt,
    !app.wizard?.publishedAt,
  ].filter(Boolean).length;
  const previewNeedsWork =
    previewSummary?.status === "permission_required" ? 1 : 0;
  return missingCoreItems + previewNeedsWork;
}

export function appSurfaceStatus(
  app: FeishuAppSummary,
  previewSummary?: PreviewDriveStatusResponse["summary"],
): { label: string; tone: SurfaceTone; detail: string } {
  const externalManaged = Boolean(
    app.readOnly || app.runtimeOnly || app.runtimeOverride,
  );
  const missingCoreItems = [
    !app.wizard?.scopesExportedAt,
    !app.wizard?.eventsConfirmedAt,
    !app.wizard?.callbacksConfirmedAt,
    !app.wizard?.menusConfirmedAt,
    !app.wizard?.publishedAt,
  ].filter(Boolean).length;
  const previewNeedsWork = previewSummary?.status === "permission_required";

  if (
    app.runtimeApply?.pending &&
    app.runtimeApply.action === "remove" &&
    !app.persisted
  ) {
    return {
      label: "待移除",
      tone: "warn",
      detail:
        app.runtimeApply.error || "本地配置已经删除，但运行时里还没移除干净。",
    };
  }
  if (app.runtimeApply?.pending) {
    return {
      label: "待同步",
      tone: "warn",
      detail:
        app.runtimeApply.error || "本地配置已经更新，但运行时里还没应用成功。",
    };
  }
  if (!app.enabled) {
    return {
      label: "已停用",
      tone: "neutral",
      detail: "当前不会继续接收飞书消息。",
    };
  }
  if (app.status?.state === "auth_failed") {
    return {
      label: externalManaged ? "外部接管，需处理" : "需处理",
      tone: "danger",
      detail: app.status.lastError || "当前凭证校验失败，请先处理连接问题。",
    };
  }
  if (externalManaged) {
    if (app.status?.state === "degraded") {
      return {
        label: "外部接管，待关注",
        tone: "warn",
        detail:
          app.status.lastError || "当前由外部配置接管，但最近连接状态不稳定。",
      };
    }
    return {
      label: "外部接管",
      tone: app.status?.state === "connected" ? "good" : "neutral",
      detail:
        app.readOnlyReason ||
        (app.status?.lastConnectedAt
          ? `最近连接 ${formatDateTime(app.status.lastConnectedAt)}`
          : "当前由运行时参数或外部配置接管。"),
    };
  }
  if (!app.wizard?.connectionVerifiedAt) {
    return {
      label: "待验证",
      tone: "warn",
      detail:
        app.appId || app.hasSecret
          ? "先完成一次连接验证，确认这份凭证可用。"
          : "先填写 App ID 和 App Secret。",
    };
  }
  if (app.status?.state === "degraded") {
    return {
      label: "可用，待关注",
      tone: "warn",
      detail: app.status.lastError || "最近连接状态不稳定，建议重新检查一下。",
    };
  }
  if (missingCoreItems > 0 || previewNeedsWork) {
    const pendingCount = missingCoreItems + (previewNeedsWork ? 1 : 0);
    return {
      label: "可用，待补全",
      tone: "warn",
      detail: `基础接入已经可用，当前还有 ${pendingCount} 项建议补齐。`,
    };
  }
  if (app.status?.lastConnectedAt) {
    return {
      label: "可用",
      tone: "good",
      detail: `最近连接 ${formatDateTime(app.status.lastConnectedAt)}`,
    };
  }
  return {
    label: "可用",
    tone: "good",
    detail: "当前没有明显需要处理的问题。",
  };
}

export function appConnectionStatus(app: FeishuAppSummary): {
  label: string;
  tone: SurfaceTone;
  detail: string;
} {
  if (!app.enabled) {
    return {
      label: "已停用",
      tone: "neutral",
      detail: "当前不会继续接收飞书消息。",
    };
  }
  if (app.status?.state === "auth_failed") {
    return {
      label: "连接失败",
      tone: "danger",
      detail: app.status.lastError || "凭证或飞书侧配置有问题，请先处理。",
    };
  }
  if (app.status?.state === "connected") {
    return {
      label: "连接正常",
      tone: "good",
      detail: app.status.lastConnectedAt
        ? `最近连接 ${formatDateTime(app.status.lastConnectedAt)}`
        : "已经建立连接。",
    };
  }
  if (app.status?.state === "degraded") {
    return {
      label: "连接不稳定",
      tone: "warn",
      detail: app.status.lastError || "最近连接不稳定，建议重新检查。",
    };
  }
  if (app.wizard?.connectionVerifiedAt) {
    return {
      label: "已验证",
      tone: "warn",
      detail: "这份凭证已经验证过，但当前还没看到稳定连接。",
    };
  }
  if (app.appId || app.hasSecret) {
    return {
      label: "待验证",
      tone: "warn",
      detail: "先完成一次连接验证，确认这份凭证可用。",
    };
  }
  return {
    label: "未填写",
    tone: "neutral",
    detail: "还没有填写这条飞书应用的凭证。",
  };
}

export function appInteractionStatus(app: FeishuAppSummary): {
  label: string;
  tone: SurfaceTone;
  detail: string;
} {
  if (!app.wizard?.connectionVerifiedAt) {
    return {
      label: "等待连接验证",
      tone: "warn",
      detail: "先完成连接验证，再继续补齐权限、事件、回调、菜单和发布。",
    };
  }
  const missing: string[] = [];
  if (!app.wizard?.scopesExportedAt) {
    missing.push("基础权限");
  }
  if (!app.wizard?.eventsConfirmedAt) {
    missing.push("事件订阅");
  }
  if (!app.wizard?.callbacksConfirmedAt) {
    missing.push("卡片回调");
  }
  if (!app.wizard?.menusConfirmedAt) {
    missing.push("飞书应用菜单");
  }
  if (!app.wizard?.publishedAt) {
    missing.push("发布验收");
  }
  if (missing.length === 0) {
    return {
      label: "基础对话与交互已就绪",
      tone: "good",
      detail: "基础收发消息、事件、卡片回调和菜单相关能力都已经补齐。",
    };
  }
  return {
    label: `还需处理 ${missing.length} 项`,
    tone: "warn",
    detail: `待补齐：${missing.join("、")}`,
  };
}

export function appPreviewStatus(
  previewSummary?: PreviewDriveStatusResponse["summary"],
): { label: string; tone: SurfaceTone; detail: string } {
  if (!previewSummary) {
    return {
      label: "状态未获取",
      tone: "warn",
      detail: "当前还没有拿到这条飞书应用的预览目录摘要。",
    };
  }
  if (previewSummary.status === "permission_required") {
    return {
      label: "待开通 Drive 权限",
      tone: "warn",
      detail: normalizeLegacyFeishuCopy(
        previewSummary.statusMessage ||
          "如需 Markdown 预览，请先开通 drive:drive 权限。",
      ),
    };
  }
  if (previewSummary.status === "api_unavailable") {
    return {
      label: "当前未配置",
      tone: "neutral",
      detail: normalizeLegacyFeishuCopy(
        previewSummary.statusMessage ||
          "Markdown 预览是可选增强项，不影响基础对话。",
      ),
    };
  }
  if (previewSummary.rootURL) {
    return {
      label: "已可用",
      tone: "good",
      detail: previewSummary.newestLastUsedAt
        ? `最近使用 ${formatDateTime(previewSummary.newestLastUsedAt)}`
        : "固定预览目录已经建立。",
    };
  }
  return {
    label: "尚未生成",
    tone: "neutral",
    detail: "需要实际生成过 Markdown 预览后，这里才会出现目录摘要。",
  };
}

export function appSourceLabel(app: FeishuAppSummary): string {
  if (
    app.runtimeApply?.pending &&
    app.runtimeApply.action === "remove" &&
    !app.persisted
  ) {
    return "本地已删除，待运行时移除";
  }
  if (app.runtimeOverride) {
    return "启动参数接管";
  }
  if (app.runtimeOnly) {
    return "仅运行时存在";
  }
  if (app.persisted) {
    return "本地配置";
  }
  return "未说明";
}

export function normalizeLegacyFeishuCopy(value: string): string {
  return value.replaceAll("机器人", "飞书应用");
}

export function statusTone(
  state?: string,
): "neutral" | "good" | "warn" | "danger" {
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

export function appConnectionTone(
  app: FeishuAppSummary,
): "neutral" | "good" | "warn" | "danger" {
  if (app.runtimeApply?.pending) {
    return "warn";
  }
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
  if (app.runtimeApply?.pending && app.runtimeApply.action === "remove") {
    return "待移除";
  }
  if (app.runtimeApply?.pending) {
    return "未生效";
  }
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

export function appSetupProgress(app: FeishuAppSummary): {
  completed: number;
  total: number;
  complete: boolean;
  remaining: number;
} {
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

export function instanceStatusTone(
  instance: AdminInstanceSummary,
): "neutral" | "good" | "warn" | "danger" {
  if (instance.status === "error" || instance.lastError) {
    return "danger";
  }
  if (instance.status === "starting") {
    return "warn";
  }
  if (
    instance.status === "busy" ||
    instance.status === "idle" ||
    instance.status === "online"
  ) {
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
    case "editor_settings":
      return "旧版 settings 方式";
    case "both":
      return "旧版双接入";
    case "managed_shim":
      return "扩展入口";
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

export function vscodeReadinessText(
  vscode: VSCodeDetectResponse | null,
): string {
  if (!vscode) {
    return "尚未检测";
  }
  if (vscode.sshSession && !vscodeHasDetectedBundle(vscode)) {
    return "还没检测到这台远程机器上的 VS Code 扩展安装。";
  }
  if (!vscode.sshSession && !vscodeHasDetectedBundle(vscode)) {
    return "这台机器还没检测到可处理的 VS Code 扩展安装。";
  }
  if (vscode.settings.matchesBinary) {
    return "检测到旧版 settings.json 接入，建议迁移到扩展入口。";
  }
  if (vscode.needsShimReinstall) {
    return "检测到 VS Code 扩展已升级，建议重新安装扩展入口。";
  }
  if (vscodeIsReady(vscode)) {
    return vscode.sshSession
      ? "这台远程机器已经通过扩展入口接入 VS Code。"
      : "这台机器已经通过扩展入口接入 VS Code。";
  }
  if (vscode.sshSession) {
    return "这台远程机器还没接入 VS Code，请先处理扩展入口。";
  }
  return "这台机器还没接入 VS Code。当前只支持扩展入口接入。";
}
