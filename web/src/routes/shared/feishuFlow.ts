import { useEffect, useState } from "react";
import { APIRequestError, requestJSON, requestJSONAllowHTTPError, sendJSON } from "../../lib/api";
import type {
  FeishuAppAutoConfigApplyResponse,
  FeishuAppAutoConfigPublishResponse,
  FeishuOnboardingCompleteResponse,
  FeishuOnboardingSession,
  FeishuOnboardingSessionResponse,
} from "../../lib/types";
import { autoConfigNoticeTone } from "./feishuAutoConfig";
import { readAPIError } from "./helpers";

export type NoticeTone = "good" | "warn" | "danger";
export type ConnectMode = "qr" | "manual";

type RuntimeApplyFailureDetails = {
  gatewayId?: string;
  app?: {
    id?: string;
  };
};

type AutoConfigMutationResponse =
  | FeishuAppAutoConfigApplyResponse
  | FeishuAppAutoConfigPublishResponse;

type AutoConfigMutationResult<T extends AutoConfigMutationResponse> =
  | {
      ok: true;
      payload: T;
      notice: {
        tone: NoticeTone;
        message: string;
      };
    }
  | {
      ok: false;
      message: string;
    };

type UseQRCodeOnboardingFlowOptions = {
  enabled: boolean;
  actionBusy: string;
  setActionBusy: (value: string) => void;
  sessionsPath: string;
  onCompleteSuccess: (
    appID: string,
    session: FeishuOnboardingSession,
  ) => Promise<void> | void;
  resetSessionOnSuccess?: boolean;
  defaultPollIntervalSeconds?: number;
  messages?: {
    startFailure?: string;
    refreshFailure?: string;
    verifyFailure?: string;
    completeFailure?: string;
  };
};

const defaultQRCodePollIntervalSeconds = 5;

export function useQRCodeOnboardingFlow(options: UseQRCodeOnboardingFlowOptions) {
  const {
    enabled,
    actionBusy,
    setActionBusy,
    sessionsPath,
    onCompleteSuccess,
    resetSessionOnSuccess = false,
    defaultPollIntervalSeconds: pollIntervalSeconds = defaultQRCodePollIntervalSeconds,
    messages,
  } = options;
  const [connectMode, setConnectMode] = useState<ConnectMode>("qr");
  const [onboardingSession, setOnboardingSession] =
    useState<FeishuOnboardingSession | null>(null);
  const [connectError, setConnectError] = useState("");

  useEffect(() => {
    if (!enabled || connectMode !== "qr") {
      return;
    }
    if (actionBusy === "qr-start" || actionBusy === "qr-complete") {
      return;
    }
    if (!onboardingSession) {
      if (!connectError) {
        void startQRCodeSession();
      }
      return;
    }
    if (onboardingSession.status === "ready" && !connectError) {
      void completeQRCodeSession(onboardingSession.id);
      return;
    }
    if (onboardingSession.status !== "pending") {
      return;
    }
    const pollDelaySeconds = Math.max(
      onboardingSession.pollIntervalSeconds || pollIntervalSeconds,
      pollIntervalSeconds,
    );
    const timer = window.setTimeout(() => {
      void refreshQRCodeSession(onboardingSession.id);
    }, pollDelaySeconds * 1_000);
    return () => window.clearTimeout(timer);
  }, [
    actionBusy,
    connectError,
    connectMode,
    enabled,
    onboardingSession,
    onCompleteSuccess,
    pollIntervalSeconds,
    sessionsPath,
  ]);

  function changeConnectMode(nextMode: ConnectMode) {
    setConnectMode(nextMode);
    setConnectError("");
    setOnboardingSession(null);
  }

  function clearConnectError() {
    setConnectError("");
  }

  function resetConnectFlow() {
    setConnectError("");
    setOnboardingSession(null);
  }

  async function startQRCodeSession() {
    setActionBusy("qr-start");
    setConnectError("");
    try {
      const response = await sendJSON<FeishuOnboardingSessionResponse>(
        sessionsPath,
        "POST",
      );
      setOnboardingSession(response.session);
    } catch {
      setConnectError(messages?.startFailure || "暂时无法开始扫码，请稍后重试。");
    } finally {
      setActionBusy("");
    }
  }

  async function refreshQRCodeSession(sessionID: string) {
    try {
      const response = await requestJSON<FeishuOnboardingSessionResponse>(
        `${sessionsPath}/${encodeURIComponent(sessionID)}`,
      );
      setOnboardingSession(response.session);
      if (response.session.status === "pending") {
        setConnectError("");
      }
    } catch {
      setConnectError(messages?.refreshFailure || "扫码状态暂时没有刷新成功，请稍后重试。");
    }
  }

  async function completeQRCodeSession(sessionID: string) {
    setActionBusy("qr-complete");
    try {
      const response = await requestJSONAllowHTTPError<FeishuOnboardingCompleteResponse>(
        `${sessionsPath}/${encodeURIComponent(sessionID)}/complete`,
        { method: "POST" },
      );
      setOnboardingSession(response.data.session);
      if (!response.ok) {
        setConnectError(
          messages?.verifyFailure || "扫码已经完成，但连接验证没有通过，请重新验证。",
        );
        return;
      }
      await onCompleteSuccess(response.data.app.id, response.data.session);
      setConnectError("");
      if (resetSessionOnSuccess) {
        setOnboardingSession(null);
      }
    } catch {
      setConnectError(
        messages?.completeFailure || "扫码已经完成，但当前还不能继续，请稍后重试。",
      );
    } finally {
      setActionBusy("");
    }
  }

  return {
    connectMode,
    connectError,
    onboardingSession,
    changeConnectMode,
    clearConnectError,
    completeQRCodeSession,
    resetConnectFlow,
  };
}

export async function saveAndVerifyFeishuApp(options: {
  save: () => Promise<string>;
  verifyPath: (appID: string) => string;
  reload: (appID: string) => Promise<void>;
}): Promise<{ verified: boolean; appID: string }> {
  const appID = await options.save();
  const verify = await requestJSONAllowHTTPError(options.verifyPath(appID), {
    method: "POST",
  });
  await options.reload(appID);
  return {
    verified: verify.ok,
    appID,
  };
}

export async function runAutoConfigMutation<T extends AutoConfigMutationResponse>(options: {
  path: string;
  init?: RequestInit;
  fallbackErrorMessage: string;
  fallbackSuccessMessage: string;
}): Promise<AutoConfigMutationResult<T>> {
  const response = await requestJSONAllowHTTPError<T>(
    options.path,
    { method: "POST", ...(options.init || {}) },
  );
  if (!response.ok) {
    const payload = readAPIError(response);
    return {
      ok: false,
      message:
        typeof payload?.details === "string" && payload.details.trim()
          ? payload.details.trim()
          : options.fallbackErrorMessage,
    };
  }
  const payload = response.data as T;
  return {
    ok: true,
    payload,
    notice: {
      tone: autoConfigNoticeTone(payload.result.status),
      message: payload.result.summary?.trim() || options.fallbackSuccessMessage,
    },
  };
}

export function resolveRuntimeApplyFailureTarget(
  error: unknown,
  fallbackAppID?: string,
): string | null {
  if (!(error instanceof APIRequestError) || error.code !== "gateway_apply_failed") {
    return null;
  }
  const details = error.details as RuntimeApplyFailureDetails | undefined;
  return details?.app?.id || details?.gatewayId || fallbackAppID || null;
}
