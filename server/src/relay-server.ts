import http from "node:http";
import type { AddressInfo } from "node:net";

import express, {
  type NextFunction,
  type Request,
  type Response,
} from "express";
import { WebSocket, WebSocketServer } from "ws";
import { z } from "zod";
import type { ApprovalRequestId, RelayEvent, RelayEventBatch } from "shared";

import {
  SessionRegistry,
  type MessageClassification,
  type MessageDirection,
  type SessionConnection,
  type SessionDetail,
  type SessionHistoryEntry,
} from "./session-registry.js";

const messageClassificationSchema = z.enum([
  "agentMessage",
  "toolCall",
  "serverRequest",
  "turnLifecycle",
  "threadLifecycle",
  "unknown",
]);

const messageDirectionSchema = z.enum(["in", "out"]);

const registerMessageSchema = z.object({
  type: z.literal("register"),
  sessionId: z.string().min(1),
  displayName: z.string().min(1),
  metadata: z.record(z.unknown()).default({}),
});

const sessionMessageSchema = z.object({
  type: z.literal("message"),
  sessionId: z.string().min(1),
  direction: messageDirectionSchema.default("out"),
  classification: messageClassificationSchema,
  method: z.string().min(1).optional(),
  raw: z.string(),
  payload: z.unknown().optional(),
  threadId: z.string().nullable().optional(),
  turnId: z.string().nullable().optional(),
});

const autoDetachMessageSchema = z.object({
  type: z.literal("auto-detach"),
  sessionId: z.string().min(1),
  reason: z.string().min(1),
});

const sessionInputSchema = z.union([
  z.object({
    type: z.literal("prompt"),
    content: z.string().min(1),
  }),
  z.object({
    type: z.literal("approval"),
    approved: z.boolean(),
    requestId: z.union([z.string().min(1), z.number().finite()]),
  }),
  z.object({
    method: z.string().min(1),
    params: z.unknown().optional(),
    content: z.string().optional(),
  }),
]);

const APPROVAL_RESPONSE_MESSAGE_TYPE = "approval-response" as const;
const RELAY_EVENT_LOG_LIMIT = 1_000;

const attachBodySchema = z.object({
  userId: z.string().min(1),
});

export interface RelayServerConfig {
  apiPort: number;
  wsPort: number;
  gracePeriodMs: number;
  historyLimit: number;
}

export interface UserSessionMessageEvent {
  type: "message";
  userId: string;
  sessionId: string;
  session: SessionDetail;
  message: SessionHistoryEntry;
}

export interface UserSessionAutoDetachEvent {
  type: "auto-detach";
  userId: string;
  sessionId: string;
  session: SessionDetail;
  reason: string;
}

export type UserSessionEvent =
  | UserSessionMessageEvent
  | UserSessionAutoDetachEvent;

interface UserEventBase {
  id: number;
  occurredAt: string;
  userId: string;
  sessionId: string;
  displayName: string;
}

interface UserMessageEvent extends UserEventBase {
  type: "message";
  message: SessionHistoryEntry;
}

interface UserAutoDetachEvent extends UserEventBase {
  type: "auto-detach";
  reason: string;
}

interface UserEventBatch {
  latestEventId: number;
  events: Array<UserMessageEvent | UserAutoDetachEvent>;
}

type UserEventRecord = UserMessageEvent | UserAutoDetachEvent;

type UserSessionCallback = (event: UserSessionEvent) => void;
type RelayTurnCompletedEventDraft = Omit<
  Extract<RelayEvent, { type: "turn-completed" }>,
  "id" | "occurredAt"
>;
type RelayInputRequiredEventDraft = Omit<
  Extract<RelayEvent, { type: "input-required" }>,
  "id" | "occurredAt"
>;
type RelayAutoDetachEventDraft = Omit<
  Extract<RelayEvent, { type: "auto-detach" }>,
  "id" | "occurredAt"
>;
type RelayEventDraft =
  | RelayTurnCompletedEventDraft
  | RelayInputRequiredEventDraft
  | RelayAutoDetachEventDraft;

