import { afterEach, describe, expect, it, vi } from "vitest";

import { BotService } from "./bot-service.js";

describe("BotService", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("lists available sessions", async () => {
    const relay = createRelayDouble({
      listSessions: vi.fn().mockResolvedValue([
        createSessionDetail({
          sessionId: "session-1",
          displayName: "workspace-a",
        }),
        createSessionDetail({
          sessionId: "session-2",
          displayName: "workspace-b",
          online: false,
        }),
      ]),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger);

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/list",
    });

    expect(relay.listSessions).toHaveBeenCalledTimes(1);
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("workspace-a"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("workspace-b"),
    );
  });

  it("attaches with a unique partial match and sends a session summary", async () => {
    const relay = createRelayDouble({
      listSessions: vi.fn().mockResolvedValue([
        createSessionDetail({
          sessionId: "session-1",
          displayName: "workspace-a",
        }),
        createSessionDetail({
          sessionId: "session-2",
          displayName: "workspace-b",
        }),
      ]),
      attach: vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            state: "executing",
            turnCount: 3,
            attachedUser: "user-1",
            historySize: 1,
            lastMessage: createHistoryEntry({
              raw: '{"method":"item/agentMessage/delta","params":{"delta":"compiled successfully"}}',
              payload: {
                method: "item/agentMessage/delta",
                params: {
                  delta: "compiled successfully",
                },
              },
            }),
          }),
        ),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger, { pollIntervalMs: 1_000 });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach space-a",
    });

    expect(relay.attach).toHaveBeenCalledWith("session-1", "user-1");
    expect(messenger.sendText).toHaveBeenCalledTimes(1);
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("Attached to [workspace-a]."),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("State: executing"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("Turns: 3"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("Last message: compiled successfully"),
    );

    service.close();
  });

  it("lists ambiguous partial matches instead of attaching", async () => {
    const relay = createRelayDouble({
      listSessions: vi.fn().mockResolvedValue([
        createSessionDetail({
          sessionId: "session-1",
          displayName: "workspace-api",
        }),
        createSessionDetail({
          sessionId: "session-2",
          displayName: "workspace-app",
        }),
      ]),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger);

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach workspace-a",
    });

    expect(relay.attach).not.toHaveBeenCalled();
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining('Multiple sessions match "workspace-a"'),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("workspace-api"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("workspace-app"),
    );
  });

  it("uses the current attachment for status and history commands", async () => {
    const relay = createRelayDouble({
      listSessions: vi.fn().mockResolvedValue([
        createSessionDetail({
          sessionId: "session-1",
          displayName: "workspace-a",
        }),
      ]),
      attach: vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            attachedUser: "user-1",
          }),
        ),
      getSession: vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            state: "executing",
            turnCount: 3,
            threadId: "thread-1",
            turnId: "turn-9",
            historySize: 1,
            lastMessage: createHistoryEntry({
              raw: '{"method":"item/agentMessage/delta","params":{"delta":"assistant output"}}',
              payload: {
                method: "item/agentMessage/delta",
                params: {
                  delta: "assistant output",
                },
              },
            }),
          }),
        ),
      getHistory: vi.fn().mockResolvedValue([
        createHistoryEntry({
          raw: "assistant output",
          payload: { text: "assistant output" },
          threadId: "thread-1",
          turnId: "turn-1",
        }),
      ]),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger);

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach session-1",
    });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-2",
      text: "/status",
    });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-3",
      text: "/history 1",
    });

    expect(relay.getSession).toHaveBeenCalledWith("session-1");
    expect(relay.getHistory).toHaveBeenCalledWith("session-1", 1);
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("Session ID: session-1"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("State: executing"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("Thread: thread-1"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("Turn: turn-9"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("Last message: assistant output"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("[workspace-a]\n```text\n"),
    );
  });

  it("forwards agent messages while attached and stops after detach", async () => {
    vi.useFakeTimers();

    const initialEntry = createHistoryEntry({
      raw: '{"method":"item/agentMessage/delta","params":{"delta":"existing"}}',
      payload: {
        method: "item/agentMessage/delta",
        params: {
          delta: "existing",
        },
      },
      receivedAt: "2026-03-31T00:00:00.000Z",
    });
    const forwardedEntry = createHistoryEntry({
      raw: '{"method":"item/agentMessage/delta","params":{"delta":"streamed update"}}',
      payload: {
        method: "item/agentMessage/delta",
        params: {
          delta: "streamed update",
        },
      },
      receivedAt: "2026-03-31T00:00:01.000Z",
    });
    const detachedEntry = createHistoryEntry({
      raw: '{"method":"item/agentMessage/delta","params":{"delta":"should stay local"}}',
      payload: {
        method: "item/agentMessage/delta",
        params: {
          delta: "should stay local",
        },
      },
      receivedAt: "2026-03-31T00:00:02.000Z",
    });

    const historyBySession = new Map<string, ReturnType<typeof createHistoryEntry>[]>([
      ["session-1", [initialEntry]],
    ]);

    const relay = createRelayDouble({
      listSessions: vi.fn().mockResolvedValue([
        createSessionDetail({
          sessionId: "session-1",
          displayName: "workspace-a",
        }),
      ]),
      attach: vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            attachedUser: "user-1",
            historySize: 1,
            lastMessage: initialEntry,
          }),
        ),
      getHistory: vi
        .fn()
        .mockImplementation(async (sessionId: string) => historyBySession.get(sessionId) ?? []),
      detach: vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            attachedUser: null,
          }),
        ),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger, { pollIntervalMs: 100 });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach workspace-a",
    });

    historyBySession.set("session-1", [initialEntry, forwardedEntry]);
    await vi.advanceTimersByTimeAsync(100);

    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("[workspace-a] streamed update"),
    );

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-2",
      text: "/detach",
    });

    historyBySession.set("session-1", [initialEntry, forwardedEntry, detachedEntry]);
    await vi.advanceTimersByTimeAsync(100);

    const sentMessages = messenger.sendText.mock.calls.map(([, text]) => text);
    expect(sentMessages).toContain("Detached from [workspace-a].");
    expect(sentMessages).not.toContain("[workspace-a] should stay local");

    service.close();
  });

  it("keeps forwarded messages isolated per attached user", async () => {
    vi.useFakeTimers();

    const initialA = createHistoryEntry({
      raw: '{"method":"item/agentMessage/delta","params":{"delta":"initial a"}}',
      payload: {
        method: "item/agentMessage/delta",
        params: {
          delta: "initial a",
        },
      },
      receivedAt: "2026-03-31T00:00:00.000Z",
    });
    const initialB = createHistoryEntry({
      raw: '{"method":"item/agentMessage/delta","params":{"delta":"initial b"}}',
      payload: {
        method: "item/agentMessage/delta",
        params: {
          delta: "initial b",
        },
      },
      receivedAt: "2026-03-31T00:00:00.000Z",
    });
    const forwardedA = createHistoryEntry({
      raw: '{"method":"item/agentMessage/delta","params":{"delta":"only a"}}',
      payload: {
        method: "item/agentMessage/delta",
        params: {
          delta: "only a",
        },
      },
      receivedAt: "2026-03-31T00:00:01.000Z",
    });
    const forwardedB = createHistoryEntry({
      raw: '{"method":"item/agentMessage/delta","params":{"delta":"only b"}}',
      payload: {
        method: "item/agentMessage/delta",
        params: {
          delta: "only b",
        },
      },
      receivedAt: "2026-03-31T00:00:01.000Z",
    });

    const historyBySession = new Map<string, ReturnType<typeof createHistoryEntry>[]>([
      ["session-a", [initialA]],
      ["session-b", [initialB]],
    ]);

    const relay = createRelayDouble({
      listSessions: vi.fn().mockResolvedValue([
        createSessionDetail({
          sessionId: "session-a",
          displayName: "workspace-a",
        }),
        createSessionDetail({
          sessionId: "session-b",
          displayName: "workspace-b",
        }),
      ]),
      attach: vi.fn().mockImplementation(async (sessionId: string, userId: string) =>
        createSessionDetail({
          sessionId,
          displayName: sessionId === "session-a" ? "workspace-a" : "workspace-b",
          attachedUser: userId,
          historySize: 1,
          lastMessage: sessionId === "session-a" ? initialA : initialB,
        }),
      ),
      getHistory: vi
        .fn()
        .mockImplementation(async (sessionId: string) => historyBySession.get(sessionId) ?? []),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger, { pollIntervalMs: 100 });

    await service.handleTextMessage({
      userId: "user-a",
      chatId: "chat-a",
      messageId: "message-1",
      text: "/attach workspace-a",
    });
    await service.handleTextMessage({
      userId: "user-b",
      chatId: "chat-b",
      messageId: "message-2",
      text: "/attach workspace-b",
    });

    historyBySession.set("session-a", [initialA, forwardedA]);
    historyBySession.set("session-b", [initialB, forwardedB]);
    await vi.advanceTimersByTimeAsync(100);

    const chatAMessages = messenger.sendText.mock.calls
      .filter(([chatId]) => chatId === "chat-a")
      .map(([, text]) => text)
      .join("\n");
    const chatBMessages = messenger.sendText.mock.calls
      .filter(([chatId]) => chatId === "chat-b")
      .map(([, text]) => text)
      .join("\n");

    expect(chatAMessages).toContain("[workspace-a] only a");
    expect(chatAMessages).not.toContain("only b");
    expect(chatBMessages).toContain("[workspace-b] only b");
    expect(chatBMessages).not.toContain("only a");

    service.close();
  });

  it("returns user-friendly errors for unknown commands and detached prompts", async () => {
    const relay = createRelayDouble();
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger);

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/unknown",
    });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-2",
      text: "/attach missing-session",
    });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-3",
      text: "hello",
    });

    expect(relay.sendPrompt).not.toHaveBeenCalled();
    expect(messenger.sendText).toHaveBeenNthCalledWith(
      1,
      "chat-1",
      'Unknown command "/unknown".',
    );
    expect(messenger.sendText).toHaveBeenNthCalledWith(
      2,
      "chat-1",
      'Session "missing-session" not found.',
    );
    expect(messenger.sendText).toHaveBeenNthCalledWith(
      3,
      "chat-1",
      "Attach to a session first with /attach <session>.",
    );
  });
});

