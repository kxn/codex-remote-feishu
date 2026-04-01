import { afterEach, describe, expect, it } from "vitest";
import { WebSocket } from "ws";

import {
  readRelayServerConfig,
  startRelayServer,
  type StartedRelayServer,
  type UserSessionEvent,
} from "./relay-server.js";
import type { SessionDetail } from "./session-registry.js";

interface SocketTracker {
  queue: unknown[];
  closedError: Error | null;
  waiters: Array<{
    resolve: (message: unknown) => void;
    reject: (error: Error) => void;
  }>;
}

const socketTrackers = new WeakMap<WebSocket, SocketTracker>();

describe("relay server", () => {
  let server: StartedRelayServer | undefined;

  afterEach(async () => {
    if (server) {
      await server.close();
      server = undefined;
    }
  });

  it("starts empty and exposes JSON REST responses", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 50,
      historyLimit: 5,
    });

    const response = await fetch(`${server.apiBaseUrl}/sessions`);

    expect(response.status).toBe(200);
    expect(response.headers.get("content-type")).toContain("application/json");
    expect(await response.json()).toEqual([]);
  });

  it("registers wrapper sessions and exposes them over REST", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 50,
      historyLimit: 5,
    });

    const client = await connect(server.wsUrl);
    await sendJson(client, {
      type: "register",
      sessionId: "session-1",
      displayName: "workspace-a",
      metadata: {
        version: "0.1.0",
        workspacePath: "/tmp/workspace-a",
      },
    });

    expect(await nextJsonMessage(client)).toEqual(
      expect.objectContaining({
        type: "registered",
        sessionId: "session-1",
        resumed: false,
      }),
    );

    const sessionsResponse = await fetch(`${server.apiBaseUrl}/sessions`);
    expect(sessionsResponse.status).toBe(200);
    expect(await sessionsResponse.json()).toEqual([
      expect.objectContaining({
        sessionId: "session-1",
        displayName: "workspace-a",
        state: "idle",
        online: true,
        turnCount: 0,
        graceExpiresAt: null,
      }),
    ]);

    const detailResponse = await fetch(`${server.apiBaseUrl}/sessions/session-1`);
    expect(detailResponse.status).toBe(200);
    expect(await detailResponse.json()).toEqual(
      expect.objectContaining({
        sessionId: "session-1",
        historySize: 0,
        lastMessage: null,
      }),
    );

    client.close();
    await waitForClose(client);
  });

  it("tracks execution state transitions and turn counts", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 50,
      historyLimit: 10,
    });

    const client = await connect(server.wsUrl);
    await register(client);

    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "turnLifecycle",
      method: "turn/started",
      raw: '{"method":"turn/started"}',
    });
    expect((await fetchSession(server.apiBaseUrl, "session-1")).state).toBe(
      "executing",
    );

    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "serverRequest",
      method: "serverRequest/approval",
      raw: '{"method":"serverRequest/approval"}',
    });
    expect((await fetchSession(server.apiBaseUrl, "session-1")).state).toBe(
      "waitingApproval",
    );

    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "turnLifecycle",
      method: "turn/completed",
      raw: '{"method":"turn/completed"}',
    });

    expect(await fetchSession(server.apiBaseUrl, "session-1")).toEqual(
      expect.objectContaining({
        state: "idle",
        turnCount: 1,
      }),
    );

    client.close();
    await waitForClose(client);
  });

  it("ignores approval messages while idle", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 50,
      historyLimit: 10,
    });

    const client = await connect(server.wsUrl);
    await register(client);

    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "serverRequest",
      method: "serverRequest/approval",
      raw: '{"method":"serverRequest/approval"}',
    });

    expect(await fetchSession(server.apiBaseUrl, "session-1")).toEqual(
      expect.objectContaining({
        state: "idle",
        turnCount: 0,
        historySize: 0,
        lastMessage: null,
      }),
    );

    client.close();
    await waitForClose(client);
  });

  it("keeps disconnected sessions during grace period, resumes on reconnect, and evicts after timeout", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 60,
      historyLimit: 10,
    });

    const initialClient = await connect(server.wsUrl);
    await register(initialClient);
    await sendJson(initialClient, {
      type: "message",
      sessionId: "session-1",
      classification: "turnLifecycle",
      method: "turn/started",
      raw: '{"method":"turn/started"}',
    });
    await sendJson(initialClient, {
      type: "message",
      sessionId: "session-1",
      classification: "serverRequest",
      method: "serverRequest/approval",
      raw: '{"method":"serverRequest/approval"}',
    });

    initialClient.close();
    await waitForClose(initialClient);

    const duringGrace = await fetchSession(server.apiBaseUrl, "session-1");
    expect(duringGrace).toEqual(
      expect.objectContaining({
        online: false,
        state: "waitingApproval",
      }),
    );
    expect(duringGrace.graceExpiresAt).not.toBeNull();

    const resumedClient = await connect(server.wsUrl);
    await sendJson(resumedClient, {
      type: "register",
      sessionId: "session-1",
      displayName: "workspace-a",
      metadata: {},
    });
    expect(await nextJsonMessage(resumedClient)).toEqual(
      expect.objectContaining({
        type: "registered",
        sessionId: "session-1",
        resumed: true,
      }),
    );

    const resumedSession = await fetchSession(server.apiBaseUrl, "session-1");
    expect(resumedSession).toEqual(
      expect.objectContaining({
        online: true,
        state: "waitingApproval",
        graceExpiresAt: null,
      }),
    );

    resumedClient.close();
    await waitForClose(resumedClient);
    await sleep(100);

    const missingResponse = await fetch(`${server.apiBaseUrl}/sessions/session-1`);
    expect(missingResponse.status).toBe(404);
  });

  it("replays detached attach status when a wrapper reconnects after offline detach", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 200,
      historyLimit: 10,
    });

    const initialClient = await connect(server.wsUrl);
    await register(initialClient);

    const attachResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/attach`,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
        },
        body: JSON.stringify({
          userId: "user-1",
        }),
      },
    );
    expect(attachResponse.status).toBe(200);
    expect(await nextJsonMessage(initialClient)).toEqual({
      type: "attach-status-changed",
      attached: true,
      userId: "user-1",
    });

    initialClient.close();
    await waitForClose(initialClient);

    const detachResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/detach`,
      {
        method: "POST",
      },
    );
    expect(detachResponse.status).toBe(200);
    expect(await detachResponse.json()).toEqual(
      expect.objectContaining({
        sessionId: "session-1",
        attachedUser: null,
        online: false,
      }),
    );

    const resumedClient = await connect(server.wsUrl);
    await sendJson(resumedClient, {
      type: "register",
      sessionId: "session-1",
      displayName: "workspace-a",
      metadata: {},
    });
    expect(await nextJsonMessage(resumedClient)).toEqual(
      expect.objectContaining({
        type: "registered",
        sessionId: "session-1",
        resumed: true,
      }),
    );
    await expect(
      Promise.race([
        nextJsonMessage(resumedClient),
        sleep(200).then(() => {
          throw new Error("Timed out waiting for attach-status replay");
        }),
      ]),
    ).resolves.toEqual({
      type: "attach-status-changed",
      attached: false,
    });

    resumedClient.close();
    await waitForClose(resumedClient);
  });

  it("returns history in order and honors the limit query", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 50,
      historyLimit: 2,
    });

    const client = await connect(server.wsUrl);
    await register(client);

    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "turnLifecycle",
      method: "turn/started",
      raw: "first",
    });
    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "serverRequest",
      method: "serverRequest/approval",
      raw: "second",
    });
    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "turnLifecycle",
      method: "turn/completed",
      raw: "third",
    });

    const historyResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/history`,
    );
    expect(historyResponse.status).toBe(200);
    expect(await historyResponse.json()).toEqual([
      expect.objectContaining({ raw: "second" }),
      expect.objectContaining({ raw: "third" }),
    ]);

    const limitedHistory = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/history?limit=1`,
    );
    expect(limitedHistory.status).toBe(200);
    expect(await limitedHistory.json()).toEqual([
      expect.objectContaining({ raw: "third" }),
    ]);

    const invalidLimit = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/history?limit=-1`,
    );
    expect(invalidLimit.status).toBe(400);
    expect(await invalidLimit.json()).toEqual({
      error: "Invalid limit query parameter",
    });

    client.close();
    await waitForClose(client);
  });

  it("handles malformed websocket and REST payloads without crashing", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 50,
      historyLimit: 5,
    });

    const client = await connect(server.wsUrl);

    client.send("{bad json");
    expect(await nextJsonMessage(client)).toEqual({
      type: "error",
      error: "Invalid JSON message",
    });

    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "agentMessage",
      raw: "before register",
    });
    expect(await nextJsonMessage(client)).toEqual({
      type: "error",
      error: "Connection must register before sending messages",
    });

    await sendJson(client, {
      type: "register",
      sessionId: "",
      displayName: "workspace-a",
      metadata: {},
    });
    expect(await nextJsonMessage(client)).toEqual(
      expect.objectContaining({
        type: "error",
        error: "Invalid register message",
      }),
    );

    await register(client);
    await sendJson(client, {
      type: "message",
      sessionId: "other-session",
      classification: "agentMessage",
      raw: "wrong session",
    });
    expect(await nextJsonMessage(client)).toEqual({
      type: "error",
      error: "Session message does not match registered session",
    });

    const malformedBody = await fetch(`${server.apiBaseUrl}/sessions`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
      },
      body: "{bad json",
    });
    expect(malformedBody.status).toBe(400);
    expect(await malformedBody.json()).toEqual({ error: "Invalid JSON body" });

    const stillHealthy = await fetch(`${server.apiBaseUrl}/sessions`);
    expect(stillHealthy.status).toBe(200);
    expect(await stillHealthy.json()).toEqual([
      expect.objectContaining({ sessionId: "session-1" }),
    ]);

    client.close();
    await waitForClose(client);
  });

  it("exposes lifecycle and auto-detach events for polling clients", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 200,
      historyLimit: 10,
    });

    const client = await connect(server.wsUrl);
    await register(client);

    const baselineResponse = await fetch(`${server.apiBaseUrl}/events`);
    expect(baselineResponse.status).toBe(200);
    expect(await baselineResponse.json()).toEqual({
      latestEventId: 0,
      events: [],
    });

    const attachResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/attach`,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
        },
        body: JSON.stringify({
          userId: "user-1",
        }),
      },
    );
    expect(attachResponse.status).toBe(200);
    expect(await nextJsonMessage(client)).toEqual({
      type: "attach-status-changed",
      attached: true,
      userId: "user-1",
    });

    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "turnLifecycle",
      method: "turn/started",
      raw: '{"method":"turn/started"}',
    });
    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "serverRequest",
      method: "serverRequest/approval",
      raw: '{"method":"serverRequest/approval","params":{"id":"req-1"}}',
      payload: {
        method: "serverRequest/approval",
        params: {
          id: "req-1",
        },
      },
    });
    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "turnLifecycle",
      method: "turn/completed",
      raw: '{"method":"turn/completed"}',
    });
    await sendJson(client, {
      type: "auto-detach",
      sessionId: "session-1",
      reason: "local-input",
    });
    expect(await nextJsonMessage(client)).toEqual({
      type: "ack",
      acknowledged: "auto-detach",
    });
    expect(await nextJsonMessage(client)).toEqual({
      type: "attach-status-changed",
      attached: false,
      reason: "local-input",
    });

    const eventsResponse = await fetch(`${server.apiBaseUrl}/events?after=0`);
    expect(eventsResponse.status).toBe(200);
    expect(await eventsResponse.json()).toEqual({
      latestEventId: 3,
      events: [
        expect.objectContaining({
          id: 1,
          type: "input-required",
          sessionId: "session-1",
          displayName: "workspace-a",
          requestId: "req-1",
        }),
        expect.objectContaining({
          id: 2,
          type: "turn-completed",
          sessionId: "session-1",
          displayName: "workspace-a",
          turnCount: 1,
        }),
        expect.objectContaining({
          id: 3,
          type: "auto-detach",
          sessionId: "session-1",
          displayName: "workspace-a",
          userId: "user-1",
          reason: "local-input",
        }),
      ],
    });

    const filteredResponse = await fetch(`${server.apiBaseUrl}/events?after=1`);
    expect(filteredResponse.status).toBe(200);
    expect(await filteredResponse.json()).toEqual({
      latestEventId: 3,
      events: [
        expect.objectContaining({
          id: 2,
          type: "turn-completed",
        }),
        expect.objectContaining({
          id: 3,
          type: "auto-detach",
        }),
      ],
    });

    const invalidAfterResponse = await fetch(
      `${server.apiBaseUrl}/events?after=-1`,
    );
    expect(invalidAfterResponse.status).toBe(400);
    expect(await invalidAfterResponse.json()).toEqual({
      error: "Invalid after query parameter",
    });

    client.close();
    await waitForClose(client);
  });

  it("parses configuration from both relay-specific and manifest environment variables", () => {
    expect(
      readRelayServerConfig({
        RELAY_API_PORT: "9511",
        RELAY_PORT: "9510",
        SESSION_GRACE_PERIOD: "42",
        MESSAGE_BUFFER_SIZE: "7",
      }),
    ).toEqual({
      apiPort: 9511,
      wsPort: 9510,
      gracePeriodMs: 42_000,
      historyLimit: 7,
    });

    expect(
      readRelayServerConfig({
        PORT: "9521",
        WS_PORT: "9520",
      }),
    ).toEqual({
      apiPort: 9521,
      wsPort: 9520,
      gracePeriodMs: 300_000,
      historyLimit: 100,
    });
  });

  it("delivers REST input and interrupts to the correct wrapper and rejects unavailable sessions", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 200,
      historyLimit: 10,
    });

    const sessionA = await connect(server.wsUrl);
    await sendJson(sessionA, {
      type: "register",
      sessionId: "session-a",
      displayName: "workspace-a",
      metadata: {},
    });
    await nextJsonMessage(sessionA);

    const sessionB = await connect(server.wsUrl);
    await sendJson(sessionB, {
      type: "register",
      sessionId: "session-b",
      displayName: "workspace-b",
      metadata: {},
    });
    await nextJsonMessage(sessionB);

    const promptResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-a/input`,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
        },
        body: JSON.stringify({
          type: "prompt",
          content: "hello from bot",
        }),
      },
    );
    expect(promptResponse.status).toBe(200);
    expect(await promptResponse.json()).toEqual({ ok: true });
    expect(await nextJsonMessage(sessionA)).toEqual({
      type: "input",
      content: "hello from bot",
    });
    await expectNoMessage(sessionB, 150);

    const interruptResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-b/interrupt`,
      {
        method: "POST",
      },
    );
    expect(interruptResponse.status).toBe(200);
    expect(await interruptResponse.json()).toEqual({ ok: true });
    expect(await nextJsonMessage(sessionB)).toEqual({
      type: "interrupt",
    });
    await expectNoMessage(sessionA, 150);

    const approvalResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-a/input`,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
        },
        body: JSON.stringify({
          type: "approval",
          requestId: "req-1",
          approved: true,
        }),
      },
    );
    expect(approvalResponse.status).toBe(200);
    expect(await approvalResponse.json()).toEqual({ ok: true });
    expect(await nextJsonMessage(sessionA)).toEqual({
      type: "approval-response",
      requestId: "req-1",
      decision: "accept",
    });
    await expectNoMessage(sessionB, 150);

    const missingSession = await fetch(
      `${server.apiBaseUrl}/sessions/missing/input`,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
        },
        body: JSON.stringify({
          type: "prompt",
          content: "hello",
        }),
      },
    );
    expect(missingSession.status).toBe(404);
    expect(await missingSession.json()).toEqual({ error: "Session not found" });

    sessionA.close();
    await waitForClose(sessionA);

    const offlineResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-a/input`,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
        },
        body: JSON.stringify({
          type: "prompt",
          content: "still there?",
        }),
      },
    );
    expect(offlineResponse.status).toBe(409);
    expect(await offlineResponse.json()).toEqual({
      error: "Session is offline",
    });

    const invalidApprovalResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-b/input`,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
        },
        body: JSON.stringify({
          type: "approval",
          approved: false,
        }),
      },
    );
    expect(invalidApprovalResponse.status).toBe(400);
    expect(await invalidApprovalResponse.json()).toEqual(
      expect.objectContaining({
        error: "Invalid input body",
      }),
    );

    sessionB.close();
    await waitForClose(sessionB);
  });

  it("manages attach state, notifies wrappers, and forwards attached session events to callbacks", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 200,
      historyLimit: 10,
    });

    const client = await connect(server.wsUrl);
    await register(client);

    const events: UserSessionEvent[] = [];
    const unsubscribe = server.subscribeUserEvents("user-1", (event) => {
      events.push(event);
    });

    const attachResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/attach`,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
        },
        body: JSON.stringify({
          userId: "user-1",
        }),
      },
    );
    expect(attachResponse.status).toBe(200);
    expect(await attachResponse.json()).toEqual(
      expect.objectContaining({
        sessionId: "session-1",
        attachedUser: "user-1",
      }),
    );
    expect(await nextJsonMessage(client)).toEqual({
      type: "attach-status-changed",
      attached: true,
      userId: "user-1",
    });

    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "agentMessage",
      method: "item/agentMessage/delta",
      raw: "agent says hi",
      payload: { text: "hi" },
      threadId: "thread-1",
      turnId: "turn-1",
    });
    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "serverRequest",
      method: "serverRequest/approval",
      raw: "approval needed",
      payload: { id: "req-1" },
      threadId: "thread-1",
      turnId: "turn-1",
    });

    await waitForCondition(() => events.length === 2);
    expect(events).toHaveLength(2);
    expect(events).toEqual([
      expect.objectContaining({
        type: "message",
        sessionId: "session-1",
        userId: "user-1",
        message: expect.objectContaining({
          raw: "agent says hi",
          classification: "agentMessage",
        }),
      }),
      expect.objectContaining({
        type: "message",
        sessionId: "session-1",
        userId: "user-1",
        message: expect.objectContaining({
          raw: "approval needed",
          classification: "serverRequest",
        }),
      }),
    ]);

    const secondAttach = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/attach`,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
        },
        body: JSON.stringify({
          userId: "user-2",
        }),
      },
    );
    expect(secondAttach.status).toBe(409);
    expect(await secondAttach.json()).toEqual({
      error: "Session is already attached by another user",
    });

    await sendJson(client, {
      type: "auto-detach",
      sessionId: "session-1",
      reason: "local-input",
    });
    expect(await nextJsonMessage(client)).toEqual({
      type: "ack",
      acknowledged: "auto-detach",
    });
    expect(await nextJsonMessage(client)).toEqual({
      type: "attach-status-changed",
      attached: false,
      reason: "local-input",
    });

    await waitForCondition(() => events.length === 3);
    expect(events.at(-1)).toEqual(
      expect.objectContaining({
        type: "auto-detach",
        sessionId: "session-1",
        userId: "user-1",
        reason: "local-input",
      }),
    );
    expect(await fetchSession(server.apiBaseUrl, "session-1")).toEqual(
      expect.objectContaining({
        attachedUser: null,
        threadId: "thread-1",
        turnId: "turn-1",
      }),
    );

    const detachResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/detach`,
      {
        method: "POST",
      },
    );
    expect(detachResponse.status).toBe(200);
    expect(await detachResponse.json()).toEqual(
      expect.objectContaining({
        attachedUser: null,
      }),
    );
    await expectNoMessage(client, 150);

    unsubscribe();
    client.close();
    await waitForClose(client);
  });

  it("keeps post-detach agent output in history without forwarding it to detached users", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 200,
      historyLimit: 10,
    });

    const client = await connect(server.wsUrl);
    await register(client);

    const events: UserSessionEvent[] = [];
    const unsubscribe = server.subscribeUserEvents("user-1", (event) => {
      events.push(event);
    });

    const attachResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/attach`,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
        },
        body: JSON.stringify({
          userId: "user-1",
        }),
      },
    );
    expect(attachResponse.status).toBe(200);
    expect(await nextJsonMessage(client)).toEqual({
      type: "attach-status-changed",
      attached: true,
      userId: "user-1",
    });

    const detachResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/detach`,
      {
        method: "POST",
      },
    );
    expect(detachResponse.status).toBe(200);
    expect(await detachResponse.json()).toEqual(
      expect.objectContaining({
        attachedUser: null,
      }),
    );
    expect(await nextJsonMessage(client)).toEqual({
      type: "attach-status-changed",
      attached: false,
    });
    await expectNoMessage(client, 150);

    const eventCountAfterDetach = events.length;
    await sendJson(client, {
      type: "message",
      sessionId: "session-1",
      classification: "agentMessage",
      method: "item/agentMessage/delta",
      raw: "post-detach history",
      payload: {
        params: {
          delta: "post-detach history",
        },
      },
      threadId: "thread-1",
      turnId: "turn-1",
    });

    await sleep(50);
    expect(events).toHaveLength(eventCountAfterDetach);

    const historyResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/history`,
    );
    expect(historyResponse.status).toBe(200);
    expect(await historyResponse.json()).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          raw: "post-detach history",
          classification: "agentMessage",
          threadId: "thread-1",
          turnId: "turn-1",
        }),
      ]),
    );

    unsubscribe();
    client.close();
    await waitForClose(client);
  });

  it("exposes lossless per-user message events even when history snapshots evict burst output", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 200,
      historyLimit: 2,
    });

    const client = await connect(server.wsUrl);
    await register(client);

    const attachResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/attach`,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
        },
        body: JSON.stringify({
          userId: "user-1",
        }),
      },
    );
    expect(attachResponse.status).toBe(200);
    const attachedSession = (await attachResponse.json()) as {
      userEventCursor: number;
    };
    expect(attachedSession.userEventCursor).toBeTypeOf("number");
    expect(await nextJsonMessage(client)).toEqual({
      type: "attach-status-changed",
      attached: true,
      userId: "user-1",
    });

    for (const raw of ["burst-1", "burst-2", "burst-3", "burst-4", "burst-5"]) {
      await sendJson(client, {
        type: "message",
        sessionId: "session-1",
        classification: "agentMessage",
        method: "item/agentMessage/delta",
        raw,
        payload: {
          params: {
            delta: raw,
          },
        },
      });
    }

    const userEventsResponse = await fetch(
      `${server.apiBaseUrl}/users/user-1/events?after=${attachedSession.userEventCursor}`,
    );
    expect(userEventsResponse.status).toBe(200);
    expect(await userEventsResponse.json()).toEqual({
      latestEventId: expect.any(Number),
      events: [
        expect.objectContaining({
          type: "message",
          sessionId: "session-1",
          displayName: "workspace-a",
          message: expect.objectContaining({
            raw: "burst-1",
            classification: "agentMessage",
          }),
        }),
        expect.objectContaining({
          type: "message",
          sessionId: "session-1",
          displayName: "workspace-a",
          message: expect.objectContaining({
            raw: "burst-2",
            classification: "agentMessage",
          }),
        }),
        expect.objectContaining({
          type: "message",
          sessionId: "session-1",
          displayName: "workspace-a",
          message: expect.objectContaining({
            raw: "burst-3",
            classification: "agentMessage",
          }),
        }),
        expect.objectContaining({
          type: "message",
          sessionId: "session-1",
          displayName: "workspace-a",
          message: expect.objectContaining({
            raw: "burst-4",
            classification: "agentMessage",
          }),
        }),
        expect.objectContaining({
          type: "message",
          sessionId: "session-1",
          displayName: "workspace-a",
          message: expect.objectContaining({
            raw: "burst-5",
            classification: "agentMessage",
          }),
        }),
      ],
    });

    const historyResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-1/history`,
    );
    expect(historyResponse.status).toBe(200);
    expect((await historyResponse.json()).map((entry: { raw: string }) => entry.raw)).toEqual([
      "burst-4",
      "burst-5",
    ]);

    client.close();
    await waitForClose(client);
  });

  it("keeps message history and callbacks isolated per session", async () => {
    server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: 200,
      historyLimit: 10,
    });

    const sessionA = await connect(server.wsUrl);
    await sendJson(sessionA, {
      type: "register",
      sessionId: "session-a",
      displayName: "workspace-a",
      metadata: {},
    });
    await nextJsonMessage(sessionA);

    const sessionB = await connect(server.wsUrl);
    await sendJson(sessionB, {
      type: "register",
      sessionId: "session-b",
      displayName: "workspace-b",
      metadata: {},
    });
    await nextJsonMessage(sessionB);

    const events: UserSessionEvent[] = [];
    const unsubscribe = server.subscribeUserEvents("user-a", (event) => {
      events.push(event);
    });

    const attachResponse = await fetch(
      `${server.apiBaseUrl}/sessions/session-a/attach`,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
        },
        body: JSON.stringify({
          userId: "user-a",
        }),
      },
    );
    expect(attachResponse.status).toBe(200);
    await nextJsonMessage(sessionA);

    await sendJson(sessionA, {
      type: "message",
      sessionId: "session-a",
      classification: "agentMessage",
      method: "item/agentMessage/delta",
      raw: "only a",
    });
    await sendJson(sessionB, {
      type: "message",
      sessionId: "session-b",
      classification: "agentMessage",
      method: "item/agentMessage/delta",
      raw: "only b",
    });

    await waitForCondition(() => events.length === 1);
    expect(events).toHaveLength(1);
    expect(events[0]).toEqual(
      expect.objectContaining({
        sessionId: "session-a",
        message: expect.objectContaining({
          raw: "only a",
        }),
      }),
    );

    const historyA = await fetch(`${server.apiBaseUrl}/sessions/session-a/history`);
    const historyB = await fetch(`${server.apiBaseUrl}/sessions/session-b/history`);
    expect((await historyA.json()).map((entry: { raw: string }) => entry.raw)).toEqual([
      "only a",
    ]);
    expect((await historyB.json()).map((entry: { raw: string }) => entry.raw)).toEqual([
      "only b",
    ]);

    unsubscribe();
    sessionA.close();
    sessionB.close();
    await Promise.all([waitForClose(sessionA), waitForClose(sessionB)]);
  });
});

async function connect(url: string): Promise<WebSocket> {
  return new Promise((resolve, reject) => {
    const socket = new WebSocket(url);
    socket.once("open", () => {
      ensureSocketTracker(socket);
      resolve(socket);
    });
    socket.once("error", reject);
  });
}

async function register(client: WebSocket): Promise<void> {
  await sendJson(client, {
    type: "register",
    sessionId: "session-1",
    displayName: "workspace-a",
    metadata: {},
  });
  expect(await nextJsonMessage(client)).toEqual(
    expect.objectContaining({
      type: "registered",
      sessionId: "session-1",
    }),
  );
}

async function sendJson(client: WebSocket, payload: unknown): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    client.send(JSON.stringify(payload), (error) => {
      if (error) {
        reject(error);
        return;
      }
      resolve();
    });
  });
}

async function nextJsonMessage(client: WebSocket): Promise<unknown> {
  const tracker = ensureSocketTracker(client);
  if (tracker.queue.length > 0) {
    return tracker.queue.shift();
  }

  if (tracker.closedError) {
    throw tracker.closedError;
  }

  return new Promise((resolve, reject) => {
    tracker.waiters.push({ resolve, reject });
  });
}

async function expectNoMessage(client: WebSocket, waitMs: number): Promise<void> {
  const tracker = ensureSocketTracker(client);
  if (tracker.queue.length > 0) {
    throw new Error(`Unexpected WebSocket message: ${JSON.stringify(tracker.queue[0])}`);
  }

  await new Promise<void>((resolve, reject) => {
    const waiter = {
      resolve: (message: unknown) => {
        cleanup();
        reject(
          new Error(`Unexpected WebSocket message: ${JSON.stringify(message)}`),
        );
      },
      reject: (error: Error) => {
        cleanup();
        if (error.message === "Socket closed before message was received") {
          resolve();
          return;
        }
        reject(error);
      },
    };

    const timer = setTimeout(() => {
      cleanup();
      resolve();
    }, waitMs);

    const cleanup = () => {
      clearTimeout(timer);
      const index = tracker.waiters.indexOf(waiter);
      if (index >= 0) {
        tracker.waiters.splice(index, 1);
      }
    };

    tracker.waiters.push(waiter);
  });
}

function ensureSocketTracker(client: WebSocket): SocketTracker {
  const existing = socketTrackers.get(client);
  if (existing) {
    return existing;
  }

  const tracker: SocketTracker = {
    queue: [],
    closedError: null,
    waiters: [],
  };

  client.on("message", (data) => {
    const parsed = JSON.parse(data.toString()) as unknown;
    const waiter = tracker.waiters.shift();
    if (waiter) {
      waiter.resolve(parsed);
      return;
    }
    tracker.queue.push(parsed);
  });

  client.on("error", (error) => {
    tracker.closedError = error;
    while (tracker.waiters.length > 0) {
      tracker.waiters.shift()?.reject(error);
    }
  });

  client.on("close", () => {
    tracker.closedError = new Error("Socket closed before message was received");
    while (tracker.waiters.length > 0) {
      tracker.waiters.shift()?.reject(tracker.closedError);
    }
  });

  socketTrackers.set(client, tracker);
  return tracker;
}

async function fetchSession(
  baseUrl: string,
  sessionId: string,
): Promise<SessionDetail> {
  const response = await fetch(`${baseUrl}/sessions/${sessionId}`);
  expect(response.status).toBe(200);
  return (await response.json()) as SessionDetail;
}

async function waitForClose(client: WebSocket): Promise<void> {
  if (client.readyState === WebSocket.CLOSED) {
    return;
  }

  await new Promise<void>((resolve) => {
    client.once("close", () => resolve());
  });
}

async function sleep(milliseconds: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, milliseconds));
}

async function waitForCondition(
  predicate: () => boolean,
  timeoutMs = 1_000,
): Promise<void> {
  const startedAt = Date.now();
  while (!predicate()) {
    if (Date.now() - startedAt > timeoutMs) {
      throw new Error("Timed out waiting for condition");
    }
    await sleep(10);
  }
}
