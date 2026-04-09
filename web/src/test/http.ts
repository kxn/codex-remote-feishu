import { vi } from "vitest";

export type MockFetchCall = {
  path: string;
  method: string;
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
    const url = new URL(rawURL, window.location.origin);
    const path = `${url.pathname}${url.search}`;
    const method = init?.method ?? request?.method ?? "GET";
    const call: MockFetchCall = { path, method, init };
    calls.push(call);

    const handler = routes[path] ?? routes[url.pathname];
    if (!handler && (url.pathname === "/api/setup/autostart/detect" || url.pathname === "/api/admin/autostart/detect")) {
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
    const response = typeof handler === "function" ? await handler(call) : handler;
    return new Response(JSON.stringify(response.body ?? {}), {
      status: response.status ?? 200,
      headers: {
        "Content-Type": "application/json",
        ...(response.headers ?? {}),
      },
    });
  });

  return { calls, fetchMock };
}