function createRelayDouble(
  overrides: Partial<{
    listSessions: ReturnType<typeof vi.fn>;
    getSession: ReturnType<typeof vi.fn>;
    getHistory: ReturnType<typeof vi.fn>;
    sendPrompt: ReturnType<typeof vi.fn>;
    sendApproval: ReturnType<typeof vi.fn>;
    interrupt: ReturnType<typeof vi.fn>;
    attach: ReturnType<typeof vi.fn>;
    detach: ReturnType<typeof vi.fn>;
  }> = {},
) {
  return {
    listSessions: overrides.listSessions ?? vi.fn().mockResolvedValue([]),
    getSession:
      overrides.getSession ??
      vi.fn().mockResolvedValue(createSessionDetail({ sessionId: "session-1" })),
    getHistory: overrides.getHistory ?? vi.fn().mockResolvedValue([]),
    sendPrompt: overrides.sendPrompt ?? vi.fn().mockResolvedValue(undefined),
    sendApproval:
      overrides.sendApproval ?? vi.fn().mockResolvedValue(undefined),
    interrupt: overrides.interrupt ?? vi.fn().mockResolvedValue(undefined),
    attach:
      overrides.attach ??
      vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            attachedUser: "user-1",
          }),
        ),
    detach:
      overrides.detach ??
      vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            attachedUser: null,
          }),
        ),
  };
}

function createMessengerDouble() {
  return {
    sendText: vi.fn().mockResolvedValue(undefined),
  };
}

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
    lastMessage: unknown;
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

function createHistoryEntry(
  overrides: Partial<{
    direction: "in" | "out";
    classification:
      | "agentMessage"
      | "toolCall"
      | "serverRequest"
      | "turnLifecycle"
      | "threadLifecycle"
      | "unknown";
    method: string;
    raw: string;
    payload: unknown;
    threadId: string | null;
    turnId: string | null;
    receivedAt: string;
  }> = {},
) {
  return {
    direction: overrides.direction ?? "out",
    classification: overrides.classification ?? "agentMessage",
    method: overrides.method ?? "item/agentMessage/delta",
    raw: overrides.raw ?? "assistant output",
    payload: overrides.payload ?? { text: "assistant output" },
    threadId: overrides.threadId ?? null,
    turnId: overrides.turnId ?? null,
    receivedAt: overrides.receivedAt ?? "2026-03-31T00:00:00.000Z",
  };
}
