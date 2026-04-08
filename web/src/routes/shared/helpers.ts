import { formatError, requestJSON } from "../../lib/api";
import type { FeishuAppMutation, FeishuAppSummary, VSCodeDetectResponse } from "../../lib/types";

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

export function buildAdminFeishuVerifySuccessMessage(app: FeishuAppSummary, duration: number): string {
  const parts = [`连接测试成功，用时 ${(duration / 1_000_000_000).toFixed(1)}s。`];
  parts.push("这一步只验证当前凭证可连接。");
  if (app.status?.state !== "connected") {
    parts.push("运行态仍在重连，实际使用请以连接状态恢复为准。");
  }
  parts.push("如果刚切到另一个飞书 App，旧会话不会自动迁移，请到新机器人侧重新开始会话。");
  return parts.join("");
}

export function buildSetupFeishuVerifySuccessMessage(app: FeishuAppSummary, mutation?: FeishuAppMutation): string {
  const parts: string[] = [];
  if (mutation?.message) {
    parts.push(mutation.message);
  }
  parts.push("飞书应用连接成功，已进入下一步。");
  parts.push("这一步只验证当前凭证可连接。");
  if (app.status?.state !== "connected") {
    parts.push("运行态仍在重连，后续实际使用请以连接状态恢复为准。");
  }
  if (mutation?.requiresNewChat) {
    parts.push("请到新机器人侧重新开始会话，再继续后面的联调。");
  }
  return parts.join("");
}
