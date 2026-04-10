import { relativeLocalPath } from "./paths";

export interface APIErrorShape {
  error?: {
    code?: string;
    message?: string;
    details?: unknown;
  };
}

export interface JSONResult<T> {
  ok: boolean;
  status: number;
  data: T;
}

export class APIRequestError extends Error {
  readonly status: number;
  readonly code?: string;
  readonly details?: unknown;

  constructor(status: number, message: string, code?: string, details?: unknown) {
    super(message);
    this.name = "APIRequestError";
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

export async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const result = await requestJSONAllowHTTPError<T | APIErrorShape>(path, init);
  if (!result.ok) {
    const payload = result.data as APIErrorShape;
    const apiError = payload.error;
    throw new APIRequestError(
      result.status,
      apiError?.message?.trim() || `request failed with status ${result.status}`,
      apiError?.code?.trim(),
      apiError?.details,
    );
  }
  return result.data as T;
}

export async function requestJSONAllowHTTPError<T>(path: string, init?: RequestInit): Promise<JSONResult<T>> {
  const response = await fetch(resolveRequestPath(path), {
    credentials: "same-origin",
    ...init,
    headers: {
      Accept: "application/json",
      ...(init?.headers ?? {}),
    },
  });

  const text = await response.text();
  const contentType = response.headers.get("content-type") ?? "";
  const isJSON = contentType.includes("application/json");

  if (!isJSON) {
    throw new APIRequestError(response.status, `unexpected response content-type: ${contentType || "unknown"}`);
  }
  return {
    ok: response.ok,
    status: response.status,
    data: JSON.parse(text) as T,
  };
}

export async function requestVoid(path: string, init?: RequestInit): Promise<void> {
  const response = await fetch(resolveRequestPath(path), {
    credentials: "same-origin",
    ...init,
    headers: {
      Accept: "application/json",
      ...(init?.headers ?? {}),
    },
  });
  if (response.ok) {
    return;
  }

  const text = await response.text();
  const contentType = response.headers.get("content-type") ?? "";
  if (contentType.includes("application/json") && text) {
    const payload = JSON.parse(text) as APIErrorShape;
    const apiError = payload.error;
    throw new APIRequestError(
      response.status,
      apiError?.message?.trim() || response.statusText,
      apiError?.code?.trim(),
      apiError?.details,
    );
  }
  throw new APIRequestError(response.status, text.trim() || response.statusText);
}

export async function sendJSON<TResponse>(path: string, method: string, body?: unknown): Promise<TResponse> {
  const headers: Record<string, string> = {};
  const init: RequestInit = { method, headers };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  return requestJSON<TResponse>(path, init);
}

export function formatError(error: unknown): string {
  if (error instanceof APIRequestError) {
    const detail = formatErrorDetails(error.details);
    const base = error.code ? `${error.code}: ${error.message}` : error.message;
    if (detail && detail !== error.message) {
      return `${base} (${detail})`;
    }
    return base;
  }
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

function resolveRequestPath(path: string): string {
  return relativeLocalPath(path);
}

function formatErrorDetails(details: unknown): string {
  if (details === undefined || details === null) {
    return "";
  }
  if (typeof details === "string") {
    return details.trim();
  }
  try {
    return JSON.stringify(details);
  } catch {
    return String(details);
  }
}