export interface StartedRelayServer {
  apiPort: number;
  wsPort: number;
  apiBaseUrl: string;
  wsUrl: string;
  subscribeUserEvents: (
    userId: string,
    callback: UserSessionCallback,
  ) => () => void;
  close: () => Promise<void>;
}

export async function startRelayServer(
  config: RelayServerConfig,
): Promise<StartedRelayServer> {
  const registry = new SessionRegistry({
    gracePeriodMs: config.gracePeriodMs,
    historyLimit: config.historyLimit,
  });

  const userCallbacks = new Map<string, Set<UserSessionCallback>>();
  const relayEvents: RelayEvent[] = [];
  const userEventLogs = new Map<string, UserEventRecord[]>();
  let latestEventId = 0;
  let latestUserEventId = 0;

  const app = express();
  app.disable("x-powered-by");
  app.use(express.json());

  app.get("/events", (request, response) => {
    const parsedAfter = parseAfterQuery(request.query.after);
    if (parsedAfter === "invalid") {
      response.status(400).json({ error: "Invalid after query parameter" });
      return;
    }

    response.json(
      buildRelayEventBatch(relayEvents, latestEventId, parsedAfter),
    );
  });

  app.get("/users/:id/events", (request, response) => {
    const parsedAfter = parseAfterQuery(request.query.after);
    if (parsedAfter === "invalid") {
      response.status(400).json({ error: "Invalid after query parameter" });
      return;
    }

    response.json(
      buildUserEventBatch(
        userEventLogs,
        request.params.id,
        latestUserEventId,
        parsedAfter,
      ),
    );
  });

  app.get("/sessions", (_request, response) => {
    response.json(registry.listSessions());
  });

  app.get("/sessions/:id", (request, response) => {
    const session = registry.getSession(request.params.id);
    if (!session) {
      response.status(404).json({ error: "Session not found" });
      return;
    }

    response.json(session);
  });

  app.get("/sessions/:id/history", (request, response) => {
    const session = registry.getSession(request.params.id);
    if (!session) {
      response.status(404).json({ error: "Session not found" });
      return;
    }

    const parsedLimit = parseLimitQuery(request.query.limit);
    if (parsedLimit === "invalid") {
      response.status(400).json({ error: "Invalid limit query parameter" });
      return;
    }

    response.json(registry.getHistory(request.params.id, parsedLimit));
  });

  app.post("/sessions/:id/input", (request, response) => {
    const session = registry.getSession(request.params.id);
    if (!session) {
      response.status(404).json({ error: "Session not found" });
      return;
    }

    const input = sessionInputSchema.safeParse(request.body);
    if (!input.success) {
      response.status(400).json({
        error: "Invalid input body",
        details: input.error.flatten(),
      });
      return;
    }

    const payload = buildRelayInputPayload(input.data);
    const delivered = registry.deliverToSession(request.params.id, payload);
    if (delivered === "offline") {
      response.status(409).json({ error: "Session is offline" });
      return;
    }

    response.json({ ok: true });
  });

  app.post("/sessions/:id/interrupt", (request, response) => {
    const delivered = registry.deliverToSession(request.params.id, {
      type: "interrupt",
    });

    if (delivered === "not_found") {
      response.status(404).json({ error: "Session not found" });
      return;
    }

    if (delivered === "offline") {
      response.status(409).json({ error: "Session is offline" });
      return;
    }

    response.json({ ok: true });
  });

  app.post("/sessions/:id/attach", (request, response) => {
    const attached = attachBodySchema.safeParse(request.body);
    if (!attached.success) {
      response.status(400).json({
        error: "Invalid attach body",
        details: attached.error.flatten(),
      });
      return;
    }

    const result = registry.attachUser(request.params.id, attached.data.userId);
    if (result.status === "not_found") {
      response.status(404).json({ error: "Session not found" });
      return;
    }

    if (result.status === "offline") {
      response.status(409).json({ error: "Session is offline" });
      return;
    }

    if (result.status === "conflict") {
      response.status(409).json({
        error: "Session is already attached by another user",
      });
      return;
    }

    if (result.previousUser !== attached.data.userId) {
      notifyAttachmentStatus(registry, request.params.id, {
        attached: true,
        userId: attached.data.userId,
      });
    }

    response.json({
      ...result.session,
      userEventCursor: latestUserEventId,
    });
  });

  app.post("/sessions/:id/detach", (request, response) => {
    const result = registry.detachUser(request.params.id);
    if (result.status === "not_found") {
      response.status(404).json({ error: "Session not found" });
      return;
    }

    if (result.previousUser) {
      notifyAttachmentStatus(registry, request.params.id, {
        attached: false,
      });
    }

    response.json(result.session);
  });

  app.use(
    (
      error: unknown,
      _request: Request,
      response: Response,
      next: NextFunction,
    ) => {
      if (isBodyParseError(error)) {
        response.status(400).json({ error: "Invalid JSON body" });
        return;
      }

      next(error);
    },
  );

  const httpServer = await listenHttpServer(app, config.apiPort);

  const websocketServer = new WebSocketServer({ port: config.wsPort });
  await onceWebSocketListening(websocketServer);
  let shuttingDown = false;

  websocketServer.on("connection", (socket) => {
    let sessionId: string | undefined;
    const connection: SessionConnection = {
      close: () => {
        if (
          socket.readyState === WebSocket.OPEN ||
          socket.readyState === WebSocket.CONNECTING
        ) {
          socket.close(1000, "Replaced by a newer connection");
        }
      },
      send: (payload) => sendJson(socket, payload),
    };

    socket.on("message", (data, isBinary) => {
      const raw = isBinary ? data.toString() : data.toString();
      const parsed = safeParseJson(raw);
      if (!parsed.ok) {
        sendJson(socket, {
          type: "error",
          error: "Invalid JSON message",
        });
        return;
      }

      const type = parsed.value.type;
      if (type === "register") {
        const registration = registerMessageSchema.safeParse(parsed.value);
        if (!registration.success) {
          sendJson(socket, {
            type: "error",
            error: "Invalid register message",
            details: registration.error.flatten(),
          });
          return;
        }

        if (sessionId) {
          sendJson(socket, {
            type: "error",
            error: "Connection is already registered",
          });
          return;
        }

        sessionId = registration.data.sessionId;
        const result = registry.register({
          sessionId: registration.data.sessionId,
          displayName: registration.data.displayName,
          metadata: registration.data.metadata,
          connection,
        });

        sendJson(socket, {
          type: "registered",
          sessionId: registration.data.sessionId,
          resumed: result.resumed,
        });

        if (result.resumed || result.session.attachedUser) {
          notifyAttachmentStatus(registry, registration.data.sessionId, {
            attached: result.session.attachedUser !== null,
            ...(result.session.attachedUser
              ? { userId: result.session.attachedUser }
              : {}),
          });
        }
        return;
      }

      if (type === "message") {
        if (!sessionId) {
          sendJson(socket, {
            type: "error",
            error: "Connection must register before sending messages",
          });
          return;
        }

        const message = sessionMessageSchema.safeParse(parsed.value);
        if (!message.success) {
          sendJson(socket, {
            type: "error",
            error: "Invalid session message",
            details: message.error.flatten(),
          });
          return;
        }

        if (message.data.sessionId !== sessionId) {
          sendJson(socket, {
            type: "error",
            error: "Session message does not match registered session",
          });
          return;
        }

        const previousSession = registry.getSession(sessionId);
        const entry = registry.recordMessage(sessionId, {
          direction: message.data.direction as MessageDirection,
          classification: message.data.classification as MessageClassification,
          method: message.data.method,
          raw: message.data.raw,
          payload: message.data.payload,
          threadId: message.data.threadId,
          turnId: message.data.turnId,
        });

        if (entry) {
          const nextSession = registry.getSession(sessionId);
          if (nextSession) {
            const relayEvent = createRelayEventFromMessage(
              previousSession,
              nextSession,
              entry,
            );
            if (relayEvent) {
              appendRelayEvent(
                relayEvents,
                relayEvent,
                () => {
                  latestEventId += 1;
                  return latestEventId;
                },
              );
            }
          }
          emitAttachedUserMessage(
            registry,
            userCallbacks,
            userEventLogs,
            sessionId,
            entry,
            () => {
              latestUserEventId += 1;
              return latestUserEventId;
            },
          );
        }
        return;
      }

      if (type === "auto-detach") {
        if (!sessionId) {
          sendJson(socket, {
            type: "error",
            error: "Connection must register before sending auto-detach events",
          });
          return;
        }

        const autoDetach = autoDetachMessageSchema.safeParse(parsed.value);
        if (!autoDetach.success) {
          sendJson(socket, {
            type: "error",
            error: "Invalid auto-detach message",
            details: autoDetach.error.flatten(),
          });
          return;
        }

        if (autoDetach.data.sessionId !== sessionId) {
          sendJson(socket, {
            type: "error",
            error: "Auto-detach session does not match registered session",
          });
          return;
        }

        const detached = registry.detachUser(sessionId);
        sendJson(socket, {
          type: "ack",
          acknowledged: "auto-detach",
        });

        if (
          detached.status === "ok" &&
          detached.previousUser &&
          detached.session
        ) {
          appendRelayEvent(
            relayEvents,
            {
              type: "auto-detach",
              userId: detached.previousUser,
              sessionId,
              displayName: detached.session.displayName,
              reason: autoDetach.data.reason,
            },
            () => {
              latestEventId += 1;
              return latestEventId;
            },
          );

          notifyAttachmentStatus(registry, sessionId, {
            attached: false,
            reason: autoDetach.data.reason,
          });

          emitUserEvent(userCallbacks, detached.previousUser, {
            type: "auto-detach",
            userId: detached.previousUser,
            sessionId,
            session: detached.session,
            reason: autoDetach.data.reason,
          }, userEventLogs, () => {
            latestUserEventId += 1;
            return latestUserEventId;
          });
        }
        return;
      }

      sendJson(socket, {
        type: "error",
        error: "Unsupported message type",
      });
    });

    socket.on("close", () => {
      if (!shuttingDown && sessionId) {
        registry.disconnect(sessionId, connection);
      }
    });
  });

  let closed = false;

  return {
    apiPort: getPort(httpServer),
    wsPort: getPort(websocketServer),
    apiBaseUrl: `http://127.0.0.1:${getPort(httpServer)}`,
    wsUrl: `ws://127.0.0.1:${getPort(websocketServer)}`,
    subscribeUserEvents: (userId, callback) => {
      const callbacks = userCallbacks.get(userId) ?? new Set<UserSessionCallback>();
      callbacks.add(callback);
      userCallbacks.set(userId, callbacks);

      return () => {
        const current = userCallbacks.get(userId);
        if (!current) {
          return;
        }
        current.delete(callback);
        if (current.size === 0) {
          userCallbacks.delete(userId);
        }
      };
    },
    close: async () => {
      if (closed) {
        return;
      }

      closed = true;
      shuttingDown = true;
      registry.dispose();

      for (const client of websocketServer.clients) {
        client.close();
      }

      await Promise.all([
        closeWebSocketServer(websocketServer),
        closeHttpServer(httpServer),
      ]);
    },
  };
}

