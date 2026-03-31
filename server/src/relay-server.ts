import http from "node:http";
import type { AddressInfo } from "node:net";

import express, {
  type NextFunction,
  type Request,
  type Response,
} from "express";
import { WebSocket, WebSocketServer } from "ws";
import { z } from "zod";

import {
  SessionRegistry,
  type MessageClassification,
  type SessionConnection,
} from "./session-registry.js";

const messageClassificationSchema = z.enum([
  "agentMessage",
  "toolCall",
  "serverRequest",
  "turnLifecycle",
  "threadLifecycle",
  "unknown",
]);

const registerMessageSchema = z.object({
  type: z.literal("register"),
  sessionId: z.string().min(1),
  displayName: z.string().min(1),
  metadata: z.record(z.unknown()).default({}),
});

const sessionMessageSchema = z.object({
  type: z.literal("message"),
  sessionId: z.string().min(1),
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

export interface RelayServerConfig {
  apiPort: number;
  wsPort: number;
  gracePeriodMs: number;
  historyLimit: number;
}

export interface StartedRelayServer {
  apiPort: number;
  wsPort: number;
  apiBaseUrl: string;
  wsUrl: string;
  close: () => Promise<void>;
}

export async function startRelayServer(
  config: RelayServerConfig,
): Promise<StartedRelayServer> {
  const registry = new SessionRegistry({
    gracePeriodMs: config.gracePeriodMs,
    historyLimit: config.historyLimit,
  });

  const app = express();
  app.disable("x-powered-by");
  app.use(express.json());

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

        registry.recordMessage(sessionId, {
          classification: message.data.classification as MessageClassification,
          method: message.data.method,
          raw: message.data.raw,
          payload: message.data.payload,
          threadId: message.data.threadId,
          turnId: message.data.turnId,
        });
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

        sendJson(socket, {
          type: "ack",
          acknowledged: "auto-detach",
        });
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

function sendJson(socket: WebSocket, payload: unknown): void {
  if (socket.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify(payload));
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
