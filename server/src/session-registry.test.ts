import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { SessionRegistry } from "./session-registry.js";

function createConnection() {
  return {
    close: vi.fn(),
  };
}

describe("SessionRegistry", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-03-31T12:00:00.000Z"));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("registers a new session as idle and online", () => {
    const registry = new SessionRegistry({
      gracePeriodMs: 5_000,
      historyLimit: 5,
    });

    const result = registry.register({
      sessionId: "session-1",
      displayName: "workspace-a",
      metadata: {
        version: "0.1.0",
        workspacePath: "/tmp/workspace-a",
      },
      connection: createConnection(),
    });

    expect(result.resumed).toBe(false);
    expect(registry.listSessions()).toEqual([
      expect.objectContaining({
        sessionId: "session-1",
        displayName: "workspace-a",
        state: "idle",
        online: true,
        turnCount: 0,
        metadata: {
          version: "0.1.0",
          workspacePath: "/tmp/workspace-a",
        },
        graceExpiresAt: null,
      }),
    ]);
  });

  it("tracks turn execution, approval, and completion transitions", () => {
    const registry = new SessionRegistry({
      gracePeriodMs: 5_000,
      historyLimit: 10,
    });

    registry.register({
      sessionId: "session-1",
      displayName: "workspace-a",
      metadata: {},
      connection: createConnection(),
    });

    registry.recordMessage("session-1", {
      classification: "turnLifecycle",
      method: "turn/started",
      raw: '{"method":"turn/started"}',
    });
    expect(registry.getSession("session-1")?.state).toBe("executing");

    registry.recordMessage("session-1", {
      classification: "serverRequest",
      method: "serverRequest/approval",
      raw: '{"method":"serverRequest/approval"}',
    });
    expect(registry.getSession("session-1")?.state).toBe("waitingApproval");

    registry.recordMessage("session-1", {
      classification: "turnLifecycle",
      method: "turn/completed",
      raw: '{"method":"turn/completed"}',
    });

    expect(registry.getSession("session-1")).toEqual(
      expect.objectContaining({
        state: "idle",
        turnCount: 1,
      }),
    );
  });

  it("starts a grace period on disconnect and resumes within the window", () => {
    const registry = new SessionRegistry({
      gracePeriodMs: 5_000,
      historyLimit: 10,
    });

    const firstConnection = createConnection();
    registry.register({
      sessionId: "session-1",
      displayName: "workspace-a",
      metadata: {},
      connection: firstConnection,
    });
    registry.recordMessage("session-1", {
      classification: "serverRequest",
      method: "serverRequest/approval",
      raw: '{"method":"serverRequest/approval"}',
    });

    registry.disconnect("session-1", firstConnection);

    expect(registry.getSession("session-1")).toEqual(
      expect.objectContaining({
        online: false,
        state: "waitingApproval",
        graceExpiresAt: "2026-03-31T12:00:05.000Z",
      }),
    );

    vi.advanceTimersByTime(3_000);

    const secondConnection = createConnection();
    const result = registry.register({
      sessionId: "session-1",
      displayName: "workspace-a",
      metadata: {},
      connection: secondConnection,
    });

    expect(result.resumed).toBe(true);
    expect(registry.getSession("session-1")).toEqual(
      expect.objectContaining({
        online: true,
        state: "waitingApproval",
        graceExpiresAt: null,
      }),
    );
    expect(registry.getHistory("session-1")).toHaveLength(1);
  });

  it("evicts sessions after the grace period expires", () => {
    const registry = new SessionRegistry({
      gracePeriodMs: 5_000,
      historyLimit: 10,
    });

    const connection = createConnection();
    registry.register({
      sessionId: "session-1",
      displayName: "workspace-a",
      metadata: {},
      connection,
    });

    registry.disconnect("session-1", connection);
    vi.advanceTimersByTime(5_001);

    expect(registry.getSession("session-1")).toBeUndefined();
    expect(registry.listSessions()).toEqual([]);
  });

  it("replaces duplicate active registrations without creating duplicates", () => {
    const registry = new SessionRegistry({
      gracePeriodMs: 5_000,
      historyLimit: 10,
    });

    const firstConnection = createConnection();
    const secondConnection = createConnection();

    registry.register({
      sessionId: "session-1",
      displayName: "workspace-a",
      metadata: {},
      connection: firstConnection,
    });
    registry.recordMessage("session-1", {
      classification: "turnLifecycle",
      method: "turn/started",
      raw: '{"method":"turn/started"}',
    });

    const result = registry.register({
      sessionId: "session-1",
      displayName: "workspace-a",
      metadata: {},
      connection: secondConnection,
    });

    expect(result.resumed).toBe(false);
    expect(firstConnection.close).toHaveBeenCalledTimes(1);
    expect(registry.listSessions()).toHaveLength(1);
    expect(registry.getSession("session-1")).toEqual(
      expect.objectContaining({
        online: true,
        state: "executing",
      }),
    );
  });

  it("stores history chronologically and enforces the configured limit", () => {
    const registry = new SessionRegistry({
      gracePeriodMs: 5_000,
      historyLimit: 2,
    });

    registry.register({
      sessionId: "session-1",
      displayName: "workspace-a",
      metadata: {},
      connection: createConnection(),
    });

    registry.recordMessage("session-1", {
      classification: "agentMessage",
      method: "item/agentMessage/delta",
      raw: "first",
    });
    registry.recordMessage("session-1", {
      classification: "serverRequest",
      method: "serverRequest/approval",
      raw: "second",
    });
    registry.recordMessage("session-1", {
      classification: "turnLifecycle",
      method: "turn/completed",
      raw: "third",
    });

    expect(registry.getHistory("session-1").map((entry) => entry.raw)).toEqual([
      "second",
      "third",
    ]);
    expect(
      registry.getHistory("session-1", 1).map((entry) => entry.raw),
    ).toEqual(["third"]);
  });
});