export function readRelayServerConfig(
  environment: NodeJS.ProcessEnv = process.env,
): RelayServerConfig {
  return {
    apiPort: readInteger(environment, ["RELAY_API_PORT", "PORT"], 9501),
    wsPort: readInteger(environment, ["RELAY_PORT", "WS_PORT"], 9500),
    gracePeriodMs:
      readInteger(environment, ["SESSION_GRACE_PERIOD"], 300) * 1_000,
    historyLimit: readInteger(environment, ["MESSAGE_BUFFER_SIZE"], 100),
  };
}

async function listenHttpServer(
  app: express.Express,
  port: number,
): Promise<http.Server> {
  const server = http.createServer(app);
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, () => {
      server.off("error", reject);
      resolve();
    });
  });
  return server;
}

async function onceWebSocketListening(server: WebSocketServer): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.once("listening", resolve);
    server.once("error", reject);
  });
}

function sendJson(socket: WebSocket, payload: unknown): boolean {
  if (socket.readyState !== WebSocket.OPEN) {
    return false;
  }

  try {
    socket.send(JSON.stringify(payload));
    return true;
  } catch {
    return false;
  }
}

function safeParseJson(
  value: string,
): { ok: true; value: Record<string, unknown> } | { ok: false } {
  try {
    const parsed = JSON.parse(value) as unknown;
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return { ok: true, value: parsed as Record<string, unknown> };
    }
    return { ok: false };
  } catch {
    return { ok: false };
  }
}

