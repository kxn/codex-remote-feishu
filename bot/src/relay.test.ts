import { describe, expect, it, vi } from "vitest";

import { RelayClient, RelayClientError } from "./relay.js";

describe("RelayClient", () => {
  it("wraps the session listing, detail, and history REST APIs", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        jsonResponse([
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
          }),
        ]),
      )
      .mockResolvedValueOnce(
        jsonResponse(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            turnCount: 2,
          }),
        ),
      )
      .mockResolvedValueOnce(
        jsonResponse([
          {
            direction: "out",
            classification: "agentMessage",
            method: "item/agentMessage/delta",
            raw: "hello",
            payload: { text: "hello" },
            threadId: "thread-1",
            turnId: "turn-1",
            receivedAt: "2026-03-31T00:00:00.000Z",
          },
        ]),
      );

    const client = new RelayClient({
      baseUrl: "http://relay.test",
      fetch: fetchMock,
    });

    await expect(client.listSessions()).resolves.toEqual([
      expect.objectContaining({
        sessionId: "session-1",
        displayName: "workspace-a",
      }),
    ]);

    await expect(client.getSession("session-1")).resolves.toEqual(
      expect.objectContaining({
        sessionId: "session-1",
        turnCount: 2,
      }),
    );

    await expect(client.getHistory("session-1", 5)).resolves.toEqual([
      expect.objectContaining({
        classification: "agentMessage",
        raw: "hello",
      }),
    ]);

    expect(fetchMock.mock.calls).toEqual([
      ["http://relay.test/sessions", expect.objectContaining({ method: "GET" })],
      [
        "http://relay.test/sessions/session-1",
        expect.objectContaining({ method: "GET" }),
      ],
      [
        "http://relay.test/sessions/session-1/history?limit=5",
        expect.objectContaining({ method: "GET" }),
      ],
    ]);
  });

  it("wraps prompt, approval, interrupt, attach, and detach REST calls", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse({ ok: true }))
      .mockResolvedValueOnce(jsonResponse({ ok: true }))
      .mockResolvedValueOnce(jsonResponse({ ok: true }))
      .mockResolvedValueOnce(
        jsonResponse(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            attachedUser: "user-1",
          }),
        ),
      )
      .mockResolvedValueOnce(
        jsonResponse(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            attachedUser: null,
          }),
        ),
      );

    const client = new RelayClient({
      baseUrl: "http://relay.test",
      fetch: fetchMock,
    });

    await client.sendPrompt("session-1", "hello relay");
    await client.sendApproval("session-1", "request-1", true);
    await client.interrupt("session-1");

    await expect(client.attach("session-1", "user-1")).resolves.toEqual(
      expect.objectContaining({
        attachedUser: "user-1",
      }),
    );

    await expect(client.detach("session-1")).resolves.toEqual(
      expect.objectContaining({
        attachedUser: null,
      }),
    );

    expect(fetchMock.mock.calls).toEqual([
      [
        "http://relay.test/sessions/session-1/input",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            type: "prompt",
            content: "hello relay",
          }),
        }),
      ],
      [
        "http://relay.test/sessions/session-1/input",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            type: "approval",
            requestId: "request-1",
            approved: true,
          }),
        }),
      ],
      [
        "http://relay.test/sessions/session-1/interrupt",
        expect.objectContaining({
          method: "POST",
        }),
      ],
      [
        "http://relay.test/sessions/session-1/attach",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            userId: "user-1",
          }),
        }),
      ],
      [
        "http://relay.test/sessions/session-1/detach",
        expect.objectContaining({
          method: "POST",
        }),
      ],
    ]);
  });

  it("surfaces API errors with status codes", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      jsonResponse(
        {
          error: "Session not found",
        },
        { status: 404 },
      ),
    );

    const client = new RelayClient({
      baseUrl: "http://relay.test",
      fetch: fetchMock,
    });

    await expect(client.getSession("missing")).rejects.toEqual(
      expect.objectContaining<Partial<RelayClientError>>({
        message: "Session not found",
        status: 404,
      }),
    );
  });

  it("rejects malformed API responses", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValue(jsonResponse({ unexpected: true }));

    const client = new RelayClient({
      baseUrl: "http://relay.test",
      fetch: fetchMock,
    });

    await expect(client.listSessions()).rejects.toThrow(
      /Invalid relay response/,
    );
  });
});

function createSessionDetail(
  overrides: Partial<{
    sessionId: string;
    displayName: string;
    state: "idle" | "executing" | "waitingApproval";
    online: boolean;
    turnCount: number;
    threadId: string | null;
    turnId: string | null;
    attachedUser: string | null;
    metadata: Record<string, unknown>;
    graceExpiresAt: string | null;
    historySize: number;
    lastMessage: {
      direction: "in" | "out";
      classification:
        | "agentMessage"
        | "toolCall"
        | "serverRequest"
        | "turnLifecycle"
        | "threadLifecycle"
        | "unknown";
      raw: string;
      receivedAt: string;
      method?: string;
      payload?: unknown;
      threadId?: string | null;
      turnId?: string | null;
    } | null;
  }> = {},
) {
  return {
    sessionId: overrides.sessionId ?? "session-1",
    displayName: overrides.displayName ?? "workspace-a",
    state: overrides.state ?? "idle",
    online: overrides.online ?? true,
    turnCount: overrides.turnCount ?? 0,
    threadId: overrides.threadId ?? null,
    turnId: overrides.turnId ?? null,
    attachedUser: overrides.attachedUser ?? null,
    metadata: overrides.metadata ?? {},
    graceExpiresAt: overrides.graceExpiresAt ?? null,
    historySize: overrides.historySize ?? 0,
    lastMessage: overrides.lastMessage ?? null,
  };
}

function jsonResponse(body: unknown, init?: ResponseInit): Response {
  return new Response(JSON.stringify(body), {
    status: init?.status ?? 200,
    headers: {
      "content-type": "application/json",
      ...(init?.headers ?? {}),
    },
  });
}
