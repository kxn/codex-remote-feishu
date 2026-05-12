import { type APIErrorShape, formatError, requestJSON } from "../../lib/api";
import type { AutostartDetectResponse, VSCodeDetectResponse } from "../../lib/types";

export type VSCodeUsageScenario = "current_machine" | "remote_only";

export function blankToUndefined(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed ? trimmed : undefined;
}

export function readAPIError(response: { ok: boolean; data: unknown }) {
  if (response.ok) {
    return null;
  }
  const payload = response.data as APIErrorShape;
  return payload.error || null;
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

export function vscodeIsReady(vscode: VSCodeDetectResponse | null): boolean {
  if (!vscode) {
    return false;
  }
  return vscode.latestShim.matchesBinary && !vscode.needsShimReinstall && !vscode.settings.matchesBinary;
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