function parseLimitQuery(value: unknown): number | undefined | "invalid" {
  return parseNonNegativeIntegerQuery(value);
}

function parseAfterQuery(value: unknown): number | undefined | "invalid" {
  return parseNonNegativeIntegerQuery(value);
}

function parseNonNegativeIntegerQuery(
  value: unknown,
): number | undefined | "invalid" {
  if (value === undefined) {
    return undefined;
  }

  if (Array.isArray(value)) {
    return "invalid";
  }

  const parsed = Number.parseInt(String(value), 10);
  if (!Number.isInteger(parsed) || parsed < 0) {
    return "invalid";
  }

  return parsed;
}

function readInteger(
  environment: NodeJS.ProcessEnv,
  keys: readonly string[],
  fallback: number,
): number {
  for (const key of keys) {
    const raw = environment[key];
    if (raw === undefined) {
      continue;
    }

    const parsed = Number.parseInt(raw, 10);
    if (Number.isInteger(parsed) && parsed >= 0) {
      return parsed;
    }
  }

  return fallback;
}

function isBodyParseError(error: unknown): boolean {
  return (
    error instanceof SyntaxError &&
    typeof error === "object" &&
    error !== null &&
    "status" in error
  );
}

function getPort(server: http.Server | WebSocketServer): number {
  const address = server.address() as AddressInfo | null;
  if (!address || typeof address === "string") {
    throw new Error("Unable to determine bound port");
  }

  return address.port;
}

