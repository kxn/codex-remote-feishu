import { afterEach, describe, expect, it } from "vitest";
import { WebSocket } from "ws";

import {
  readRelayServerConfig,
  startRelayServer,
  type StartedRelayServer,
} from "./relay-server.js";
import type { SessionDetail } from "./session-registry.js";

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
      classification: "agentMessage",
      method: "item/agentMessage/delta",
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
});

async function connect(url: string): Promise<WebSocket> {
  return new Promise((resolve, reject) => {
    const socket = new WebSocket(url);
    socket.once("open", () => resolve(socket));
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
  return new Promise((resolve, reject) => {
    const onMessage = (data: WebSocket.RawData) => {
      cleanup();
      resolve(JSON.parse(data.toString()));
    };
    const onError = (error: Error) => {
      cleanup();
      reject(error);
    };
    const onClose = () => {
      cleanup();
      reject(new Error("Socket closed before message was received"));
    };
    const cleanup = () => {
      client.off("message", onMessage);
      client.off("error", onError);
      client.off("close", onClose);
    };

    client.on("message", onMessage);
    client.once("error", onError);
    client.once("close", onClose);
  });
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
