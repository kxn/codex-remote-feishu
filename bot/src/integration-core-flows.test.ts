import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { once } from "node:events";
import {
  mkdtemp,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { afterEach, describe, expect, it } from "vitest";

import type {
  StartedRelayServer,
  UserSessionEvent,
} from "../../server/dist/relay-server.js";
import { startRelayServer } from "../../server/dist/relay-server.js";
import {
  BotService,
  type IncomingTextMessage,
  type RelayClientLike,
} from "./bot-service.js";
import {
  RelayClient,
  type RelayHistoryEntry,
  type RelaySessionSummary,
} from "./relay.js";

const TEST_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const WRAPPER_BINARY_PATH = path.join(
  TEST_ROOT,
  "wrapper",
  "target",
  "debug",
  "codex-relay-wrapper",
);

const MOCK_CODEX_SCRIPT = `
import readline from "node:readline";
import { appendFile } from "node:fs/promises";

const inputLogPath = process.env.MOCK_CODEX_INPUT_LOG_PATH;
let turnCounter = 0;

function nextTurnId(prefix = "turn") {
  turnCounter += 1;
  return \`\${prefix}-\${turnCounter}\`;
}

async function logInput(line) {
  if (!inputLogPath) {
    return;
  }

  await appendFile(inputLogPath, \`\${line}\\n\`, "utf8");
}

async function emit(message) {
  const line =
    typeof message === "string"
      ? message
      : message && typeof message === "object" && !Array.isArray(message) && "raw" in message
        ? String(message.raw)
        : JSON.stringify(message);
  process.stdout.write(\`\${line}\\n\`);
}

function parseJson(line) {
  try {
    return JSON.parse(line);
  } catch {
    return null;
  }
}

async function handlePrompt(message) {
  const prompt =
    typeof message?.params?.prompt === "string"
      ? message.params.prompt
      : typeof message?.content === "string"
        ? message.content
        : "";
  const turnId = nextTurnId("prompt");
  await emit({
    method: "turn/started",
    params: {
      threadId: "thread-1",
      turnId,
    },
  });
  await emit({
    method: "item/agentMessage/delta",
    params: {
      delta: \`echo:\${prompt}\`,
    },
  });
  await emit({
    method: "turn/completed",
    params: {
      threadId: "thread-1",
      turnId,
      status: "completed",
    },
  });
}

async function handleInterrupt() {
  await emit({
    method: "turn/completed",
    params: {
      threadId: "thread-1",
      turnId: nextTurnId("interrupt"),
      status: "interrupted",
    },
  });
}

async function handleApprovalResponse(message) {
  const decision = message?.result?.decision;
  if (decision !== "accept" && decision !== "decline") {
    return;
  }

  await emit({
    method: "item/agentMessage/delta",
    params: {
      delta: \`approval:\${decision}\`,
    },
  });
  await emit({
    method: "turn/completed",
    params: {
      threadId: "thread-1",
      turnId: nextTurnId("approval"),
      status: decision,
    },
  });
}

const reader = readline.createInterface({
  input: process.stdin,
  crlfDelay: Infinity,
});

for await (const line of reader) {
  await logInput(line);
  const message = parseJson(line);
  if (!message || typeof message !== "object" || Array.isArray(message)) {
    continue;
  }

  if (message.mock === "emit" && Array.isArray(message.messages)) {
    for (const outbound of message.messages) {
      await emit(outbound);
    }
    continue;
  }

  if (message.method === "turn/start") {
    await handlePrompt(message);
    continue;
  }

  if (message.method === "turn/interrupt") {
    await handleInterrupt();
    continue;
  }

  if ("id" in message && message.result && typeof message.result === "object") {
    await handleApprovalResponse(message);
  }
}
`;

type Cleanup = () => Promise<void>;

interface LoggedMessage {
  raw: string;
  parsed: unknown;
}

interface MessengerRecorder {
  sendText: (chatId: string, text: string) => Promise<void>;
  readonly sent: Array<{ chatId: string; text: string }>;
  messagesFor: (chatId: string) => string[];
}

interface MockWrapperSession {
  displayName: string;
  workspaceDir: string;
  inputLogPath: string;
  process: ChildProcessWithoutNullStreams;
  stdoutLines: string[];
  stderrLines: string[];
  emitCodexMessages: (messages: unknown[]) => Promise<void>;
  sendLocalJson: (message: unknown) => Promise<void>;
  readLoggedInputs: () => Promise<LoggedMessage[]>;
  stop: () => Promise<void>;
}

interface RuntimeWebSocket {
  readonly readyState: number;
  send: (data: string) => void;
  close: () => void;
  addEventListener: (
    type: "open" | "message" | "error" | "close",
    listener: (event: { data?: string }) => void,
    options?: { once?: boolean },
  ) => void;
}

const RuntimeWebSocket = (
  globalThis as unknown as { WebSocket: new (url: string) => RuntimeWebSocket }
).WebSocket;

describe("integration core flows", () => {
  let cleanups: Cleanup[] = [];

  afterEach(async () => {
    while (cleanups.length > 0) {
      const cleanup = cleanups.pop();
      if (!cleanup) {
        continue;
      }

      try {
        await cleanup();
      } catch {
        // Best-effort cleanup for child processes and temp directories.
      }
    }
  });

  it("registers wrappers, relays prompts and interrupts through the server API, and auto-detaches on local input", async () => {
    const server = await startTestRelayServer();
    const relayClient = new RelayClient({
      baseUrl: server.apiBaseUrl,
    });
    const wrapper = await spawnMockWrapper(server.wsUrl, "workspace-a");

    const session = await waitForSession(relayClient, "workspace-a");
    expect(session).toEqual(
      expect.objectContaining({
        displayName: "workspace-a",
        online: true,
        state: "idle",
      }),
    );

    const userEvents: UserSessionEvent[] = [];
    const unsubscribe = server.subscribeUserEvents("user-a", (event) => {
      userEvents.push(event);
    });
    cleanups.push(async () => {
      unsubscribe();
    });

    const attached = await relayClient.attach(session.sessionId, "user-a");
    expect(attached.attachedUser).toBe("user-a");

    await wrapper.emitCodexMessages([
      {
        method: "thread/started",
        params: {
          threadId: "thread-1",
        },
      },
      {
        method: "turn/started",
        params: {
          threadId: "thread-1",
          turnId: "seed-turn",
        },
      },
      {
        method: "item/agentMessage/delta",
        params: {
          delta: "hello remote",
        },
      },
    ]);

    await waitForCondition(() => {
      return userEvents.some(
        (event) =>
          event.type === "message" &&
          getRelayMessageText(event.message) === "hello remote",
      );
    });

    const prompt = "hello\\n世界 `json` {\"x\":1}";
    await relayClient.sendPrompt(session.sessionId, prompt);

    const promptInput = await waitForLoggedInput(wrapper, (message) => {
      const parsed = asRecord(message.parsed);
      return (
        parsed?.method === "turn/start" &&
        asRecord(parsed.params)?.prompt === prompt
      );
    });
    expect(asRecord(asRecord(promptInput.parsed)?.params)?.prompt).toBe(prompt);

    await waitForCondition(() => {
      return userEvents.some(
        (event) =>
          event.type === "message" &&
          getRelayMessageText(event.message) === `echo:${prompt}`,
      );
    });

    await relayClient.interrupt(session.sessionId);
    const interruptInput = await waitForLoggedInput(wrapper, (message) => {
      return asRecord(message.parsed)?.method === "turn/interrupt";
    });
    expect(asRecord(interruptInput.parsed)?.method).toBe("turn/interrupt");

    await wrapper.sendLocalJson({
      method: "turn/start",
      params: {
        prompt: "local override",
      },
    });

    await waitForCondition(() => {
      return userEvents.some(
        (event) =>
          event.type === "auto-detach" &&
          event.sessionId === session.sessionId &&
          event.reason === "local-input",
      );
    });

    expect((await relayClient.getSession(session.sessionId)).attachedUser).toBeNull();

    const eventCountAfterDetach = userEvents.length;
    await wrapper.emitCodexMessages([
      {
        method: "item/agentMessage/delta",
        params: {
          delta: "after detach should stay local",
        },
      },
    ]);
    await sleep(200);

    expect(userEvents).toHaveLength(eventCountAfterDetach);
  });

  it("runs approval flows end-to-end through the bot service with a mocked messenger", async () => {
    const server = await startTestRelayServer();
    const relayClient = new RelayClient({
      baseUrl: server.apiBaseUrl,
    });
    const wrapper = await spawnMockWrapper(server.wsUrl, "workspace-b");
    const session = await waitForSession(relayClient, "workspace-b");

    await wrapper.emitCodexMessages([
      {
        method: "turn/started",
        params: {
          threadId: "thread-1",
          turnId: "pre-attach-turn",
        },
      },
      {
        method: "serverRequest/approval",
        params: {
          id: "req-pending",
          tool: "shell",
          command: "npm test",
          path: "src/index.ts",
        },
      },
    ]);

    await waitForCondition(async () => {
      const detail = await relayClient.getSession(session.sessionId);
      return detail.state === "waitingApproval";
    });

    const messenger = createMessengerRecorder();
    const service = new BotService(relayClient, messenger, {
      pollIntervalMs: 25,
    });
    cleanups.push(async () => {
      service.close();
    });

    await service.handleTextMessage(
      createIncomingText("user-1", "chat-1", "/attach workspace-b"),
    );

    await waitForCondition(() => {
      return messenger.messagesFor("chat-1").some((message) => {
        return message.includes("Approval requested.");
      });
    });

    const approvalPrompt = messenger
      .messagesFor("chat-1")
      .find((message) => message.includes("Approval requested."));
    expect(approvalPrompt).toContain("Command: npm test");
    expect(approvalPrompt).toContain("Path: src/index.ts");

    await service.handleTextMessage(createIncomingText("user-1", "chat-1", "y"));
    const accepted = await waitForLoggedInput(wrapper, (message) => {
      const parsed = asRecord(message.parsed);
      return (
        parsed?.id === "req-pending" &&
        asRecord(parsed.result)?.decision === "accept"
      );
    });
    expect(asRecord(asRecord(accepted.parsed)?.result)?.decision).toBe("accept");

    const remotePrompt = "remote prompt keeps attachment";
    await service.handleTextMessage(
      createIncomingText("user-1", "chat-1", remotePrompt),
    );

    await waitForLoggedInput(wrapper, (message) => {
      const parsed = asRecord(message.parsed);
      return (
        parsed?.method === "turn/start" &&
        asRecord(parsed.params)?.prompt === remotePrompt
      );
    });

    expect(service.getAttachment("user-1")).toEqual(
      expect.objectContaining({
        sessionId: session.sessionId,
      }),
    );

    await waitForCondition(() => {
      return messenger
        .messagesFor("chat-1")
        .some((message) => message.includes(`echo:${remotePrompt}`));
    });

    await wrapper.emitCodexMessages([
      {
        method: "turn/started",
        params: {
          threadId: "thread-1",
          turnId: "deny-turn",
        },
      },
      {
        method: "serverRequest/approval",
        params: {
          id: "req-deny",
          tool: "fileChange",
          path: "README.md",
        },
      },
    ]);

    await waitForCondition(() => {
      return messenger.messagesFor("chat-1").some((message) => {
        return message.includes("Request ID: req-deny");
      });
    });

    await service.handleTextMessage(createIncomingText("user-1", "chat-1", "n"));
    const declined = await waitForLoggedInput(wrapper, (message) => {
      const parsed = asRecord(message.parsed);
      return (
        parsed?.id === "req-deny" &&
        asRecord(parsed.result)?.decision === "decline"
      );
    });
    expect(asRecord(asRecord(declined.parsed)?.result)?.decision).toBe("decline");

    expect(messenger.messagesFor("chat-1")).toContain(
      "Approved request for [workspace-b].",
    );
    expect(messenger.messagesFor("chat-1")).toContain(
      "Denied request for [workspace-b].",
    );
  });

  it("forwards every attached agent message under burst output even when history snapshots are bounded", async () => {
    const server = await startTestRelayServer({
      historyLimit: 2,
    });
    const relayClient = new RelayClient({
      baseUrl: server.apiBaseUrl,
    });
    const wrapper = await spawnMockWrapper(server.wsUrl, "workspace-burst");
    const session = await waitForSession(relayClient, "workspace-burst");

    const messenger = createMessengerRecorder();
    const service = new BotService(relayClient, messenger, {
      pollIntervalMs: 10,
    });
    cleanups.push(async () => {
      service.close();
    });

    await service.handleTextMessage(
      createIncomingText("user-burst", "chat-burst", "/attach workspace-burst"),
    );

    const deltas = Array.from({ length: 20 }, (_, index) => `burst-${index + 1}`);
    await wrapper.emitCodexMessages(
      deltas.map((delta) => ({
        method: "item/agentMessage/delta",
        params: {
          delta,
        },
      })),
    );

    await waitForCondition(() => {
      return (
        filterForwardedMessages(
          messenger.messagesFor("chat-burst"),
          "workspace-burst",
        ).length >= deltas.length
      );
    });

    expect(
      filterForwardedMessages(
        messenger.messagesFor("chat-burst"),
        "workspace-burst",
      ),
    ).toEqual(deltas.map((delta) => `[workspace-burst] ${delta}`));
    expect((await relayClient.getHistory(session.sessionId)).map((entry) => entry.raw)).toHaveLength(2);
  });

  it("keeps multiple sessions isolated, preserves ordering, and avoids duplicates across reconnects", async () => {
    const server = await startTestRelayServer();
    const relayClient = new RelayClient({
      baseUrl: server.apiBaseUrl,
    });
    const wrapperA = await spawnMockWrapper(server.wsUrl, "workspace-c");
    const wrapperB = await spawnMockWrapper(server.wsUrl, "workspace-d");

    const sessionA = await waitForSession(relayClient, "workspace-c");
    const sessionB = await waitForSession(relayClient, "workspace-d");

    const messenger = createMessengerRecorder();
    const service = new BotService(relayClient, messenger, {
      pollIntervalMs: 25,
    });
    cleanups.push(async () => {
      service.close();
    });

    await service.handleTextMessage(
      createIncomingText("user-a", "chat-a", "/attach workspace-c"),
    );
    await service.handleTextMessage(
      createIncomingText("user-b", "chat-b", "/attach workspace-d"),
    );

    await wrapperA.emitCodexMessages([
      {
        method: "item/agentMessage/delta",
        params: {
          delta: "one",
        },
      },
      {
        method: "item/agentMessage/delta",
        params: {
          delta: "two",
        },
      },
      {
        method: "item/agentMessage/delta",
        params: {
          delta: "three",
        },
      },
    ]);
    await wrapperB.emitCodexMessages([
      {
        method: "item/agentMessage/delta",
        params: {
          delta: "alpha",
        },
      },
      {
        method: "item/agentMessage/delta",
        params: {
          delta: "beta",
        },
      },
    ]);

    await waitForCondition(() => {
      return (
        filterForwardedMessages(messenger.messagesFor("chat-a"), "workspace-c")
          .length >= 3 &&
        filterForwardedMessages(messenger.messagesFor("chat-b"), "workspace-d")
          .length >= 2
      );
    });

    await wrapperA.emitCodexMessages([
      {
        method: "item/agentMessage/delta",
        params: {
          delta: "before reconnect",
        },
      },
    ]);

    await waitForCondition(() => {
      return filterForwardedMessages(
        messenger.messagesFor("chat-a"),
        "workspace-c",
      ).includes("[workspace-c] before reconnect");
    });

    const shadowSocket = await connectShadowSession(
      server.wsUrl,
      sessionA.sessionId,
      sessionA.displayName,
    );
    await closeShadowSession(shadowSocket);

    await waitForCondition(async () => {
      return !(await relayClient.getSession(sessionA.sessionId)).online;
    });
    await waitForCondition(async () => {
      return (await relayClient.getSession(sessionA.sessionId)).online;
    });

    await wrapperA.emitCodexMessages([
      {
        method: "item/agentMessage/delta",
        params: {
          delta: "after reconnect",
        },
      },
    ]);

    await waitForCondition(() => {
      return filterForwardedMessages(
        messenger.messagesFor("chat-a"),
        "workspace-c",
      ).includes("[workspace-c] after reconnect");
    });

    const chatAMessages = filterForwardedMessages(
      messenger.messagesFor("chat-a"),
      "workspace-c",
    );
    const chatBMessages = filterForwardedMessages(
      messenger.messagesFor("chat-b"),
      "workspace-d",
    );

    expect(chatAMessages).toEqual([
      "[workspace-c] one",
      "[workspace-c] two",
      "[workspace-c] three",
      "[workspace-c] before reconnect",
      "[workspace-c] after reconnect",
    ]);
    expect(chatBMessages).toEqual([
      "[workspace-d] alpha",
      "[workspace-d] beta",
    ]);
    expect(chatAMessages.filter((message) => message.includes("before reconnect"))).toHaveLength(1);
    expect(chatAMessages.filter((message) => message.includes("after reconnect"))).toHaveLength(1);
    expect(chatAMessages.some((message) => message.includes("[workspace-d]"))).toBe(
      false,
    );
    expect(chatBMessages.some((message) => message.includes("[workspace-c]"))).toBe(
      false,
    );
  });

  async function startTestRelayServer(
    overrides: Partial<{
      gracePeriodMs: number;
      historyLimit: number;
    }> = {},
  ): Promise<StartedRelayServer> {
    const server = await startRelayServer({
      apiPort: 0,
      wsPort: 0,
      gracePeriodMs: overrides.gracePeriodMs ?? 500,
      historyLimit: overrides.historyLimit ?? 200,
    });
    cleanups.push(async () => {
      await server.close();
    });
    return server;
  }

  async function spawnMockWrapper(
    relayUrl: string,
    displayName: string,
  ): Promise<MockWrapperSession> {
    const workspaceDir = await mkdtemp(
      path.join(tmpdir(), "codex-relay-bot-integration-"),
    );
    cleanups.push(async () => {
      await rm(workspaceDir, {
        recursive: true,
        force: true,
      });
    });

    const mockCodexPath = path.join(workspaceDir, "mock-codex.mjs");
    const inputLogPath = path.join(workspaceDir, "mock-codex-input.log");
    await writeFile(mockCodexPath, MOCK_CODEX_SCRIPT, "utf8");

    const wrapper = spawn(
      WRAPPER_BINARY_PATH,
      [
        "--codex-binary",
        process.execPath,
        "--relay-url",
        relayUrl,
        "--name",
        displayName,
        "--",
        mockCodexPath,
      ],
      {
        cwd: workspaceDir,
        env: {
          ...process.env,
          MOCK_CODEX_INPUT_LOG_PATH: inputLogPath,
        },
        stdio: ["pipe", "pipe", "pipe"],
      },
    );

    const stdoutLines = collectChildLines(wrapper.stdout);
    const stderrLines = collectChildLines(wrapper.stderr);

    const stop = async (): Promise<void> => {
      if (wrapper.stdin.writable) {
        wrapper.stdin.end();
      }

      const exited = await waitForProcessExit(wrapper, 2_000);
      if (!exited) {
        wrapper.kill("SIGTERM");
        await waitForProcessExit(wrapper, 2_000);
      }
    };

    cleanups.push(stop);

    return {
      displayName,
      workspaceDir,
      inputLogPath,
      process: wrapper,
      stdoutLines,
      stderrLines,
      emitCodexMessages: async (messages) => {
        await writeChildJson(wrapper, {
          mock: "emit",
          messages,
        });
      },
      sendLocalJson: async (message) => {
        await writeChildJson(wrapper, message);
      },
      readLoggedInputs: async () => {
        return readLoggedInputs(inputLogPath);
      },
      stop,
    };
  }
});

async function waitForSession(
  relayClient: RelayClientLike,
  displayName: string,
): Promise<RelaySessionSummary> {
  let matchedSession: RelaySessionSummary | undefined;

  await waitForCondition(async () => {
    const sessions = await relayClient.listSessions();
    matchedSession = sessions.find((session) => session.displayName === displayName);
    return matchedSession !== undefined;
  });

  return matchedSession as RelaySessionSummary;
}

function createIncomingText(
  userId: string,
  chatId: string,
  text: string,
): IncomingTextMessage {
  return {
    userId,
    chatId,
    messageId: `${userId}-${Date.now()}-${Math.random()}`,
    text,
  };
}

function createMessengerRecorder(): MessengerRecorder {
  const sent: Array<{ chatId: string; text: string }> = [];

  return {
    sent,
    sendText: async (chatId: string, text: string) => {
      sent.push({
        chatId,
        text,
      });
    },
    messagesFor: (chatId: string) => {
      return sent
        .filter((message) => message.chatId === chatId)
        .map((message) => message.text);
    },
  };
}

async function waitForLoggedInput(
  wrapper: MockWrapperSession,
  predicate: (message: LoggedMessage) => boolean,
): Promise<LoggedMessage> {
  let matchedMessage: LoggedMessage | undefined;

  await waitForCondition(async () => {
    const messages = await wrapper.readLoggedInputs();
    matchedMessage = messages.find(predicate);
    return matchedMessage !== undefined;
  });

  return matchedMessage as LoggedMessage;
}

async function readLoggedInputs(inputLogPath: string): Promise<LoggedMessage[]> {
  const raw = await readFile(inputLogPath, "utf8").catch((error: NodeJS.ErrnoException) => {
    if (error.code === "ENOENT") {
      return "";
    }

    throw error;
  });

  return raw
    .split("\n")
    .filter((line) => line.length > 0)
    .map((line) => ({
      raw: line,
      parsed: safeParseJson(line),
    }));
}

async function writeChildJson(
  wrapper: ChildProcessWithoutNullStreams,
  message: unknown,
): Promise<void> {
  await writeChildLine(wrapper, `${JSON.stringify(message)}\n`);
}

async function writeChildLine(
  wrapper: ChildProcessWithoutNullStreams,
  line: string,
): Promise<void> {
  if (!wrapper.stdin.write(line)) {
    await once(wrapper.stdin, "drain");
  }
}

function collectChildLines(
  stream: NodeJS.ReadableStream,
): string[] {
  const lines: string[] = [];
  let buffer = "";
  stream.setEncoding("utf8");
  stream.on("data", (chunk: string) => {
    buffer += chunk;

    let newlineIndex = buffer.indexOf("\n");
    while (newlineIndex >= 0) {
      lines.push(buffer.slice(0, newlineIndex));
      buffer = buffer.slice(newlineIndex + 1);
      newlineIndex = buffer.indexOf("\n");
    }
  });
  stream.on("end", () => {
    if (buffer.length > 0) {
      lines.push(buffer);
    }
  });

  return lines;
}

async function waitForProcessExit(
  child: ChildProcessWithoutNullStreams,
  timeoutMs: number,
): Promise<boolean> {
  if (child.exitCode !== null || child.killed) {
    return true;
  }

  return Promise.race([
    once(child, "exit").then(() => true),
    sleep(timeoutMs).then(() => false),
  ]);
}

function getRelayMessageText(message: RelayHistoryEntry): string | undefined {
  const payload = asRecord(message.payload);
  const params = asRecord(payload?.params);

  return firstString(
    params?.delta,
    params?.text,
    payload?.delta,
    payload?.text,
    message.raw,
  );
}

function firstString(...values: unknown[]): string | undefined {
  for (const value of values) {
    if (typeof value === "string" && value.length > 0) {
      return value;
    }
  }

  return undefined;
}

function filterForwardedMessages(
  messages: string[],
  sessionName: string,
): string[] {
  return messages.filter((message) => message.startsWith(`[${sessionName}] `));
}

async function connectShadowSession(
  wsUrl: string,
  sessionId: string,
  displayName: string,
): Promise<RuntimeWebSocket> {
  const socket = await new Promise<RuntimeWebSocket>((resolve, reject) => {
    const client = new RuntimeWebSocket(wsUrl);
    client.addEventListener(
      "open",
      () => {
        resolve(client);
      },
      { once: true },
    );
    client.addEventListener(
      "error",
      () => {
        reject(new Error("Failed to open shadow websocket session"));
      },
      { once: true },
    );
  });

  await new Promise<void>((resolve, reject) => {
    socket.addEventListener(
      "message",
      (event) => {
        const parsed = safeParseJson(String(event.data));
        const payload = asRecord(parsed);
        if (payload?.type === "registered") {
          resolve();
          return;
        }
        reject(new Error(`Unexpected shadow websocket payload: ${JSON.stringify(parsed)}`));
      },
      { once: true },
    );
    socket.send(
      JSON.stringify({
        type: "register",
        sessionId,
        displayName,
        metadata: {
          shadow: true,
        },
      }),
    );
  });

  return socket;
}

async function closeShadowSession(socket: RuntimeWebSocket): Promise<void> {
  await new Promise<void>((resolve) => {
    socket.addEventListener(
      "close",
      () => {
        resolve();
      },
      { once: true },
    );
    socket.close();
  });
}

async function waitForCondition(
  predicate: () => boolean | Promise<boolean>,
  timeoutMs = 5_000,
): Promise<void> {
  const startedAt = Date.now();

  while (true) {
    if (await predicate()) {
      return;
    }

    if (Date.now() - startedAt > timeoutMs) {
      throw new Error("Timed out waiting for condition");
    }

    await sleep(25);
  }
}

async function sleep(milliseconds: number): Promise<void> {
  await new Promise((resolve) => {
    setTimeout(resolve, milliseconds);
  });
}

function safeParseJson(value: string): unknown {
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return value;
  }
}

function asRecord(
  value: unknown,
): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}