function closeHttpServer(server: http.Server): Promise<void> {
  return new Promise((resolve, reject) => {
    server.close((error) => {
      if (error) {
        reject(error);
        return;
      }
      resolve();
    });
  });
}

function closeWebSocketServer(server: WebSocketServer): Promise<void> {
  return new Promise((resolve, reject) => {
    server.close((error) => {
      if (error) {
        reject(error);
        return;
      }
      resolve();
    });
  });
}

function buildRelayInputPayload(
  input: z.infer<typeof sessionInputSchema>,
): Record<string, unknown> {
  if ("type" in input && input.type === "prompt") {
    return {
      type: "input",
      content: input.content,
    };
  }

  if ("type" in input && input.type === "approval") {
    return {
      type: APPROVAL_RESPONSE_MESSAGE_TYPE,
      requestId: input.requestId,
      decision: input.approved ? "accept" : "decline",
    };
  }

  return {
    type: "input",
    ...(input.content ? { content: input.content } : {}),
    method: input.method,
    ...(input.params === undefined ? {} : { params: input.params }),
  };
}

function notifyAttachmentStatus(
  registry: SessionRegistry,
  sessionId: string,
  payload: {
    attached: boolean;
    userId?: string;
    reason?: string;
  },
): void {
  registry.deliverToSession(sessionId, {
    type: "attach-status-changed",
    attached: payload.attached,
    ...(payload.userId ? { userId: payload.userId } : {}),
    ...(payload.reason ? { reason: payload.reason } : {}),
  });
}

export { APPROVAL_RESPONSE_MESSAGE_TYPE };

function emitAttachedUserMessage(
  registry: SessionRegistry,
  userCallbacks: Map<string, Set<UserSessionCallback>>,
  userEventLogs: Map<string, UserEventRecord[]>,
  sessionId: string,
  message: SessionHistoryEntry,
  nextId: () => number,
): void {
  const session = registry.getSession(sessionId);
  if (!session?.attachedUser) {
    return;
  }

  if (
    (message.classification !== "agentMessage" &&
      message.classification !== "serverRequest") &&
    message.method !== "turn/completed"
  ) {
    return;
  }

  emitUserEvent(userCallbacks, session.attachedUser, {
    type: "message",
    userId: session.attachedUser,
    sessionId,
    session,
    message,
  }, userEventLogs, nextId);
}

