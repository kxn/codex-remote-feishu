import { vi } from "vitest";

export type MockFetchCall = {
  path: string;
  method: string;
  rawURL: string;
  init?: RequestInit;
};

type MockResponse = {
  status?: number;
  body?: unknown;
  headers?: Record<string, string>;
};

type MockHandler = MockResponse | ((call: MockFetchCall) => MockResponse | Promise<MockResponse>);

export function installMockFetch(routes: Record<string, MockHandler>) {
  const calls: MockFetchCall[] = [];
  const fetchMock = vi.mocked(fetch);

  fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = input instanceof Request ? input : null;
    const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    const url = new URL(rawURL, window.location.href);
    const path = `${url.pathname}${url.search}`;
    const method = init?.method ?? request?.method ?? "GET";
    const call: MockFetchCall = { path, method, rawURL, init };
    calls.push(call);

    const handler = routes[path] ?? routes[url.pathname];
    if (
      !handler &&
      (url.pathname.endsWith("/api/setup/runtime-requirements/detect") ||
        url.pathname.endsWith("/api/admin/runtime-requirements/detect"))
    ) {
      return new Response(JSON.stringify({
        ready: true,
        summary: "当前机器已满足基础运行条件，可以继续后面的可选配置。",
        currentBinary: "/usr/local/bin/codex-remote",
        codexRealBinary: "/usr/local/bin/codex",
        codexRealBinarySource: "config",
        resolvedCodexRealBinary: "/usr/local/bin/codex",
        lookupMode: "absolute",
        checks: [
          {
            id: "headless_launcher",
            title: "Headless 启动器",
            status: "pass",
            summary: "当前服务已经有可用的 codex-remote 启动器。",
          },
        ],
      }), {
        status: 200,
        headers: {
          "Content-Type": "application/json",
        },
      });
    }
    if (
      !handler &&
      (url.pathname.endsWith("/api/setup/autostart/detect") || url.pathname.endsWith("/api/admin/autostart/detect"))
    ) {
      return new Response(JSON.stringify({
        platform: "linux",
        supported: true,
        manager: "systemd_user",
        currentManager: "systemd_user",
        status: "enabled",
        configured: true,
        enabled: true,
        canApply: true,
      }), {
        status: 200,
        headers: {
          "Content-Type": "application/json",
        },
      });
    }
    if (!handler) {
      throw new Error(`Unhandled fetch for ${method} ${path}`);
    }
    const response = await resolveMockHandler(handler, call, init?.signal ?? request?.signal);
    const status = response.status ?? 200;
    if (status === 204 || status === 205 || status === 304) {
      return new Response(null, {
        status,
        headers: {
          ...(response.headers ?? {}),
        },
      });
    }
    return new Response(JSON.stringify(response.body ?? {}), {
      status,
      headers: {
        "Content-Type": "application/json",
        ...(response.headers ?? {}),
      },
    });
  });

  return { calls, fetchMock };
}

async function resolveMockHandler(
  handler: MockHandler,
  call: MockFetchCall,
  signal?: AbortSignal | null,
): Promise<MockResponse> {
  if (!signal) {
    return typeof handler === "function" ? await handler(call) : handler;
  }
  if (signal.aborted) {
    throw buildAbortError();
  }

  return await new Promise<MockResponse>((resolve, reject) => {
    const abort = () => reject(buildAbortError());
    signal.addEventListener("abort", abort, { once: true });
    Promise.resolve(typeof handler === "function" ? handler(call) : handler).then(
      (response) => {
        signal.removeEventListener("abort", abort);
        resolve(response);
      },
      (error) => {
        signal.removeEventListener("abort", abort);
        reject(error);
      },
    );
  });
}

function buildAbortError() {
  if (typeof DOMException === "function") {
    return new DOMException("The operation was aborted.", "AbortError");
  }
  const error = new Error("The operation was aborted.");
  error.name = "AbortError";
  return error;
}
