import { afterEach, describe, expect, it, vi } from "vitest";

import { BotService } from "./bot-service.js";

describe("BotService", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
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

    const userEventsByUser = new Map<string, ReturnType<typeof createUserMessageEvent>[]>([
      ["user-1", []],
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
      listUserEvents: vi
        .fn()
        .mockImplementation(async (userId: string, afterEventId?: number) =>
          createUserEventBatch(userEventsByUser.get(userId) ?? [], afterEventId),
        ),
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

    userEventsByUser.set("user-1", [
      createUserMessageEvent({
        id: 1,
        sessionId: "session-1",
        displayName: "workspace-a",
        message: forwardedEntry,
      }),
    ]);
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

    userEventsByUser.set("user-1", [
      createUserMessageEvent({
        id: 1,
        sessionId: "session-1",
        displayName: "workspace-a",
        message: forwardedEntry,
      }),
      createUserMessageEvent({
        id: 2,
        sessionId: "session-1",
        displayName: "workspace-a",
        message: detachedEntry,
      }),
    ]);
    await vi.advanceTimersByTimeAsync(100);

    const sentMessages = messenger.sendText.mock.calls.map(([, text]) => text);
    expect(sentMessages).toContain("Detached from [workspace-a].");
    expect(sentMessages).not.toContain("[workspace-a] should stay local");

    service.close();
  });

  it("suppresses empty agent messages and still forwards non-empty text fields", async () => {
    vi.useFakeTimers();

    const emptyEntry = createHistoryEntry({
      raw: '{"method":"item/agentMessage/delta","params":{"delta":"","text":"   "}}',
      payload: {
        method: "item/agentMessage/delta",
        params: {
          delta: "",
          text: "   ",
        },
      },
      receivedAt: "2026-03-31T00:00:00.000Z",
    });
    const nonEmptyEntry = createHistoryEntry({
      raw: '{"method":"item/agentMessage/delta","params":{"delta":"","text":"assistant output"}}',
      payload: {
        method: "item/agentMessage/delta",
        params: {
          delta: "",
          text: "assistant output",
        },
      },
      receivedAt: "2026-03-31T00:00:01.000Z",
    });

    const userEventsByUser = new Map<string, ReturnType<typeof createUserMessageEvent>[]>([
      ["user-1", []],
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
            lastMessage: emptyEntry,
          }),
        ),
      listUserEvents: vi
        .fn()
        .mockImplementation(async (userId: string, afterEventId?: number) =>
          createUserEventBatch(userEventsByUser.get(userId) ?? [], afterEventId),
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

    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("Last message: (none yet)"),
    );

    userEventsByUser.set("user-1", [
      createUserMessageEvent({
        id: 1,
        sessionId: "session-1",
        displayName: "workspace-a",
        message: emptyEntry,
      }),
    ]);
    await vi.advanceTimersByTimeAsync(100);

    expect(messenger.sendText).toHaveBeenCalledTimes(1);

    userEventsByUser.set("user-1", [
      createUserMessageEvent({
        id: 1,
        sessionId: "session-1",
        displayName: "workspace-a",
        message: emptyEntry,
      }),
      createUserMessageEvent({
        id: 2,
        sessionId: "session-1",
        displayName: "workspace-a",
        message: nonEmptyEntry,
      }),
    ]);
    await vi.advanceTimersByTimeAsync(100);

    const sentMessages = messenger.sendText.mock.calls.map(([, text]) => text);
    expect(sentMessages).toContain("[workspace-a] assistant output");
    expect(
      sentMessages.some((message) =>
        message.includes('{"method":"item/agentMessage/delta"'),
      ),
    ).toBe(false);

    service.close();
  });

  it("logs and recovers from unexpected forwarding poll errors without leaking unhandled rejections", async () => {
    vi.useFakeTimers();

    const rejection = new Error("relay fetch failed");
    const unhandledRejections: unknown[] = [];
    const handleUnhandledRejection = (reason: unknown) => {
      unhandledRejections.push(reason);
    };
    process.on("unhandledRejection", handleUnhandledRejection);

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
      listUserEvents: vi
        .fn()
        .mockRejectedValueOnce(rejection)
        .mockResolvedValue({
          latestEventId: 0,
          events: [],
        }),
    });
    const messenger = createMessengerDouble();
    const consoleErrorSpy = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);
    const service = new BotService(relay, messenger, { pollIntervalMs: 100 });

    try {
      await service.handleTextMessage({
        userId: "user-1",
        chatId: "chat-1",
        messageId: "message-1",
        text: "/attach workspace-a",
      });

      await vi.advanceTimersByTimeAsync(100);
      await Promise.resolve();

      expect(unhandledRejections).toEqual([]);
      expect(consoleErrorSpy).toHaveBeenCalledWith(
        expect.stringContaining("Background forwarding poll failed"),
        rejection,
      );
      expect(service.getAttachment("user-1")).toMatchObject({
        sessionId: "session-1",
      });

      await vi.advanceTimersByTimeAsync(100);

      expect(relay.listUserEvents).toHaveBeenCalledTimes(2);
    } finally {
      service.close();
      process.off("unhandledRejection", handleUnhandledRejection);
    }
  });

  it("prompts for approvals, re-prompts on non-y/n replies, and approves on y", async () => {
    vi.useFakeTimers();

    const approvalEntry = createHistoryEntry({
      classification: "serverRequest",
      method: "serverRequest/approval",
      raw: JSON.stringify({
        method: "serverRequest/approval",
        params: {
          id: "req-1",
          tool: "commandExecution",
          command: "npm test --watch=false",
        },
      }),
      payload: {
        method: "serverRequest/approval",
        params: {
          id: "req-1",
          tool: "commandExecution",
          command: "npm test --watch=false",
        },
      },
      receivedAt: "2026-03-31T00:00:01.000Z",
    });

    const userEventsByUser = new Map<string, ReturnType<typeof createUserMessageEvent>[]>([
      ["user-1", []],
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
          }),
        ),
      listUserEvents: vi
        .fn()
        .mockImplementation(async (userId: string, afterEventId?: number) =>
          createUserEventBatch(userEventsByUser.get(userId) ?? [], afterEventId),
        ),
      sendApproval: vi.fn().mockResolvedValue(undefined),
      sendPrompt: vi.fn().mockResolvedValue(undefined),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger, { pollIntervalMs: 100 });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach workspace-a",
    });

    userEventsByUser.set("user-1", [
      createUserMessageEvent({
        id: 1,
        sessionId: "session-1",
        displayName: "workspace-a",
        message: approvalEntry,
      }),
    ]);
    await vi.advanceTimersByTimeAsync(100);

    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("Approval requested"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("Command: npm test --watch=false"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("Reply with y to approve or n to deny."),
    );

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-2",
      text: "maybe later",
    });

    expect(relay.sendApproval).not.toHaveBeenCalled();
    expect(relay.sendPrompt).not.toHaveBeenCalled();
    expect(messenger.sendText).toHaveBeenLastCalledWith(
      "chat-1",
      expect.stringContaining("Reply with y to approve or n to deny"),
    );

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-3",
      text: "y",
    });

    expect(relay.sendApproval).toHaveBeenCalledWith("session-1", "req-1", true);
    expect(messenger.sendText).toHaveBeenLastCalledWith(
      "chat-1",
      "Approved request for [workspace-a].",
    );

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-4",
      text: "continue working",
    });

    expect(relay.sendPrompt).toHaveBeenCalledWith(
      "session-1",
      "continue working",
    );

    service.close();
  });

  it("denies approvals on n replies", async () => {
    vi.useFakeTimers();

    const approvalEntry = createHistoryEntry({
      classification: "serverRequest",
      method: "serverRequest/approval",
      raw: JSON.stringify({
        method: "serverRequest/approval",
        params: {
          id: "req-2",
          tool: "fileChange",
          path: "src/bot-service.ts",
        },
      }),
      payload: {
        method: "serverRequest/approval",
        params: {
          id: "req-2",
          tool: "fileChange",
          path: "src/bot-service.ts",
        },
      },
      receivedAt: "2026-03-31T00:00:01.000Z",
    });
    const userEventsByUser = new Map<string, ReturnType<typeof createUserMessageEvent>[]>([
      ["user-1", []],
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
          }),
        ),
      listUserEvents: vi
        .fn()
        .mockImplementation(async (userId: string, afterEventId?: number) =>
          createUserEventBatch(userEventsByUser.get(userId) ?? [], afterEventId),
        ),
      sendApproval: vi.fn().mockResolvedValue(undefined),
      sendPrompt: vi.fn().mockResolvedValue(undefined),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger, { pollIntervalMs: 100 });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach workspace-a",
    });

    userEventsByUser.set("user-1", [
      createUserMessageEvent({
        id: 1,
        sessionId: "session-1",
        displayName: "workspace-a",
        message: approvalEntry,
      }),
    ]);
    await vi.advanceTimersByTimeAsync(100);

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-2",
      text: "n",
    });

    expect(relay.sendApproval).toHaveBeenCalledWith("session-1", "req-2", false);
    expect(messenger.sendText).toHaveBeenLastCalledWith(
      "chat-1",
      "Denied request for [workspace-a].",
    );

    service.close();
  });

  it("clears cancelled pending approvals before an immediate follow-up prompt is handled", async () => {
    vi.useFakeTimers();

    const approvalEntry = createHistoryEntry({
      classification: "serverRequest",
      method: "serverRequest/approval",
      raw: JSON.stringify({
        method: "serverRequest/approval",
        params: {
          id: "req-3",
          tool: "fileChange",
          path: "src/bot-service.ts",
        },
      }),
      payload: {
        method: "serverRequest/approval",
        params: {
          id: "req-3",
          tool: "fileChange",
          path: "src/bot-service.ts",
        },
      },
      receivedAt: "2026-03-31T00:00:01.000Z",
    });
    const turnCompletedEntry = createHistoryEntry({
      classification: "turnLifecycle",
      method: "turn/completed",
      raw: '{"method":"turn/completed"}',
      payload: {
        method: "turn/completed",
      },
      receivedAt: "2026-03-31T00:00:02.000Z",
    });

    const userEventsByUser = new Map<string, ReturnType<typeof createUserMessageEvent>[]>([
      ["user-1", []],
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
          }),
        ),
      listUserEvents: vi
        .fn()
        .mockImplementation(async (userId: string, afterEventId?: number) =>
          createUserEventBatch(userEventsByUser.get(userId) ?? [], afterEventId),
        ),
      sendApproval: vi.fn().mockResolvedValue(undefined),
      sendPrompt: vi.fn().mockResolvedValue(undefined),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger, { pollIntervalMs: 100 });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach workspace-a",
    });

    userEventsByUser.set("user-1", [
      createUserMessageEvent({
        id: 1,
        sessionId: "session-1",
        displayName: "workspace-a",
        message: approvalEntry,
      }),
    ]);
    await vi.advanceTimersByTimeAsync(100);

    userEventsByUser.set("user-1", [
      createUserMessageEvent({
        id: 1,
        sessionId: "session-1",
        displayName: "workspace-a",
        message: approvalEntry,
      }),
      createUserMessageEvent({
        id: 2,
        sessionId: "session-1",
        displayName: "workspace-a",
        message: turnCompletedEntry,
      }),
    ]);
    const messagesBeforeFollowUpPrompt = messenger.sendText.mock.calls.length;

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-2",
      text: "resume after cancel",
    });

    expect(relay.sendApproval).not.toHaveBeenCalled();
    expect(relay.sendPrompt).toHaveBeenCalledWith(
      "session-1",
      "resume after cancel",
    );
    expect(messenger.sendText).toHaveBeenCalledTimes(messagesBeforeFollowUpPrompt);

    service.close();
  });

  it("handles /stop edge cases for detached and idle sessions", async () => {
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
            state: "idle",
            attachedUser: "user-1",
          }),
        ),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger);

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/stop",
    });

    expect(relay.interrupt).not.toHaveBeenCalled();
    expect(messenger.sendText).toHaveBeenNthCalledWith(
      1,
      "chat-1",
      "Attach to a session first with /attach <session>.",
    );

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-2",
      text: "/attach workspace-a",
    });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-3",
      text: "/stop",
    });

    expect(relay.getSession).toHaveBeenCalledWith("session-1");
    expect(relay.interrupt).not.toHaveBeenCalled();
    expect(messenger.sendText).toHaveBeenLastCalledWith(
      "chat-1",
      "Session [workspace-a] is already idle; nothing to stop.",
    );
  });

  it("routes menu actions through the same list, stop, and detach handlers", async () => {
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
            state: "executing",
          }),
        ),
      getSession: vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            attachedUser: "user-1",
            state: "executing",
          }),
        ),
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
    const service = new BotService(relay, messenger);

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach workspace-a",
    });

    await service.handleMenuAction({
      userId: "user-1",
      eventKey: "stop",
    });
    await service.handleMenuAction({
      userId: "user-1",
      eventKey: "detach",
    });
    await service.handleMenuAction({
      userId: "user-1",
      eventKey: "list",
    });
    await service.handleMenuAction({
      userId: "user-1",
      eventKey: "mystery",
    });

    expect(relay.interrupt).toHaveBeenCalledWith("session-1");
    expect(relay.detach).toHaveBeenCalledWith("session-1");
    expect(relay.listSessions).toHaveBeenCalledTimes(2);
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("Sent stop request to [workspace-a]."),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      "Detached from [workspace-a].",
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("workspace-a"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      'Unknown menu action "mystery".',
    );
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

    const userEventsByUser = new Map<
      string,
      ReturnType<typeof createUserMessageEvent>[]
    >([
      ["user-a", []],
      ["user-b", []],
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
      listUserEvents: vi
        .fn()
        .mockImplementation(async (userId: string, afterEventId?: number) =>
          createUserEventBatch(userEventsByUser.get(userId) ?? [], afterEventId),
        ),
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

    userEventsByUser.set("user-a", [
      createUserMessageEvent({
        id: 1,
        sessionId: "session-a",
        displayName: "workspace-a",
        message: forwardedA,
      }),
    ]);
    userEventsByUser.set("user-b", [
      createUserMessageEvent({
        id: 2,
        sessionId: "session-b",
        displayName: "workspace-b",
        message: forwardedB,
      }),
    ]);
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

  it("auto-detaches on local input events for the attached session only", async () => {
    vi.useFakeTimers();

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
      listEvents: vi
        .fn()
        .mockResolvedValueOnce({
          latestEventId: 0,
          events: [],
        })
        .mockResolvedValueOnce({
          latestEventId: 2,
          events: [
            {
              type: "auto-detach",
              id: 1,
              occurredAt: "2026-03-31T00:00:01.000Z",
              userId: "user-1",
              sessionId: "session-2",
              displayName: "workspace-b",
              reason: "local-input",
            },
            {
              type: "auto-detach",
              id: 2,
              occurredAt: "2026-03-31T00:00:02.000Z",
              userId: "user-1",
              sessionId: "session-1",
              displayName: "workspace-a",
              reason: "local-input",
            },
          ],
        })
        .mockResolvedValue({
          latestEventId: 2,
          events: [],
        }),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger, { pollIntervalMs: 100 });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach workspace-a",
    });

    await vi.advanceTimersByTimeAsync(100);

    expect(service.getAttachment("user-1")).toBeUndefined();
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      "Auto-detached: local input detected.",
    );

    const autoDetachMessages = messenger.sendText.mock.calls
      .filter(([, text]) => text === "Auto-detached: local input detected.");
    expect(autoDetachMessages).toHaveLength(1);

    service.close();
  });

  it("notifies the attached user when their session goes offline", async () => {
    vi.useFakeTimers();

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
      listEvents: vi
        .fn()
        .mockResolvedValueOnce({
          latestEventId: 0,
          events: [],
        })
        .mockResolvedValue({
          latestEventId: 0,
          events: [],
        }),
      listUserEvents: vi
        .fn()
        .mockResolvedValueOnce({
          latestEventId: 1,
          events: [
            createUserOfflineEvent({
              id: 1,
              sessionId: "session-1",
              displayName: "workspace-a",
              graceExpiresAt: "2026-03-31T00:05:00.000Z",
            }),
          ],
        })
        .mockResolvedValue({
          latestEventId: 1,
          events: [],
        }),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger, { pollIntervalMs: 100 });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach workspace-a",
    });

    await vi.advanceTimersByTimeAsync(100);

    expect(service.getAttachment("user-1")).toEqual(
      expect.objectContaining({
        sessionId: "session-1",
      }),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      "[workspace-a] Session went offline",
    );

    service.close();
  });

  it("sends lightweight notifications for previously attached sessions when not attached", async () => {
    vi.useFakeTimers();

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
      detach: vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            attachedUser: null,
          }),
        ),
      listEvents: vi
        .fn()
        .mockResolvedValueOnce({
          latestEventId: 0,
          events: [],
        })
        .mockResolvedValueOnce({
          latestEventId: 2,
          events: [
            {
              type: "turn-completed",
              id: 1,
              occurredAt: "2026-03-31T00:00:01.000Z",
              sessionId: "session-1",
              displayName: "workspace-a",
              turnCount: 3,
            },
            {
              type: "input-required",
              id: 2,
              occurredAt: "2026-03-31T00:00:02.000Z",
              sessionId: "session-2",
              displayName: "workspace-b",
              requestId: "req-1",
            },
          ],
        })
        .mockResolvedValue({
          latestEventId: 2,
          events: [],
        }),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger, { pollIntervalMs: 100 });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach workspace-a",
    });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-2",
      text: "/detach",
    });

    await vi.advanceTimersByTimeAsync(100);

    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      "[workspace-a] Turn completed.",
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      "Detached from [workspace-a].",
    );
    expect(messenger.sendText).not.toHaveBeenCalledWith(
      "chat-1",
      "[workspace-b] Input required.",
    );

    service.close();
  });

  it("keeps lightweight notifications isolated to subscribed users and sessions", async () => {
    vi.useFakeTimers();

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
      attach: vi.fn().mockImplementation(async (sessionId: string, userId: string) =>
        createSessionDetail({
          sessionId,
          displayName: sessionId === "session-1" ? "workspace-a" : "workspace-b",
          attachedUser: userId,
        }),
      ),
      detach: vi.fn().mockImplementation(async (sessionId: string) =>
        createSessionDetail({
          sessionId,
          displayName: sessionId === "session-1" ? "workspace-a" : "workspace-b",
          attachedUser: null,
        }),
      ),
      listEvents: vi
        .fn()
        .mockResolvedValueOnce({
          latestEventId: 0,
          events: [],
        })
        .mockResolvedValueOnce({
          latestEventId: 2,
          events: [
            {
              type: "turn-completed",
              id: 1,
              occurredAt: "2026-03-31T00:00:01.000Z",
              sessionId: "session-1",
              displayName: "workspace-a",
              turnCount: 3,
            },
            {
              type: "input-required",
              id: 2,
              occurredAt: "2026-03-31T00:00:02.000Z",
              sessionId: "session-2",
              displayName: "workspace-b",
              requestId: "req-1",
            },
          ],
        })
        .mockResolvedValue({
          latestEventId: 2,
          events: [],
        }),
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
      userId: "user-a",
      chatId: "chat-a",
      messageId: "message-2",
      text: "/detach",
    });
    await service.handleTextMessage({
      userId: "user-b",
      chatId: "chat-b",
      messageId: "message-3",
      text: "/attach workspace-b",
    });
    await service.handleTextMessage({
      userId: "user-b",
      chatId: "chat-b",
      messageId: "message-4",
      text: "/detach",
    });
    await service.handleTextMessage({
      userId: "user-c",
      chatId: "chat-c",
      messageId: "message-5",
      text: "/list",
    });

    await vi.advanceTimersByTimeAsync(100);

    const chatAMessages = messenger.sendText.mock.calls
      .filter(([chatId]) => chatId === "chat-a")
      .map(([, text]) => text)
      .join("\n");
    const chatBMessages = messenger.sendText.mock.calls
      .filter(([chatId]) => chatId === "chat-b")
      .map(([, text]) => text)
      .join("\n");
    const chatCMessages = messenger.sendText.mock.calls
      .filter(([chatId]) => chatId === "chat-c")
      .map(([, text]) => text)
      .join("\n");

    expect(chatAMessages).toContain("[workspace-a] Turn completed.");
    expect(chatAMessages).not.toContain("[workspace-b] Input required.");
    expect(chatBMessages).toContain("[workspace-b] Input required.");
    expect(chatBMessages).not.toContain("[workspace-a] Turn completed.");
    expect(chatCMessages).not.toContain("[workspace-a] Turn completed.");
    expect(chatCMessages).not.toContain("[workspace-b] Input required.");

    service.close();
  });

  it("suppresses lightweight notifications for the currently attached session", async () => {
    vi.useFakeTimers();

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
      attach: vi.fn().mockImplementation(async (sessionId: string) =>
        createSessionDetail({
          sessionId,
          displayName: sessionId === "session-1" ? "workspace-a" : "workspace-b",
          attachedUser: "user-1",
        }),
      ),
      detach: vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-2",
            displayName: "workspace-b",
            attachedUser: null,
          }),
        ),
      listEvents: vi
        .fn()
        .mockResolvedValueOnce({
          latestEventId: 0,
          events: [],
        })
        .mockResolvedValueOnce({
          latestEventId: 2,
          events: [
            {
              type: "input-required",
              id: 1,
              occurredAt: "2026-03-31T00:00:01.000Z",
              sessionId: "session-1",
              displayName: "workspace-a",
              requestId: "req-2",
            },
            {
              type: "turn-completed",
              id: 2,
              occurredAt: "2026-03-31T00:00:02.000Z",
              sessionId: "session-2",
              displayName: "workspace-b",
              turnCount: 1,
            },
          ],
        })
        .mockResolvedValue({
          latestEventId: 2,
          events: [],
        }),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger, { pollIntervalMs: 100 });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach workspace-b",
    });
    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-2",
      text: "/detach",
    });
    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-3",
      text: "/attach workspace-a",
    });

    await vi.advanceTimersByTimeAsync(100);

    const sentMessages = messenger.sendText.mock.calls.map(([, text]) => text);
    expect(sentMessages).toContain("[workspace-b] Turn completed.");
    expect(sentMessages).not.toContain("[workspace-a] Input required.");

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

  it("maps relay transport failures to a user-friendly unavailable message", async () => {
    const relay = createRelayDouble({
      listSessions: vi.fn().mockRejectedValue(new TypeError("fetch failed")),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger);

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/list",
    });

    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      "Relay server is unavailable, please try again later.",
    );
  });
});