function emitUserEvent(
  userCallbacks: Map<string, Set<UserSessionCallback>>,
  userId: string,
  event: UserSessionEvent,
  userEventLogs: Map<string, UserEventRecord[]>,
  nextId: () => number,
): void {
  const eventLog = userEventLogs.get(userId) ?? [];
  eventLog.push(createUserEventRecord(event, nextId()));
  userEventLogs.set(userId, eventLog);

  const callbacks = userCallbacks.get(userId);
  if (!callbacks) {
    return;
  }

  for (const callback of callbacks) {
    callback(event);
  }
}

function createUserEventRecord(
  event: UserSessionEvent,
  id: number,
): UserEventRecord {
  const occurredAt = new Date().toISOString();

  if (event.type === "message") {
    return {
      type: "message",
      id,
      occurredAt,
      userId: event.userId,
      sessionId: event.sessionId,
      displayName: event.session.displayName,
      message: event.message,
    };
  }

  return {
    type: "auto-detach",
    id,
    occurredAt,
    userId: event.userId,
    sessionId: event.sessionId,
    displayName: event.session.displayName,
    reason: event.reason,
  };
}

function appendRelayEvent(
  relayEvents: RelayEvent[],
  event: RelayEventDraft,
  nextId: () => number,
): void {
  relayEvents.push({
    ...event,
    id: nextId(),
    occurredAt: new Date().toISOString(),
  } as RelayEvent);

  if (relayEvents.length > RELAY_EVENT_LOG_LIMIT) {
    relayEvents.splice(0, relayEvents.length - RELAY_EVENT_LOG_LIMIT);
  }
}

function buildRelayEventBatch(
  relayEvents: RelayEvent[],
  latestEventId: number,
  afterEventId: number | undefined,
): RelayEventBatch {
  return {
    latestEventId,
    events:
      afterEventId === undefined
        ? []
        : relayEvents.filter((event) => event.id > afterEventId),
  };
}

function buildUserEventBatch(
  userEventLogs: Map<string, UserEventRecord[]>,
  userId: string,
  latestEventId: number,
  afterEventId: number | undefined,
): UserEventBatch {
  const existing = userEventLogs.get(userId) ?? [];

  if (afterEventId === undefined) {
    return {
      latestEventId,
      events: existing.slice(),
    };
  }

  const remaining = existing.filter((event) => event.id > afterEventId);
  if (remaining.length === 0) {
    userEventLogs.delete(userId);
  } else if (remaining.length !== existing.length) {
    userEventLogs.set(userId, remaining);
  }

  return {
    latestEventId,
    events: remaining,
  };
}

function createRelayEventFromMessage(
  previousSession: SessionDetail | undefined,
  nextSession: SessionDetail,
  entry: SessionHistoryEntry,
): RelayEventDraft | undefined {
  if (
    entry.method === "turn/completed" &&
    nextSession.turnCount > (previousSession?.turnCount ?? 0)
  ) {
    return {
      type: "turn-completed",
      sessionId: nextSession.sessionId,
      displayName: nextSession.displayName,
      turnCount: nextSession.turnCount,
    };
  }

  if (entry.classification === "serverRequest") {
    const requestId = extractApprovalRequestId(entry);
    return {
      type: "input-required",
      sessionId: nextSession.sessionId,
      displayName: nextSession.displayName,
      ...(requestId === undefined ? {} : { requestId }),
    };
  }

  return undefined;
}

function extractApprovalRequestId(
  entry: SessionHistoryEntry,
): ApprovalRequestId | undefined {
  const payload = asRecord(entry.payload) ?? parseJsonObject(entry.raw);
  const params = asRecord(payload?.params);
  const candidate =
    params?.id ?? params?.requestId ?? payload?.id ?? payload?.requestId;

  return typeof candidate === "string" || typeof candidate === "number"
    ? candidate
    : undefined;
}

function parseJsonObject(
  value: string,
): Record<string, unknown> | undefined {
  const parsed = safeParseJson(value);
  return parsed.ok ? parsed.value : undefined;
}

function asRecord(
  value: unknown,
): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}
