import { describe, expect, it, vi } from "vitest";

import { RelayClient, RelayClientError } from "./relay.js";

describe("RelayClient", () => {
  it("parses real relay session list summaries and wraps detail/history APIs", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        jsonResponse([
          createSessionSummary({
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
      createSessionSummary({
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

  it("lists relay events for polling clients", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        jsonResponse({
          latestEventId: 4,
          events: [],
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          latestEventId: 6,
          events: [
            {
              type: "turn-completed",
              id: 5,
              occurredAt: "2026-03-31T00:00:01.000Z",
              sessionId: "session-1",
              displayName: "workspace-a",
              turnCount: 3,
            },
            {
              type: "auto-detach",
              id: 6,
              occurredAt: "2026-03-31T00:00:02.000Z",
              sessionId: "session-1",
              displayName: "workspace-a",
              userId: "user-1",
              reason: "local-input",
            },
          ],
        }),
      );

    const client = new RelayClient({
      baseUrl: "http://relay.test",
      fetch: fetchMock,
    });

    await expect(client.listEvents()).resolves.toEqual({
      latestEventId: 4,
      events: [],
    });

    await expect(client.listEvents(4)).resolves.toEqual({
      latestEventId: 6,
      events: [
        expect.objectContaining({
          type: "turn-completed",
          sessionId: "session-1",
          turnCount: 3,
        }),
        expect.objectContaining({
          type: "auto-detach",
          sessionId: "session-1",
          userId: "user-1",
          reason: "local-input",
        }),
      ],
    });

    expect(fetchMock.mock.calls).toEqual([
      ["http://relay.test/events", expect.objectContaining({ method: "GET" })],
      [
        "http://relay.test/events?after=4",
        expect.objectContaining({ method: "GET" }),
      ],
    ]);
  });

  it("lists per-user relay events for lossless attachment forwarding", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        jsonResponse({
          latestEventId: 3,
          events: [
            {
              type: "message",
              id: 2,
              occurredAt: "2026-03-31T00:00:01.000Z",
              userId: "user-1",
              sessionId: "session-1",
              displayName: "workspace-a",
              message: {
                direction: "out",
                classification: "agentMessage",
                method: "item/agentMessage/delta",
                raw: "hello",
                payload: {
                  params: {
                    delta: "hello",
                  },
                },
                receivedAt: "2026-03-31T00:00:01.000Z",
              },
            },
            {
              type: "auto-detach",
              id: 3,
              occurredAt: "2026-03-31T00:00:02.000Z",
              userId: "user-1",
              sessionId: "session-1",
              displayName: "workspace-a",
              reason: "local-input",
            },
          ],
        }),
      );

    const client = new RelayClient({
      baseUrl: "http://relay.test",
      fetch: fetchMock,
    });

    await expect(client.listUserEvents("user-1", 1)).resolves.toEqual({
      latestEventId: 3,
      events: [
        expect.objectContaining({
          type: "message",
          sessionId: "session-1",
          message: expect.objectContaining({
            classification: "agentMessage",
            raw: "hello",
          }),
        }),
        expect.objectContaining({
          type: "auto-detach",
          sessionId: "session-1",
          reason: "local-input",
        }),
      ],
    });

    expect(fetchMock.mock.calls).toEqual([
      [
        "http://relay.test/users/user-1/events?after=1",
        expect.objectContaining({ method: "GET" }),
      ],
    ]);
  });

  it("parses session-offline per-user relay events", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValueOnce(
      jsonResponse({
        latestEventId: 4,
        events: [
          {
            type: "session-offline",
            id: 4,
            occurredAt: "2026-03-31T00:00:03.000Z",
            userId: "user-1",
            sessionId: "session-1",
            displayName: "workspace-a",
            graceExpiresAt: "2026-03-31T00:05:03.000Z",
          },
        ],
      }),
    );

    const client = new RelayClient({
      baseUrl: "http://relay.test",
      fetch: fetchMock,
    });

    await expect(client.listUserEvents("user-1", 3)).resolves.toEqual({
      latestEventId: 4,
      events: [
        expect.objectContaining({
          type: "session-offline",
          userId: "user-1",
          sessionId: "session-1",
          displayName: "workspace-a",
          graceExpiresAt: "2026-03-31T00:05:03.000Z",
        }),
      ],
    });

    expect(fetchMock.mock.calls).toEqual([
      [
        "http://relay.test/users/user-1/events?after=3",
        expect.objectContaining({ method: "GET" }),
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

  it("maps transport failures to a stable relay-unavailable error", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockRejectedValue(new TypeError("fetch failed"));

    const client = new RelayClient({
      baseUrl: "http://relay.test",
      fetch: fetchMock,
    });

    await expect(client.listSessions()).rejects.toEqual(
      expect.objectContaining<Partial<RelayClientError>>({
        message: "Relay server is unavailable, please try again later.",
        status: 503,
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
    userEventCursor: number;
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
    userEventCursor: overrides.userEventCursor ?? 0,
    lastMessage: overrides.lastMessage ?? null,
  };
}

function createSessionSummary(
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