function createRelayDouble(
  overrides: Partial<{
    listSessions: ReturnType<typeof vi.fn>;
    getSession: ReturnType<typeof vi.fn>;
    getHistory: ReturnType<typeof vi.fn>;
    listUserEvents: ReturnType<typeof vi.fn>;
    listEvents: ReturnType<typeof vi.fn>;
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
    listUserEvents:
      overrides.listUserEvents ??
      vi
        .fn()
        .mockImplementation(async (_userId: string, afterEventId?: number) => ({
          latestEventId: afterEventId ?? 0,
          events: [],
        })),
    listEvents:
      overrides.listEvents ??
      vi.fn().mockResolvedValue({
        latestEventId: 0,
        events: [],
      }),
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
    userEventCursor: number;
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
    userEventCursor: overrides.userEventCursor ?? 0,
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

function createUserMessageEvent(
  overrides: Partial<{
    id: number;
    occurredAt: string;
    userId: string;
    sessionId: string;
    displayName: string;
    message: ReturnType<typeof createHistoryEntry>;
  }> = {},
) {
  return {
    type: "message" as const,
    id: overrides.id ?? 1,
    occurredAt: overrides.occurredAt ?? "2026-03-31T00:00:00.000Z",
    userId: overrides.userId ?? "user-1",
    sessionId: overrides.sessionId ?? "session-1",
    displayName: overrides.displayName ?? "workspace-a",
    message: overrides.message ?? createHistoryEntry(),
  };
}

function createUserOfflineEvent(
  overrides: Partial<{
    id: number;
    occurredAt: string;
    userId: string;
    sessionId: string;
    displayName: string;
    graceExpiresAt: string | null;
  }> = {},
) {
  return {
    type: "session-offline" as const,
    id: overrides.id ?? 1,
    occurredAt: overrides.occurredAt ?? "2026-03-31T00:00:00.000Z",
    userId: overrides.userId ?? "user-1",
    sessionId: overrides.sessionId ?? "session-1",
    displayName: overrides.displayName ?? "workspace-a",
    graceExpiresAt: overrides.graceExpiresAt ?? null,
  };
}

function createUserEventBatch(
  events: Array<
    | ReturnType<typeof createUserMessageEvent>
    | ReturnType<typeof createUserOfflineEvent>
  >,
  afterEventId?: number,
) {
  const filtered =
    afterEventId === undefined
      ? events
      : events.filter((event) => event.id > afterEventId);

  return {
    latestEventId: events.at(-1)?.id ?? afterEventId ?? 0,
    events: filtered,
  };
}
