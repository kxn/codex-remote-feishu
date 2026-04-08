import { formatError, requestJSON } from "../../lib/api";
import type { FeishuAppMutation, FeishuAppSummary, VSCodeDetectResponse } from "../../lib/types";

export type VSCodeUsageScenario = "local_only" | "remote_only" | "local_and_remote";

export type VSCodeSetupOutcome = "settings" | "managed_shim" | "remote_only_skip" | "deferred";

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

export function vscodeHasDetectedBundle(vscode: VSCodeDetectResponse | null): boolean {
  if (!vscode) {
    return false;
  }
  return Boolean(vscode.latestBundleEntrypoint || vscode.recordedBundleEntrypoint || vscode.candidateBundleEntrypoints?.length);
}

export function vscodeIsReady(vscode: VSCodeDetectResponse | null): boolean {
  if (!vscode) {
    return false;
  }
  if (vscode.sshSession) {
    return vscode.latestShim.matchesBinary;
  }
  return vscode.settings.matchesBinary || vscode.latestShim.matchesBinary;
}

export function vscodePrimaryActionLabel(vscode: VSCodeDetectResponse | null, scenario: VSCodeUsageScenario | null): string {
  if (vscode?.sshSession) {
    return "在这台远程机器上启用 VS Code";
  }
  if (scenario === "remote_only") {
    return "我会去目标 SSH 机器上处理";
  }
  return "在这台机器上启用 VS Code";
}

export function vscodeRequiresBundle(vscode: VSCodeDetectResponse | null, scenario: VSCodeUsageScenario | null): boolean {
  if (!vscode) {
    return false;
  }
  if (vscode.sshSession) {
    return true;
  }
  return scenario === "local_and_remote";
}

export function vscodeApplyModeForScenario(vscode: VSCodeDetectResponse | null, scenario: VSCodeUsageScenario | null): string | null {
  if (!vscode) {
    return null;
  }
  if (vscode.sshSession) {
    return "managed_shim";
  }
  switch (scenario) {
    case "local_only":
      return "editor_settings";
    case "local_and_remote":
      return "managed_shim";
    default:
      return null;
  }
}

export function currentVSCodeSummary(vscode: VSCodeDetectResponse | null): string {
  if (!vscode) {
    return "暂未处理";
  }
  const settingsReady = vscode.settings.matchesBinary;
  const shimReady = vscode.latestShim.matchesBinary;
  if (settingsReady && shimReady) {
    if (vscode.sshSession) {
      return "已在这台远程机器上接入（扩展入口）";
    }
    return "已在这台机器上接入（settings.json + 扩展入口）";
  }
  if (settingsReady) {
    return "已在这台机器上接入（settings.json）";
  }
  if (shimReady) {
    if (vscode.sshSession) {
      return "已在这台远程机器上接入（扩展入口）";
    }
    return "已在这台机器上接入（扩展入口）";
  }
  return "暂未处理";
}

export function vscodeOutcomeSummary(vscode: VSCodeDetectResponse | null, outcome: VSCodeSetupOutcome | null): string {
  switch (outcome) {
    case "settings":
      return "已在这台机器上接入（settings.json）";
    case "managed_shim":
      if (vscode?.sshSession) {
        return "已在这台远程机器上接入（扩展入口）";
      }
      return "已在这台机器上接入（扩展入口）";
    case "remote_only_skip":
      return "当前机器未接入；你选择稍后在目标 SSH 机器上处理";
    case "deferred":
      return "已留到管理页稍后处理";
    default:
      return currentVSCodeSummary(vscode);
  }
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
