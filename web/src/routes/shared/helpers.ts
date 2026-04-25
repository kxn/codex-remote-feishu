import { formatError, requestJSON } from "../../lib/api";
import type { AutostartDetectResponse, FeishuAppMutation, FeishuAppSummary, VSCodeDetectResponse } from "../../lib/types";

export type VSCodeUsageScenario = "current_machine" | "remote_only";

export type VSCodeSetupOutcome = "managed_shim" | "remote_only_skip" | "deferred";

export function blankToUndefined(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed ? trimmed : undefined;
}

export async function loadVSCodeState(
  path: string,
  timeoutMs?: number,
): Promise<{ data: VSCodeDetectResponse | null; error: string }> {
  try {
    return {
      data: await requestJSON<VSCodeDetectResponse>(path, undefined, { timeoutMs }),
      error: "",
    };
  } catch (err: unknown) {
    return {
      data: null,
      error: formatError(err),
    };
  }
}

export async function loadAutostartState(path: string): Promise<{ data: AutostartDetectResponse | null; error: string }> {
  try {
    return {
      data: await requestJSON<AutostartDetectResponse>(path),
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
  return vscode.latestShim.matchesBinary && !vscode.needsShimReinstall && !vscode.settings.matchesBinary;
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
  return scenario === "current_machine";
}

export function vscodeApplyModeForScenario(vscode: VSCodeDetectResponse | null, scenario: VSCodeUsageScenario | null): string | null {
  if (!vscode) {
    return null;
  }
  if (vscode.sshSession) {
    return "managed_shim";
  }
  switch (scenario) {
    case "current_machine":
      return "managed_shim";
    default:
      return null;
  }
}

export function currentVSCodeSummary(vscode: VSCodeDetectResponse | null): string {
  if (!vscode) {
    return "暂未处理";
  }
  if (vscode.settings.matchesBinary) {
    return "检测到旧版 settings.json 接入，需迁移到扩展入口";
  }
  if (vscode.needsShimReinstall) {
    return "检测到扩展升级，需重新安装扩展入口";
  }
  if (vscode.latestShim.matchesBinary) {
    if (vscode.sshSession) {
      return "已在这台远程机器上接入（扩展入口）";
    }
    return "已在这台机器上接入（扩展入口）";
  }
  return "暂未处理";
}

export function vscodeOutcomeSummary(vscode: VSCodeDetectResponse | null, outcome: VSCodeSetupOutcome | null): string {
  switch (outcome) {
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
